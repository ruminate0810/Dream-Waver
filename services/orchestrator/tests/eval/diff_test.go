package eval

import (
	"strings"
	"testing"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// These tests pin the diff harness's behaviour with canned Decks so
// the eval gates are themselves tested. The live LLM-driven eval
// (eval_live_test.go) builds on this — when it runs it produces a
// real Deck which we then pass through Diff.

func TestDistributionOf_EmptyDeck(t *testing.T) {
	t.Parallel()
	if d := DistributionOf(&schema.Deck{}); len(d) != 0 {
		t.Fatalf("empty deck dist not empty: %s", d)
	}
	if d := DistributionOf(nil); len(d) != 0 {
		t.Fatalf("nil deck dist not empty: %s", d)
	}
}

func TestDistributionOf_CountsByLayout(t *testing.T) {
	t.Parallel()
	deck := &schema.Deck{
		Slides: []schema.Slide{
			{Layout: schema.SlideLayout("title")},
			{Layout: schema.SlideLayout("bullets")},
			{Layout: schema.SlideLayout("bullets")},
			{Layout: schema.SlideLayout("metric")},
		},
	}
	d := DistributionOf(deck)
	if d["title"] != 1 || d["bullets"] != 2 || d["metric"] != 1 {
		t.Fatalf("distribution: %s", d)
	}
}

func TestDiff_SlideCount_ExactMatch(t *testing.T) {
	t.Parallel()
	deck := &schema.Deck{Slides: make([]schema.Slide, 8)}
	for i := range deck.Slides {
		deck.Slides[i].Layout = schema.SlideLayout("bullets")
	}

	if err := Diff(deck, Expectation{SlideCount: 8}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}

	if err := Diff(deck, Expectation{SlideCount: 10}); err == nil {
		t.Fatalf("expected fail on slide_count mismatch")
	}
}

func TestDiff_SlideCount_WithTolerance(t *testing.T) {
	t.Parallel()
	deck := &schema.Deck{Slides: make([]schema.Slide, 9)}
	// Tolerance ±2 → 7..11 inclusive should pass.
	if err := Diff(deck, Expectation{SlideCount: 8, SlideCountTolerance: 2}); err != nil {
		t.Fatalf("9 within 8±2 should pass: %v", err)
	}
	if err := Diff(deck, Expectation{SlideCount: 6, SlideCountTolerance: 2}); err == nil {
		t.Fatalf("9 outside 6±2 should fail")
	}
}

func TestDiff_RequiredLayouts(t *testing.T) {
	t.Parallel()
	// A deck that's all bullets — catches the regression that motivated
	// adding RequiredLayouts.
	deck := &schema.Deck{Slides: make([]schema.Slide, 8)}
	for i := range deck.Slides {
		deck.Slides[i].Layout = schema.SlideLayout("bullets")
	}

	err := Diff(deck, Expectation{
		SlideCount:      8,
		RequiredLayouts: []string{"title", "metric"},
	})
	if err == nil {
		t.Fatalf("expected fail when required layouts missing")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"title"`) || !strings.Contains(msg, `"metric"`) {
		t.Fatalf("error should name missing layouts: %s", msg)
	}
}

func TestDiff_MaxPerLayout_CatchesCollapse(t *testing.T) {
	t.Parallel()
	deck := &schema.Deck{Slides: make([]schema.Slide, 10)}
	for i := range deck.Slides {
		deck.Slides[i].Layout = schema.SlideLayout("bullets") // collapse mode
	}

	// Cap bullets at 6 — the planner went past it.
	err := Diff(deck, Expectation{
		SlideCount:   10,
		MaxPerLayout: map[string]int{"bullets": 6},
	})
	if err == nil {
		t.Fatalf("expected fail when bullets exceed MaxPerLayout")
	}
	if !strings.Contains(err.Error(), `"bullets"`) {
		t.Fatalf("error should call out the offending layout: %v", err)
	}
}

func TestDiff_RequiredTitleNonEmpty(t *testing.T) {
	t.Parallel()
	deck := &schema.Deck{
		Slides: []schema.Slide{
			{Layout: schema.SlideLayout("title"), Data: schema.SlideData{Title: "Hello"}},
			{Layout: schema.SlideLayout("bullets"), Data: schema.SlideData{Title: ""}},
		},
	}

	err := Diff(deck, Expectation{
		SlideCount:            2,
		RequiredTitleNonEmpty: true,
	})
	if err == nil {
		t.Fatalf("expected fail on blank title slide")
	}
	if !strings.Contains(err.Error(), "slide 1") {
		t.Fatalf("error should pinpoint blank slide index 1: %v", err)
	}
}

func TestDiff_HealthyDeck_NoViolations(t *testing.T) {
	t.Parallel()
	// What a "good" 8-slide deck looks like — gives a sense of the
	// shape the eval suite considers healthy.
	deck := &schema.Deck{
		Slides: []schema.Slide{
			{Layout: schema.SlideLayout("title"), Data: schema.SlideData{Title: "Q4 Review"}},
			{Layout: schema.SlideLayout("section"), Data: schema.SlideData{Title: "Highlights"}},
			{Layout: schema.SlideLayout("metric"), Data: schema.SlideData{Title: "Revenue up 22%"}},
			{Layout: schema.SlideLayout("bullets"), Data: schema.SlideData{Title: "What drove growth"}},
			{Layout: schema.SlideLayout("bullets"), Data: schema.SlideData{Title: "Customer wins"}},
			{Layout: schema.SlideLayout("section"), Data: schema.SlideData{Title: "Next quarter"}},
			{Layout: schema.SlideLayout("bullets"), Data: schema.SlideData{Title: "Three big bets"}},
			{Layout: schema.SlideLayout("closing"), Data: schema.SlideData{Title: "Questions?"}},
		},
	}
	err := Diff(deck, Expectation{
		SlideCount:            8,
		SlideCountTolerance:   1,
		RequiredLayouts:       []string{"title", "metric", "closing"},
		MaxPerLayout:          map[string]int{"bullets": 5},
		RequiredTitleNonEmpty: true,
	})
	if err != nil {
		t.Fatalf("healthy deck should pass, got: %v", err)
	}
}
