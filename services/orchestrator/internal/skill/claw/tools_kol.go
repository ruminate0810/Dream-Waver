package claw

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// find_kol — the researcher's KOL/influencer discovery tool, ported from the
// kol-* skills (YouTube path first). The backing KOLFinder (wired in main.go)
// hits the official YouTube Data API v3, pulls channel stats + descriptions,
// extracts public business emails, theme-scores, and returns a ranked list.
// The tool formats it as a Markdown table the researcher relays into findings
// (which the writer folds into the report). nil finder = greys out gracefully,
// same posture as the other optional capabilities.

// KOLResult is one creator row (schema mirrors the kol-* skills' CSV columns).
type KOLResult struct {
	Platform     string
	Username     string // handle (e.g. @mkbhd) or channel id
	URL          string
	Nickname     string
	Followers    string // subscriber count (raw string from the API)
	Views        string // total channel views
	VideoCount   string
	Bio          string
	Emails       []string
	Relevance    int
	MatchedTerms []string
}

// KOLFinder discovers creators for a query. theme is a preset name
// (spiritual_en|xuanxue_zh|all) or a comma-separated keyword list ("" = none).
type KOLFinder interface {
	FindKOL(ctx context.Context, query, theme string, maxResults int) ([]KOLResult, error)
}

type FindKOL struct {
	Finder  KOLFinder
	Session *Session
	Emitter event.Emitter
}

func (*FindKOL) Name() string { return "find_kol" }

func (*FindKOL) Description() string {
	return "Find KOLs/influencers (YouTube creators) for an outreach/research list: returns " +
		"channels with subscriber counts and the public business emails creators list in their " +
		"channel description, ranked by relevance. Use for influencer-discovery / 找达人 / 红人选号 " +
		"tasks. Pass a search query (English or Chinese); optionally a theme filter to drop " +
		"off-topic accounts. Returns a Markdown table — include it in your findings."
}

func (*FindKOL) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["query"],
		"properties": {
			"query": {"type": "string", "description": "Search query, e.g. \"tarot reading\" or \"国产手机评测\"."},
			"count": {"type": "integer", "description": "How many creators to return (default 25, max 50)."},
			"theme": {"type": "string", "description": "Optional theme filter: preset 'spiritual_en'|'xuanxue_zh'|'all', or a comma-separated keyword list. Drops off-topic accounts."}
		}
	}`)
}

func (t *FindKOL) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Query string `json:"query"`
		Count int    `json:"count"`
		Theme string `json:"theme"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	query := strings.TrimSpace(p.Query)
	if query == "" {
		return schema.ToolResult{Error: "query is required"}, nil
	}
	if t.Finder == nil {
		return schema.ToolResult{Output: "达人检索未启用(需要 YOUTUBE_API_KEY),跳过。"}, nil
	}
	count := p.Count
	if count <= 0 {
		count = 25
	}
	if count > 50 {
		count = 50
	}

	results, err := t.Finder.FindKOL(ctx, query, strings.TrimSpace(p.Theme), count)
	if err != nil {
		return schema.ToolResult{Error: "KOL search failed: " + err.Error()}, nil
	}
	if len(results) == 0 {
		return schema.ToolResult{Output: fmt.Sprintf("没有找到匹配「%s」的达人(可放宽 theme 或换关键词)。", query)}, nil
	}

	withEmail := 0
	for _, r := range results {
		if len(r.Emails) > 0 {
			withEmail++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 位 YouTube 达人(%d 位有公开邮箱),已按相关度排序:\n\n", len(results), withEmail)
	b.WriteString("| 频道 | 订阅数 | 视频数 | 邮箱 | 相关度 | 链接 |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, r := range results {
		name := r.Username
		if r.Nickname != "" {
			name = fmt.Sprintf("%s(%s)", r.Username, r.Nickname)
		}
		email := strings.Join(r.Emails, "; ")
		if email == "" {
			email = "—"
		}
		rel := "—"
		if r.Relevance > 0 {
			rel = fmt.Sprintf("⭐%d", r.Relevance)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			sanitizeCell(name), orDash(r.Followers), orDash(r.VideoCount), sanitizeCell(email), rel, r.URL)
	}
	b.WriteString("\n(邮箱仅来自创作者在频道简介里公开留的商务联系方式。)")

	return schema.ToolResult{Output: b.String()}, nil
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// sanitizeCell keeps a Markdown table cell on one line (escape pipes/newlines).
func sanitizeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "/")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// ── email extraction (ported from the kol-* skills' email_extract.py) ──────

var (
	emailRe   = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	deobfusAt = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\s*[\(\[\{]\s*at\s*[\)\]\}]\s*`),
		regexp.MustCompile(`(?i)\s+at\s+`),
	}
	deobfusDot = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\s*[\(\[\{]\s*(?:dot|punto)\s*[\)\]\}]\s*`),
		regexp.MustCompile(`(?i)\s+(?:dot|punto)\s+`),
	}
	fileTailRe = regexp.MustCompile(`(?i)\.(png|jpg|jpeg|gif|webp|svg|mp4|css|js)$`)
	alnumRe    = regexp.MustCompile(`[a-z0-9]`)
)

var junkDomains = map[string]bool{
	"example.com": true, "email.com": true, "domain.com": true,
	"sentry.io": true, "wixpress.com": true,
}

var validTLDs = map[string]bool{
	"com": true, "net": true, "org": true, "io": true, "co": true, "me": true,
	"info": true, "biz": true, "app": true, "dev": true, "xyz": true, "live": true,
	"tv": true, "ai": true, "shop": true, "store": true, "online": true, "site": true,
	"email": true, "link": true, "gg": true,
	"uk": true, "us": true, "ca": true, "au": true, "de": true, "fr": true, "es": true,
	"it": true, "nl": true, "se": true, "no": true, "fi": true, "dk": true, "ie": true,
	"pt": true, "pl": true, "ru": true, "ua": true, "jp": true, "cn": true, "hk": true,
	"tw": true, "kr": true, "in": true, "id": true, "ph": true, "my": true, "sg": true,
	"th": true, "vn": true, "br": true, "mx": true, "ar": true, "cl": true, "za": true,
	"nz": true, "tr": true, "ae": true,
}

// ExtractEmails returns deduped, lowercased plausible contact emails from text,
// handling "(at)/(dot)" obfuscations. Exported so the KOL adapters can reuse it.
func ExtractEmails(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	deob := text
	for _, re := range deobfusAt {
		deob = re.ReplaceAllString(deob, "@")
	}
	for _, re := range deobfusDot {
		deob = re.ReplaceAllString(deob, ".")
	}
	seen := map[string]bool{}
	var out []string
	for _, src := range []string{text, deob} {
		for _, m := range emailRe.FindAllString(src, -1) {
			email := strings.ToLower(strings.Trim(m, "."))
			if plausibleEmail(email) && !seen[email] {
				seen[email] = true
				out = append(out, email)
			}
		}
	}
	sort.Strings(out)
	return out
}

func plausibleEmail(email string) bool {
	if strings.Count(email, "@") != 1 {
		return false
	}
	local, domain, _ := strings.Cut(email, "@")
	if local == "" || !strings.Contains(domain, ".") {
		return false
	}
	if !alnumRe.MatchString(local) {
		return false
	}
	if strings.HasPrefix(local, "www.") || strings.HasPrefix(local, "http") {
		return false
	}
	if junkDomains[domain] {
		return false
	}
	if fileTailRe.MatchString(email) {
		return false
	}
	parts := strings.Split(domain, ".")
	if !validTLDs[parts[len(parts)-1]] {
		return false
	}
	return true
}

// ── theme presets + relevance scoring (ported from the kol-* skills) ───────

var kolThemes = map[string][]string{
	"spiritual_en": {
		"tarot", "tarot reading", "astrology", "astrologer", "zodiac", "horoscope",
		"witch", "witchcraft", "wicca", "psychic", "medium", "spiritual",
		"spirituality", "manifest", "manifestation", "numerology", "oracle",
		"divination", "clairvoyant", "reiki", "crystals", "crystal", "moon",
		"lunar", "esoteric", "occult", "mystic", "intuitive", "chakra", "aura",
		"energy healing", "fortune", "birth chart",
	},
	"xuanxue_zh": {
		"玄学", "命理", "算命", "风水", "八字", "紫微", "紫微斗数", "塔罗", "塔罗牌",
		"占卜", "占星", "星座", "运势", "灵修", "能量", "水晶", "周易", "易经",
		"五行", "看相", "面相", "手相", "测算", "开运", "符咒", "灵性", "冥想",
	},
}

// KOLThemeKeywords resolves a theme spec to keywords: a preset name, "all", or
// a comma-separated custom list. Empty spec → nil (no filtering).
func KOLThemeKeywords(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	if spec == "all" {
		return append(append([]string{}, kolThemes["spiritual_en"]...), kolThemes["xuanxue_zh"]...)
	}
	if kw, ok := kolThemes[spec]; ok {
		return append([]string{}, kw...)
	}
	var out []string
	for _, k := range strings.Split(spec, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// KOLScore counts distinct theme keywords found across the signal strings.
func KOLScore(signals, keywords []string) (int, []string) {
	blob := strings.ToLower(strings.Join(signals, " "))
	var matched []string
	for _, kw := range keywords {
		if strings.Contains(blob, strings.ToLower(kw)) {
			matched = append(matched, kw)
		}
	}
	sort.Strings(matched)
	return len(matched), matched
}
