package blueprints

import (
	"strings"
	"testing"
)

// TestLoadAll catches catalog-level bugs at CI time: malformed JSON,
// duplicate IDs, slide_count drift, non-contiguous Pos values. All of
// those are loadErr conditions in LoadAll().
func TestLoadAll(t *testing.T) {
	all, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) < 10 {
		t.Fatalf("want at least 10 blueprints, got %d", len(all))
	}
	for _, bp := range all {
		if bp.Label == "" || bp.Description == "" {
			t.Errorf("%s: missing label or description", bp.ID)
		}
		if bp.TargetAudience == "" {
			t.Errorf("%s: missing target_audience", bp.ID)
		}
		if len(bp.ScenarioTags) == 0 {
			t.Errorf("%s: scenario_tags empty (recommend won't match)", bp.ID)
		}
		if bp.SlideCount < 3 || bp.SlideCount > 40 {
			t.Errorf("%s: slide_count %d outside [3,40]", bp.ID, bp.SlideCount)
		}
		for _, s := range bp.Slides {
			if s.Type == "" || s.Layout == "" {
				t.Errorf("%s slide %d: missing type or layout", bp.ID, s.Pos)
			}
		}
	}
}

// TestByID — the lookup that runFromOutline uses to fetch the user's
// picked blueprint. Includes the empty-string and unknown-id branches
// so we can't regress to a panic.
func TestByID(t *testing.T) {
	if _, ok := ByID(""); ok {
		t.Error("ByID(\"\") should return false")
	}
	if _, ok := ByID("definitely-not-a-real-blueprint"); ok {
		t.Error("ByID(unknown) should return false")
	}
	bp, ok := ByID("series-a-pitch")
	if !ok {
		t.Fatal("ByID(\"series-a-pitch\") not found — did the JSON get renamed?")
	}
	if bp.SlideCount != 11 {
		t.Errorf("series-a-pitch SlideCount = %d, want 11", bp.SlideCount)
	}
}

// TestRecommend — the keyword overlap scoring driving the
// PendingBlueprintPick gate (Sprint BR.2). For each (topic, expected
// top-1) row we verify the right blueprint wins. These are the
// canonical scenarios; if a real user uses a wildly different phrase
// (e.g. "募资 PPT"), we'd need to add it to the matching blueprint's
// scenario_tags rather than change scoring logic.
func TestRecommend(t *testing.T) {
	cases := []struct {
		name      string
		topic     string
		scenario  string
		wantFirst string
	}{
		{"路演投资人 → series-a-pitch", "做一份 Series A 路演 deck 给 VC 看", "", "series-a-pitch"},
		{"产品发布 → product-launch", "下周新功能发布会的演讲稿", "product launch", "product-launch"},
		{"技术分享 → conference-talk", "技术大会演讲：DeepSeek V4 架构", "tech talk", "conference-talk"},
		{"季度汇报 → internal-update", "Q4 团队汇报 6 页内部", "", "internal-update"},
		{"销售拜访 → sales-deck", "给客户做的销售提案 deck", "b2b sales", "sales-deck"},
		{"培训课件 → workshop", "Python 入门培训课件 14 页", "", "workshop"},
		{"客户案例 → case-study", "客户成功案例：某零售品牌", "case study", "case-study"},
		{"路线图 → roadmap", "产品 H1 路线图汇报", "roadmap", "roadmap"},
		{"长读 → editorial-essay", "长文：AI 行业观点 evergreen essay", "", "editorial-essay"},
		{"作品集 → portfolio-lookbook", "我的设计作品集 portfolio", "", "portfolio-lookbook"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Recommend(tc.topic, tc.scenario, 3)
			if err != nil {
				t.Fatalf("Recommend: %v", err)
			}
			if len(got) == 0 {
				t.Fatal("Recommend returned empty")
			}
			if got[0].Blueprint.ID != tc.wantFirst {
				t.Errorf("topic=%q scenario=%q: top-1 = %s (score=%d), want %s.\n  full top-3: %s",
					tc.topic, tc.scenario, got[0].Blueprint.ID, got[0].Score, tc.wantFirst, debugCandidates(got))
			}
		})
	}
}

// TestFormatSkeleton — the markdown snippet that gets appended to the
// planner's user message. The shape is contract — if it changes, the
// prompt's BLUEPRINT section in outline.md needs matching docs.
func TestFormatSkeleton(t *testing.T) {
	bp, ok := ByID("series-a-pitch")
	if !ok {
		t.Skip("series-a-pitch missing")
	}
	md := FormatSkeleton(bp)
	for _, want := range []string{
		"BLUEPRINT:",
		"TARGET AUDIENCE:",
		"REQUIRED SLIDE COUNT: 11",
		"1. type=title, layout=title",
		"11. type=closing, layout=closing",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("FormatSkeleton missing %q in output:\n%s", want, md)
		}
	}
}

func debugCandidates(cs []Candidate) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.Blueprint.ID)
		b.WriteString("(")
		b.WriteString(c.Reason)
		b.WriteString(") ")
	}
	return b.String()
}
