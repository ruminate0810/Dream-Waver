package claw

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractEmails(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"business: mkbhd@gmail.com for inquiries", []string{"mkbhd@gmail.com"}},
		{"reach me at hello [at] brand [dot] com", []string{"hello@brand.com"}},
		{"contact (at) studio (dot) io", []string{"contact@studio.io"}},
		{"no email here", nil},
		{"foo@example.com is junk", nil},                      // junk domain dropped
		{"banner@2x.png not an email", nil},                   // file tail dropped
		{"handle@astro.globe is a social handle", nil},        // invalid TLD dropped
		{"two: a@b.com and c@d.org", []string{"a@b.com", "c@d.org"}}, // sorted, deduped
	}
	for _, c := range cases {
		got := ExtractEmails(c.in)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("ExtractEmails(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestKOLThemeKeywords(t *testing.T) {
	if len(KOLThemeKeywords("")) != 0 {
		t.Error("empty theme should yield no keywords")
	}
	if len(KOLThemeKeywords("spiritual_en")) == 0 {
		t.Error("preset spiritual_en should yield keywords")
	}
	all := KOLThemeKeywords("all")
	if len(all) <= len(KOLThemeKeywords("spiritual_en")) {
		t.Error("'all' should be larger than a single preset")
	}
	custom := KOLThemeKeywords("tarot, 塔罗 ,fengshui")
	if len(custom) != 3 {
		t.Errorf("custom list parse = %v, want 3 items", custom)
	}
}

func TestKOLScore(t *testing.T) {
	kw := KOLThemeKeywords("spiritual_en")
	score, matched := KOLScore([]string{"Daily Tarot Reading", "astrology & horoscope channel"}, kw)
	if score < 2 {
		t.Errorf("expected ≥2 theme hits, got %d (%v)", score, matched)
	}
	score0, _ := KOLScore([]string{"cooking pasta recipes"}, kw)
	if score0 != 0 {
		t.Errorf("off-theme should score 0, got %d", score0)
	}
}

type fakeKOLFinder struct{ rows []KOLResult }

func (f fakeKOLFinder) FindKOL(_ context.Context, _, _ string, _ int) ([]KOLResult, error) {
	return f.rows, nil
}

func TestFindKOLToolFormatsTable(t *testing.T) {
	tool := &FindKOL{Finder: fakeKOLFinder{rows: []KOLResult{
		{Platform: "youtube", Username: "@mkbhd", URL: "https://youtube.com/@mkbhd", Nickname: "MKBHD", Followers: "18000000", VideoCount: "1600", Emails: []string{"biz@mkbhd.com"}, Relevance: 2},
		{Platform: "youtube", Username: "@someone", URL: "https://youtube.com/@someone", Nickname: "Someone", Followers: "5000", VideoCount: "40"},
	}}}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"tech reviews"}`))
	if err != nil || res.Error != "" {
		t.Fatalf("Execute err=%v result.Error=%q", err, res.Error)
	}
	for _, want := range []string{"找到 2 位", "1 位有公开邮箱", "@mkbhd", "biz@mkbhd.com", "| 频道 |", "⭐2"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output missing %q\n---\n%s", want, res.Output)
		}
	}
}

func TestFindKOLWiring(t *testing.T) {
	// With a KOL finder wired, the researcher is enabled and buildTools gives it find_kol.
	r := &Runner{KOL: fakeKOLFinder{}}
	if !r.toolWired("find_kol") {
		t.Fatal("find_kol should be wired when KOL finder is set")
	}
	if !r.roleEnabled(RoleResearcher) {
		t.Fatal("researcher should be enabled when its find_kol is wired")
	}
	role, ok := RoleByKey(RoleResearcher)
	if !ok {
		t.Fatal("researcher role not found")
	}
	var names []string
	for _, tl := range r.buildTools(role, &Session{}) {
		names = append(names, tl.Name())
	}
	if !strings.Contains(strings.Join(names, ","), "find_kol") {
		t.Errorf("buildTools(researcher) missing find_kol; got %v", names)
	}
	// Without a finder, find_kol is unwired (greys out gracefully).
	if (&Runner{}).toolWired("find_kol") {
		t.Error("find_kol should be unwired without a KOL finder")
	}
}

func TestFindKOLToolNilFinder(t *testing.T) {
	tool := &FindKOL{Finder: nil}
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{"query":"x"}`))
	if !strings.Contains(res.Output, "未启用") {
		t.Errorf("nil finder should degrade gracefully, got %q", res.Output)
	}
}
