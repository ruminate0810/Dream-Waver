// Package eval is the regression harness for slide / video / design
// LLM-driven generation. It lives at the repo root (not under
// services/orchestrator/) because evals can need cross-service fixtures
// and the import path stays clean either way.
//
// What this file gives us — Sprint X2c (Phase 4 seed):
//   - StructuralDiff: a deck-shape comparator that catches "the
//     planner started returning all bullets-only layouts" without
//     comparing actual slide text (which is nondeterministic LLM
//     output and varies across model versions).
//   - LayoutDistribution: a tiny aggregator the diff uses, exposed
//     so tests can pin expected distributions in golden files.
//
// What's deliberately NOT here yet (Phase 4 follow-up sprints):
//   - LLM-judge quality scoring (opt-in, nightly only)
//   - Cross-skill fixtures (video specs, design canvases)
//   - Cost-per-eval accounting
package eval

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// LayoutDistribution maps layout name → count. Used to detect
// planner drift (e.g. when a model upgrade causes everything to
// collapse to "bullets" layout). Stable JSON key order so golden
// diffs are deterministic.
type LayoutDistribution map[string]int

func (d LayoutDistribution) String() string {
	if len(d) == 0 {
		return "(empty)"
	}
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, d[k]))
	}
	return strings.Join(parts, " ")
}

// DistributionOf walks a Deck and counts layouts.
func DistributionOf(d *schema.Deck) LayoutDistribution {
	out := LayoutDistribution{}
	if d == nil {
		return out
	}
	for _, s := range d.Slides {
		key := string(s.Layout)
		if key == "" {
			key = "(default)"
		}
		out[key]++
	}
	return out
}

// Expectation is the shape a tests/eval/golden/*.expected.json file
// must match. The JSON tags are lower-cased to match the file format
// (the same shape the slides Pipeline doesn't actually produce — we
// stay decoupled from runtime types here).
type Expectation struct {
	// SlideCount is what we expect the deck to have. The tolerance
	// below allows ±N slides — the LLM has some discretion about
	// expansion / compression.
	SlideCount         int                `json:"slide_count"`
	SlideCountTolerance int               `json:"slide_count_tolerance,omitempty"` // default 0 (exact)

	// LayoutDistribution is a SHAPE check, not an exact match. The
	// diff accepts the actual distribution as OK if:
	//   - every required layout appears AT LEAST MinPerLayout times
	//   - no single layout exceeds MaxPerLayout (defends against the
	//     "all bullets" collapse).
	// Set MinPerLayout / MaxPerLayout to 0 to disable per-layout
	// bounds for that key.
	RequiredLayouts []string       `json:"required_layouts,omitempty"`
	MinPerLayout    map[string]int `json:"min_per_layout,omitempty"`
	MaxPerLayout    map[string]int `json:"max_per_layout,omitempty"`

	// RequiredTitleNonEmpty — every slide must have a non-blank title.
	// Almost-always-true sanity gate; reflects the contract that the
	// renderer needs a title to lay out a slide.
	RequiredTitleNonEmpty bool `json:"required_title_non_empty,omitempty"`
}

// Diff returns nil when actual matches expectation. Otherwise the
// returned error describes every violation (single error so the test
// log shows the whole picture, not just the first failure).
func Diff(actual *schema.Deck, want Expectation) error {
	if actual == nil {
		return fmt.Errorf("eval: actual deck is nil")
	}
	var violations []string

	// Slide count check with tolerance.
	got := len(actual.Slides)
	tol := want.SlideCountTolerance
	if got < want.SlideCount-tol || got > want.SlideCount+tol {
		violations = append(violations,
			fmt.Sprintf("slide_count=%d, want %d±%d", got, want.SlideCount, tol))
	}

	dist := DistributionOf(actual)

	// Required layouts: each must appear at least once (unless
	// MinPerLayout overrides). Catches "planner dropped the metric
	// layout entirely" regressions.
	for _, layout := range want.RequiredLayouts {
		min := 1
		if v, ok := want.MinPerLayout[layout]; ok {
			min = v
		}
		if dist[layout] < min {
			violations = append(violations,
				fmt.Sprintf("layout %q: got %d, want ≥%d", layout, dist[layout], min))
		}
	}

	// Per-layout caps — defends against the "everything became
	// bullets" collapse mode we hit on Sprint G's planner drift.
	for layout, max := range want.MaxPerLayout {
		if dist[layout] > max {
			violations = append(violations,
				fmt.Sprintf("layout %q: got %d, want ≤%d", layout, dist[layout], max))
		}
	}

	// Title non-empty — cheap but catches the worst kind of regression
	// where the planner returns empty headlines.
	if want.RequiredTitleNonEmpty {
		for i, s := range actual.Slides {
			if strings.TrimSpace(s.Data.Title) == "" {
				violations = append(violations,
					fmt.Sprintf("slide %d: title is blank", i))
			}
		}
	}

	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("eval: %d violations\n  - %s\n  actual distribution: %s",
		len(violations),
		strings.Join(violations, "\n  - "),
		dist)
}
