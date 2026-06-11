package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
)

// MoA — the Deck Architect (the planning "brain").
//
// It runs AFTER the outline and BEFORE per-slide SVG authoring. The problem it
// solves: the per-slide authors are independent generalists, so (1) they pick
// charts by accident — a share-of-whole slide ends up a bullet list — and
// (2) each invents its own numbers, so the same metric comes out 38% on one
// slide and 28% on another. The Architect decides layout + chart-type + the
// ACTUAL numbers ONCE, centrally, as a single source of truth; each author then
// EXECUTES that spec instead of improvising. This is the MoA "Architect" role.

// ChartPoint is one labelled value in a chart series (bare number; the unit
// lives on ChartPlan so values stay machine-clean).
type ChartPoint struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// ChartPlan is the Architect's instruction to draw a real chart on a slide:
// the type (bar/donut/line), the unit, and the exact data points.
type ChartPlan struct {
	Type   string       `json:"type"` // "bar" | "donut" | "line"
	Unit   string       `json:"unit"` // "%", "亿元", "万辆", …
	Points []ChartPoint `json:"points"`
}

// SlideSpec is the brief for ONE slide: which layout skeleton, how dense, the
// assertion, the exact facts/numbers to render, and an optional chart.
type SlideSpec struct {
	Index      int        `json:"index"`       // 1-based, matches the outline
	Layout     string     `json:"layout"`      // a skeleton name from the library
	Density    string     `json:"density"`     // "anchor" | "dense"
	KeyMessage string     `json:"key_message"` // the slide's one-line assertion
	Facts      []string   `json:"facts"`       // concrete facts/numbers (single source of truth)
	Chart      *ChartPlan `json:"chart"`       // nil unless the slide's substance IS data
	// SVQ Phase 1 — art-direction (LLM-set): the planner pins the visual choices
	// so all N authors execute ONE coherent look instead of each guessing.
	Focal        string `json:"focal"`         // the ONE element the eye lands on first
	AccentTarget string `json:"accent_target"` // the single thing the accent colour highlights
	Depth        string `json:"depth"`         // the one raised element (one shadow), or "none"
	// Derived in normalize (not LLM-set):
	Exemplar int  `json:"-"` // which GOLD EXEMPLAR (1-6) to emulate, from Layout+Chart
	Hero     bool `json:"-"` // gets the full critique/refine budget (SVQ Phase 2)
}

// DeckSpec is the whole-deck plan: one SlideSpec per outline slide, in order.
type DeckSpec struct {
	Slides []SlideSpec `json:"slides"`
}

// ArchitectDeck asks the planner LLM to turn an approved outline into a
// per-slide DeckSpec (layout + chart plan + pinned numbers). Returns the spec,
// token usage, and any error. Callers treat an error as "no plan" and fall back
// to free per-slide authoring (graceful degradation — the Architect NEVER
// blocks the deck).
func ArchitectDeck(ctx context.Context, router llm.Router, outline *OutlineResult, theme string) (*DeckSpec, llm.Usage, error) {
	if outline == nil || len(outline.Slides) == 0 {
		return nil, llm.Usage{}, fmt.Errorf("ArchitectDeck: outline is empty")
	}
	n := len(outline.Slides)

	var user strings.Builder
	fmt.Fprintf(&user, "Deck title: %s\n", outline.Title)
	if strings.TrimSpace(outline.Subtitle) != "" {
		fmt.Fprintf(&user, "Subtitle: %s\n", outline.Subtitle)
	}
	fmt.Fprintf(&user, "Theme: %s\n\nOutline — produce EXACTLY %d slide specs, same order, sharpening each into an assertion + pinned facts:\n", theme, n)
	for _, s := range outline.Slides {
		fmt.Fprintf(&user, "\n%d. [%s] %s\n", s.Index, s.Type, s.Headline)
		if len(s.KeyPoints) > 0 && string(s.KeyPoints) != "null" {
			fmt.Fprintf(&user, "   points: %s\n", string(s.KeyPoints))
		}
		if strings.TrimSpace(s.SpeakerNotes) != "" {
			fmt.Fprintf(&user, "   note: %s\n", s.SpeakerNotes)
		}
	}
	user.WriteString("\nReturn the DeckSpec JSON now — STRICT JSON, no markdown fences.")

	client := router.For("planner") // the brain runs on the strong (v4-pro) tier
	var spec DeckSpec
	resp, err := askWithRetry(ctx, client, "svg-architect", llm.AskToolRequest{
		Model:             router.ModelFor("planner"),
		SystemPrompt:      prompts.SVGArchitect,
		Messages:          []schema.Message{schema.NewUser(user.String())},
		MaxTokens:         8000,
		EnablePromptCache: true,
	}, func(content string) error {
		var parsed DeckSpec
		if jerr := json.Unmarshal(stripFences(content), &parsed); jerr != nil {
			return fmt.Errorf("parse architect json: %w; raw=%q", jerr, truncate(content, 200))
		}
		if len(parsed.Slides) != n {
			return fmt.Errorf("architect produced %d specs, want %d", len(parsed.Slides), n)
		}
		spec = parsed
		return nil
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}
	spec.normalize(n)
	return &spec, resp.Usage, nil
}

// normalize fills 1-based indices and trims junk so downstream injection is
// safe regardless of how cleanly the LLM filled the shape.
func (d *DeckSpec) normalize(n int) {
	for i := range d.Slides {
		s := &d.Slides[i]
		s.Index = i + 1
		s.Layout = strings.TrimSpace(s.Layout)
		s.Density = strings.ToLower(strings.TrimSpace(s.Density))
		// Drop a chart with no usable data — better no chart than an empty one.
		if s.Chart != nil {
			t := strings.ToLower(strings.TrimSpace(s.Chart.Type))
			if (t != "bar" && t != "donut" && t != "line") || len(s.Chart.Points) == 0 {
				s.Chart = nil
			} else {
				s.Chart.Type = t
			}
		}
		// SVQ Phase 1/2 — trim art-direction + derive the exemplar pointer and
		// the hero/simple budget tier (both deterministic, zero planner tokens).
		s.Focal = strings.TrimSpace(s.Focal)
		s.AccentTarget = strings.TrimSpace(s.AccentTarget)
		s.Depth = strings.TrimSpace(s.Depth)
		s.Exemplar = exemplarFor(s.Layout, s.Chart)
		s.Hero = isHeroSlide(s)
	}
}

// exemplarFor maps a slide's layout + chart to the GOLD EXEMPLAR (1-6) the
// author should emulate. Deterministic — costs zero planner tokens and zero
// cache risk (it rides in the user message, not the cached system prompt).
func exemplarFor(layout string, chart *ChartPlan) int {
	if chart != nil {
		switch chart.Type {
		case "donut":
			return 5
		case "line":
			return 6
		default: // bar
			return 3
		}
	}
	l := strings.ToLower(layout)
	switch {
	case strings.Contains(l, "cover") || strings.Contains(l, "title"):
		return 1
	case strings.Contains(l, "hero") || strings.Contains(l, "big") || strings.Contains(l, "statement"):
		return 4
	default: // metric-row / cards / comparison / generic
		return 2
	}
}

// isHeroSlide marks a slide that earns the full critique/refine budget (SVQ
// Phase 2): data / chart / dense pages where polish pays off, vs sparse
// dividers/covers that rarely need a 12K rewrite. The cheap measure-repair
// still runs globally, so a "simple" slide never ships broken geometry.
func isHeroSlide(s *SlideSpec) bool {
	if s.Chart != nil || s.Density == "dense" {
		return true
	}
	l := strings.ToLower(s.Layout)
	// SVQ Phase 4 floor: the cover and the closing always earn full polish —
	// first and last impression — even though they're sparse "anchor" slides.
	if strings.Contains(l, "cover") || strings.Contains(l, "closing") {
		return true
	}
	switch l {
	case "chart", "metric-row", "metric-hero", "two-col-compare", "comparison",
		"bento", "bento-grid", "timeline", "flow-diagram", "process-flow", "multi-metric":
		return true
	}
	return false
}

// SpecFor returns the spec for a 0-based slide index, or nil when the deck has
// no plan (Architect skipped/failed) or the index is out of range.
func (d *DeckSpec) SpecFor(index0 int) *SlideSpec {
	if d == nil || index0 < 0 || index0 >= len(d.Slides) {
		return nil
	}
	return &d.Slides[index0]
}

// promptBlock renders one SlideSpec as the author-facing brief that gets
// appended to that slide's authoring prompt. Empty when spec is nil.
func (s *SlideSpec) promptBlock() string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n── DECK ARCHITECT'S BRIEF FOR THIS SLIDE — follow it exactly ──\n")
	if s.Layout != "" {
		fmt.Fprintf(&b, "Layout skeleton: %s\n", s.Layout)
	}
	if s.Density != "" {
		fmt.Fprintf(&b, "Density: %s (anchor = sparse/one focal point; dense = substantial)\n", s.Density)
	}
	if strings.TrimSpace(s.KeyMessage) != "" {
		fmt.Fprintf(&b, "Assertion (use as / drive the title): %s\n", s.KeyMessage)
	}
	if len(s.Facts) > 0 {
		b.WriteString("Use EXACTLY these facts/numbers — the single source of truth, do NOT invent different ones:\n")
		for _, f := range s.Facts {
			if f = strings.TrimSpace(f); f != "" {
				fmt.Fprintf(&b, "  • %s\n", f)
			}
		}
	}
	if s.Chart != nil && len(s.Chart.Points) > 0 {
		fmt.Fprintf(&b, "REQUIRED: render a %s chart (see the matching CHART exemplar — compute the coordinates). Unit: %s. Data points in order:\n",
			s.Chart.Type, s.Chart.Unit)
		for _, p := range s.Chart.Points {
			fmt.Fprintf(&b, "  • %s = %s\n", p.Label, trimFloat(p.Value))
		}
		b.WriteString("The chart values and any facts above MUST agree (no 38% vs 28% on the same slide).\n")
	}
	// SVQ Phase 1 — pin the visual treatment so the deck reads coherent (one
	// accent target per slide, one focal point, one unified shadow language).
	if s.Focal != "" || s.AccentTarget != "" || s.Depth != "" {
		b.WriteString("\n── VISUAL DIRECTION (execute exactly; keep the deck coherent) ──\n")
		if s.Focal != "" {
			fmt.Fprintf(&b, "Focal point (make it clearly dominant): %s\n", s.Focal)
		}
		if s.AccentTarget != "" {
			fmt.Fprintf(&b, "Accent colour highlights ONLY: %s\n", s.AccentTarget)
		}
		if s.Depth != "" {
			fmt.Fprintf(&b, "Raised element (one z-level / one soft shadow): %s\n", s.Depth)
		}
	}
	if s.Exemplar > 0 {
		fmt.Fprintf(&b, "Emulate GOLD EXEMPLAR %d for structure.\n", s.Exemplar)
	}
	return b.String()
}

// trimFloat prints a chart value without a trailing ".0" for whole numbers.
func trimFloat(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
