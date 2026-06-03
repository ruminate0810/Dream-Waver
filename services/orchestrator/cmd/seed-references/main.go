// cmd/seed-references — bootstrap the reference_decks corpus by
// generating 10 outlines via stages.Outline directly (no HTTP, no
// rendering, no wizard) and inserting them into the BR.3
// reference_decks table.
//
// Idempotent: slug-uniqueness on the table means re-running the script
// silently skips slugs that already exist. Quality_score defaults to 0
// (unscored); the operator can `psql` UPDATE to bump scores after
// manual review.
//
// Run:
//
//	go run ./cmd/seed-references         # generate the canonical 10
//	go run ./cmd/seed-references -dry    # show seed plan, no LLM calls
//	go run ./cmd/seed-references -only=series-a-pitch  # one blueprint
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/config"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm/providers"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/blueprints"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// seedRow is one entry in the canonical seed table — one blueprint
// gets one canonical topic + theme + tags. Picked to maximise
// scenario+keyword coverage so retrieval has at least one strong
// match for the most common user intents.
type seedRow struct {
	BlueprintID string
	Slug        string
	Title       string
	Topic       string
	Theme       string
	Tags        []string
}

// canonical 10 — one per blueprint. Topics are chosen to be realistic
// and high-leverage so the generated outlines are useful references.
var seeds = []seedRow{
	{
		BlueprintID: "series-a-pitch",
		Slug:        "ref-series-a-codepilot",
		Title:       "Codepilot — Series A 路演",
		Topic:       "Codepilot 是一个把注释实时变成代码的 AI 工具，正在融 Series A，目标 VC 投资人。市场是 $15B 的 AI 开发者工具赛道，已有 $2M ARR，年增长 300%。团队来自 Google / MIT。",
		Theme:       "pitch-deck",
		Tags:        []string{"SaaS", "B2B", "AI", "developer tools", "pitch", "fundraising", "Series A"},
	},
	{
		BlueprintID: "product-launch",
		Slug:        "ref-product-launch-aurora",
		Title:       "Aurora Editor — 公开发布",
		Topic:       "Aurora Editor 是给设计师和开发者协作的新一代代码编辑器，今天正式发布。亮点：AI 实时建议、设计稿 to 代码、内置 Figma 同步。",
		Theme:       "playful",
		Tags:        []string{"product launch", "editor", "AI", "design", "developer tools", "announcement"},
	},
	{
		BlueprintID: "conference-talk",
		Slug:        "ref-talk-llm-scaling",
		Title:       "LLM 训练成本曲线 — NeurIPS 2026 演讲",
		Topic:       "在 NeurIPS 2026 上分享我们对 LLM 训练成本曲线的研究。核心发现是 inference cost 5 年下降了 99%，但 training cost 没有同步下降。听众是研究者 + 工程师。",
		Theme:       "academic",
		Tags:        []string{"LLM", "research", "scaling laws", "AI", "academic", "conference"},
	},
	{
		BlueprintID: "internal-update",
		Slug:        "ref-internal-q4-platform",
		Title:       "Platform 团队 Q4 复盘",
		Topic:       "Platform 团队 Q4 季度内部汇报。这个季度上线了新的 API gateway 让请求延迟降低 40%，但 OKR 里的「文档完整度」没达成（70/100）。Q1 重点是补文档 + 上线计费集成。",
		Theme:       "corporate",
		Tags:        []string{"internal", "quarterly", "platform", "infrastructure", "review"},
	},
	{
		BlueprintID: "sales-deck",
		Slug:        "ref-sales-acme-saas",
		Title:       "ACME → Streamline 销售提案",
		Topic:       "给 ACME 这家中型制造业（5000 员工）做的销售拜访 deck，推荐我们的 Streamline SaaS。客户主要痛点是供应链可视化差。我们已在同行业有 3 个类似客户拿下，平均 ROI 7 个月回本。",
		Theme:       "corporate",
		Tags:        []string{"sales", "b2b", "saas", "manufacturing", "supply chain", "client"},
	},
	{
		BlueprintID: "workshop",
		Slug:        "ref-workshop-react-server-components",
		Title:       "React Server Components 入门工作坊",
		Topic:       "给前端工程师做的 React Server Components 半天工作坊。3 个模块：基础概念 + 数据流模式 + 真实项目重构。需要每模块带练习。",
		Theme:       "tech",
		Tags:        []string{"workshop", "training", "react", "frontend", "tutorial", "course"},
	},
	{
		BlueprintID: "case-study",
		Slug:        "ref-case-study-shein-cdn",
		Title:       "案例：Shein 把视频 CDN 成本砍 60%",
		Topic:       "一份客户成功案例：Shein 部署了我们的 CDN 智能路由方案后，视频流量成本砍了 60%，p99 延迟从 320ms 降到 180ms。客户 CTO 给了一段背书。",
		Theme:       "minimalist",
		Tags:        []string{"case study", "customer story", "CDN", "infrastructure", "ecommerce"},
	},
	{
		BlueprintID: "roadmap",
		Slug:        "ref-roadmap-2026-h1-platform",
		Title:       "2026 H1 Platform 路线图",
		Topic:       "Platform 2026 上半年路线图。回顾：去年上线了 5 个核心 API、SDK 支持 4 种语言。Q1 重点：开发者门户 + OAuth；Q2 重点：分析 dashboard + 计费集成。目标：DAU 翻倍。",
		Theme:       "azure",
		Tags:        []string{"roadmap", "platform", "API", "developer experience", "H1", "planning"},
	},
	{
		BlueprintID: "editorial-essay",
		Slug:        "ref-editorial-ai-coding-future",
		Title:       "深度长文：AI 编程的未来 5 年",
		Topic:       "一篇深度观点长文，论点：未来 5 年开发者的工作会从「写代码」变成「审阅代码 + 设计架构」。开篇 thesis 是「IDE 是过去时；编辑器是未来」。",
		Theme:       "editorial",
		Tags:        []string{"essay", "thought leadership", "AI", "coding", "future", "opinion"},
	},
	{
		BlueprintID: "portfolio-lookbook",
		Slug:        "ref-portfolio-design-studio-meridian",
		Title:       "Meridian 设计工作室作品集",
		Topic:       "Meridian Design Studio 的 2026 作品集。我们做高端品牌视觉 + 摄影方向。代表作：3 个奢侈品牌识别系统 + 2 个时装大片项目。客户包括 LVMH 和 Comme des Garçons。",
		Theme:       "noir",
		Tags:        []string{"portfolio", "design", "fashion", "branding", "luxury", "photography", "lookbook"},
	},
}

func main() {
	_ = godotenv.Load(".env", "../../.env", "../../../.env")

	var dry bool
	var only string
	flag.BoolVar(&dry, "dry", false, "print plan but don't call LLM or insert")
	flag.StringVar(&only, "only", "", "comma-separated blueprint IDs (default: all 10)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	setupLogger("info")

	// Filter by --only.
	filterSet := map[string]bool{}
	if only != "" {
		for _, id := range strings.Split(only, ",") {
			filterSet[strings.TrimSpace(id)] = true
		}
	}
	plan := make([]seedRow, 0, len(seeds))
	for _, s := range seeds {
		if len(filterSet) > 0 && !filterSet[s.BlueprintID] {
			continue
		}
		plan = append(plan, s)
	}
	if len(plan) == 0 {
		slog.Error("no seeds match filter", "only", only)
		os.Exit(1)
	}

	// Plan summary table.
	fmt.Println("Seed plan:")
	for i, s := range plan {
		bp, _ := blueprints.ByID(s.BlueprintID)
		fmt.Printf("  %2d. %-18s → slug=%s (%d slides, theme=%s)\n", i+1, s.BlueprintID, s.Slug, bp.SlideCount, s.Theme)
	}
	if dry {
		fmt.Println("\n(dry run — no LLM calls)")
		return
	}

	// LLM router. We only need the planner; outline doesn't touch
	// worker/critic. Bind worker/critic anyway so router stays
	// well-formed for any future Outline path that wants them.
	primary := providers.NewDeepSeek(cfg.DeepSeekAPIKey, cfg.DeepSeekBaseURL, cfg.ModelWorker)
	router := llm.NewMultiRouter(primary)
	router.Bind("planner", primary, cfg.ModelPlanner)
	router.Bind("worker", primary, cfg.ModelWorker)
	router.Bind("critic", primary, cfg.ModelCritic)

	// DB store. Apply migrations on connect so the reference_decks
	// table is guaranteed present even on a fresh DB.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	dataStore, err := store.New(ctx, cfg.DatabaseURL, cfg.MigrationsDir)
	if err != nil {
		slog.Error("store.New", "err", err)
		os.Exit(1)
	}
	defer dataStore.Close()
	if dataStore.ReferenceDecks == nil {
		slog.Error("dataStore.ReferenceDecks nil — refusing to seed")
		os.Exit(1)
	}

	var ok, skipped, failed int
	for i, s := range plan {
		fmt.Printf("\n[%d/%d] generating %s …\n", i+1, len(plan), s.Slug)

		// Skip if slug already exists.
		if existing, err := dataStore.ReferenceDecks.GetBySlug(ctx, s.Slug); err == nil && existing != nil {
			fmt.Printf("       skipped (already exists, id=%s, score=%d)\n", existing.ID, existing.QualityScore)
			skipped++
			continue
		} else if err != nil && !errors.Is(err, store.ErrReferenceDeckNotFound) {
			slog.Warn("GetBySlug failed (proceeding)", "slug", s.Slug, "err", err)
		}

		// Generate the outline directly via stages.Outline. Blueprint
		// constraint guarantees the slide_count + type sequence.
		bp, hasBP := blueprints.ByID(s.BlueprintID)
		if !hasBP {
			slog.Error("blueprint missing", "id", s.BlueprintID)
			failed++
			continue
		}
		params := stages.OutlineParams{
			Topic:       s.Topic,
			Audience:    bp.TargetAudience,
			SlideCount:  bp.SlideCount,
			Style:       s.Theme, // bias the planner's theme pick
			BlueprintID: s.BlueprintID,
		}
		outline, usage, err := stages.Outline(ctx, router, params)
		if err != nil {
			slog.Error("stages.Outline failed", "slug", s.Slug, "err", err)
			failed++
			continue
		}
		if len(outline.Slides) != bp.SlideCount {
			slog.Warn("slide count mismatch (saving anyway)",
				"slug", s.Slug, "want", bp.SlideCount, "got", len(outline.Slides))
		}
		outline.BlueprintID = s.BlueprintID

		// Re-serialize the outline so the row's outline_json is the
		// canonical OutlineResult shape (not whatever the planner LLM
		// emitted directly — that might have field-order quirks).
		outlineJSON, err := marshalOutline(outline)
		if err != nil {
			slog.Error("marshal outline", "slug", s.Slug, "err", err)
			failed++
			continue
		}

		row := &store.ReferenceDeck{
			Slug:         s.Slug,
			Scenario:     s.BlueprintID, // same vocabulary as blueprint id; simpler retrieval
			BlueprintID:  s.BlueprintID,
			Theme:        s.Theme,
			TopicTags:    s.Tags,
			Title:        s.Title,
			OutlineJSON:  outlineJSON,
			QualityScore: 0, // operator scores manually via psql
		}
		saved, err := dataStore.ReferenceDecks.Insert(ctx, row)
		if err != nil {
			slog.Error("Insert failed", "slug", s.Slug, "err", err)
			failed++
			continue
		}
		fmt.Printf("       ✓ saved id=%s · slides=%d · in=%d out=%d toks\n",
			saved.ID, len(outline.Slides), usage.InputTokens, usage.OutputTokens)
		ok++
	}

	fmt.Printf("\n=== summary ===\n  ok      : %d\n  skipped : %d (already in corpus)\n  failed  : %d\n  total   : %d\n",
		ok, skipped, failed, len(plan))
	if failed > 0 {
		os.Exit(2)
	}
}

// marshalOutline serialises an OutlineResult so the stored JSON is
// the typed shape we'll feed back to the planner — no field-order
// quirks from the raw LLM emission. Pretty-printed for easy psql
// review.
func marshalOutline(o *stages.OutlineResult) ([]byte, error) {
	return json.MarshalIndent(o, "", "  ")
}

func setupLogger(level string) {
	lvl := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}
