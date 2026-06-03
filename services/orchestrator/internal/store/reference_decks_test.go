package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// fixture decks for retrieval test. Keep tiny but realistic — covers
// blueprint match, scenario match, tag overlap, and a "nothing
// matches" baseline.
func seedRefs(t *testing.T) ReferenceDecks {
	t.Helper()
	s := newMemReferenceDecks()
	ctx := context.Background()
	rows := []*ReferenceDeck{
		{Slug: "saas-pitch-codepilot", Scenario: "pitch", BlueprintID: "series-a-pitch", Theme: "pitch-deck",
			TopicTags: []string{"SaaS", "AI", "developer tools"}, Title: "Codepilot Series A",
			OutlineJSON: []byte(`{"title":"Codepilot","slides":[]}`), QualityScore: 4},
		{Slug: "consumer-pitch-cookies", Scenario: "pitch", BlueprintID: "series-a-pitch", Theme: "playful",
			TopicTags: []string{"D2C", "food", "ecommerce"}, Title: "Cookie Brand Series A",
			OutlineJSON: []byte(`{"title":"Cookies","slides":[]}`), QualityScore: 3},
		{Slug: "tech-talk-llm", Scenario: "talk", BlueprintID: "conference-talk", Theme: "tech",
			TopicTags: []string{"LLM", "AI", "research"}, Title: "LLM Scaling Laws",
			OutlineJSON: []byte(`{"title":"LLM","slides":[]}`), QualityScore: 5},
		{Slug: "internal-q4-platform", Scenario: "internal-update", BlueprintID: "internal-update", Theme: "corporate",
			TopicTags: []string{"platform", "infra"}, Title: "Platform Q4",
			OutlineJSON: []byte(`{"title":"Q4","slides":[]}`), QualityScore: 2},
	}
	for _, r := range rows {
		if _, err := s.Insert(ctx, r); err != nil {
			t.Fatalf("Insert(%s): %v", r.Slug, err)
		}
	}
	return s
}

func TestReference_InsertGet(t *testing.T) {
	s := seedRefs(t)
	ctx := context.Background()

	got, err := s.GetBySlug(ctx, "tech-talk-llm")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.Title != "LLM Scaling Laws" {
		t.Errorf("title = %q, want LLM Scaling Laws", got.Title)
	}

	if _, err := s.GetBySlug(ctx, "no-such-slug"); err != ErrReferenceDeckNotFound {
		t.Errorf("GetBySlug(no-such): err = %v, want ErrReferenceDeckNotFound", err)
	}
	if _, err := s.GetByID(ctx, uuid.Nil); err != ErrReferenceDeckNotFound {
		t.Errorf("GetByID(nil): err = %v, want ErrReferenceDeckNotFound", err)
	}
	// Dup slug rejected.
	if _, err := s.Insert(ctx, &ReferenceDeck{Slug: "tech-talk-llm", Scenario: "talk", Theme: "tech",
		OutlineJSON: []byte(`{}`)}); err == nil {
		t.Error("Insert with dup slug should error")
	}
}

// TestReference_Retrieve drives the keyword+tag+scenario+blueprint
// scoring through a few realistic queries. The order matters — top-1
// is what the planner will see most prominently.
func TestReference_Retrieve(t *testing.T) {
	s := seedRefs(t)
	ctx := context.Background()

	cases := []struct {
		name      string
		q         RetrieveQuery
		wantFirst string
	}{
		{
			"blueprint+scenario+tag overlap → codepilot wins",
			RetrieveQuery{Topic: "AI developer tooling SaaS", Scenario: "pitch", BlueprintID: "series-a-pitch", K: 2},
			"saas-pitch-codepilot",
		},
		{
			"blueprint+scenario but topic D2C food → cookies wins (matching tags)",
			RetrieveQuery{Topic: "D2C cookie ecommerce", Scenario: "pitch", BlueprintID: "series-a-pitch", K: 2},
			"consumer-pitch-cookies",
		},
		{
			"talk blueprint → tech-talk-llm wins via blueprint+quality",
			RetrieveQuery{Topic: "LLM research", BlueprintID: "conference-talk", K: 2},
			"tech-talk-llm",
		},
		{
			"no blueprint, no scenario, just topic tokens → tech-talk-llm (5-star) edges out",
			RetrieveQuery{Topic: "LLM AI research", K: 2},
			"tech-talk-llm",
		},
		{
			"MinQuality=4 filters low-scored rows",
			RetrieveQuery{Topic: "anything", K: 5, MinQuality: 4},
			"tech-talk-llm", // 5 + codepilot 4 = only 2 survive; tech-talk first by score
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Retrieve(ctx, tc.q)
			if err != nil {
				t.Fatalf("Retrieve: %v", err)
			}
			if len(got) == 0 {
				t.Fatal("empty result")
			}
			if got[0].Slug != tc.wantFirst {
				slugs := []string{}
				for _, r := range got {
					slugs = append(slugs, r.Slug)
				}
				t.Errorf("top-1 = %s, want %s. ordered: %v", got[0].Slug, tc.wantFirst, slugs)
			}
		})
	}
}

func TestReference_TokenizeCJK(t *testing.T) {
	cases := map[string][]string{
		"Series A 路演":         {"series", "a", "路", "演"},
		"product-launch":      {"product", "launch"},
		"  spaces & punct! ": {"spaces", "punct"},
		"AI / 数据 / 2026":     {"ai", "数", "据", "2026"},
	}
	for in, want := range cases {
		got := tokenize(in)
		if !sliceEq(got, want) {
			t.Errorf("tokenize(%q) = %v, want %v", in, got, want)
		}
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
