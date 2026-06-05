package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
)

// PlanDeckParallel — the speed optimization for mode=svg.
//
// The serial Outline (one big v4-pro call, ~9 min for 20 slides) + ArchitectDeck
// (another v4-pro call, ~3 min) were the planning bottleneck: ~12 min of nothing
// before the (already-parallel) per-slide authoring could start. This replaces
// them with a chunked, mostly-parallel plan:
//
//	1. SKELETON — one fast call carves the deck into N coherent sections with a
//	   slide allocation (small output → quick).
//	2. SECTIONS — N section-planners run IN PARALLEL, each fleshing out its
//	   section's slides with BOTH the outline rows AND the architect spec
//	   (layout / chart / pinned numbers). Each is small (4-5 slides) so the
//	   whole fan-out finishes in ~the slowest single section, not the sum.
//	3. MERGE — concatenate sections in order → OutlineResult + DeckSpec.
//
// 20-slide planning drops from ~12 min to ~3-4 min. The caller (RunSVG) falls
// back to the serial Outline + ArchitectDeck on ANY error here, so a flaky
// parallel plan never blocks a deck.
//
// Reference grounding is preserved: the skeleton sees the full reference; each
// section gets its skeleton-distilled focus plus a truncated reference excerpt.

const svgSectionConcurrency = 4 // parallel section-planner calls

type skeletonSection struct {
	Title      string `json:"title"`
	Focus      string `json:"focus"`
	SlideCount int    `json:"slide_count"`
}

type deckSkeleton struct {
	Title    string            `json:"title"`
	Subtitle string            `json:"subtitle"`
	Theme    string            `json:"theme"`
	Sections []skeletonSection `json:"sections"`
}

// plannedSlide is one section-planner output row: the outline fields AND the
// architect-spec fields for a single slide, combined.
type plannedSlide struct {
	Type         string          `json:"type"`
	Headline     string          `json:"headline"`
	KeyPoints    json.RawMessage `json:"key_points"`
	SpeakerNotes string          `json:"speaker_notes"`
	Layout       string          `json:"layout"`
	Density      string          `json:"density"`
	KeyMessage   string          `json:"key_message"`
	Facts        []string        `json:"facts"`
	Chart        *ChartPlan      `json:"chart"`
}

type sectionPlanResult struct {
	Slides []plannedSlide `json:"slides"`
}

// PlanDeckParallel produces the same (OutlineResult, DeckSpec) pair that
// Outline + ArchitectDeck would, but via the chunked parallel plan above.
func PlanDeckParallel(ctx context.Context, router llm.Router, in OutlineParams, theme string) (*OutlineResult, *DeckSpec, llm.Usage, error) {
	var total llm.Usage
	want := in.SlideCount
	if want <= 0 {
		want = 8
	}

	skel, u, err := planSkeleton(ctx, router, in, want)
	addUsage(&total, u)
	if err != nil {
		return nil, nil, total, fmt.Errorf("skeleton: %w", err)
	}
	if len(skel.Sections) == 0 {
		return nil, nil, total, fmt.Errorf("skeleton produced 0 sections")
	}

	// Run the section planners in parallel.
	type secRes struct {
		slides []plannedSlide
		usage  llm.Usage
		err    error
	}
	results := make([]secRes, len(skel.Sections))
	sem := make(chan struct{}, svgSectionConcurrency)
	var wg sync.WaitGroup
	for i, sec := range skel.Sections {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, sec skeletonSection) {
			defer wg.Done()
			defer func() { <-sem }()
			slides, su, serr := planSection(ctx, router, in, skel, sec, i)
			results[i] = secRes{slides: slides, usage: su, err: serr}
		}(i, sec)
	}
	wg.Wait()

	// Merge in section order.
	var outSlides []OutlineSlide
	var specs []SlideSpec
	idx := 0
	for i := range skel.Sections {
		r := results[i]
		addUsage(&total, r.usage)
		if r.err != nil {
			return nil, nil, total, fmt.Errorf("section %d (%q): %w", i+1, skel.Sections[i].Title, r.err)
		}
		for _, ps := range r.slides {
			idx++
			outSlides = append(outSlides, OutlineSlide{
				Index:        idx,
				Type:         ps.Type,
				Headline:     ps.Headline,
				KeyPoints:    ps.KeyPoints,
				SpeakerNotes: ps.SpeakerNotes,
			})
			specs = append(specs, SlideSpec{
				Index:      idx,
				Layout:     ps.Layout,
				Density:    ps.Density,
				KeyMessage: ps.KeyMessage,
				Facts:      ps.Facts,
				Chart:      ps.Chart,
			})
		}
	}
	if len(outSlides) == 0 {
		return nil, nil, total, fmt.Errorf("parallel plan merged to 0 slides")
	}

	outTheme := schema.Theme(strings.TrimSpace(skel.Theme))
	if outTheme == "" {
		outTheme = schema.Theme(theme)
	}
	outline := &OutlineResult{
		Title:    strings.TrimSpace(skel.Title),
		Subtitle: strings.TrimSpace(skel.Subtitle),
		Theme:    outTheme,
		Slides:   outSlides,
	}
	spec := &DeckSpec{Slides: specs}
	spec.normalize(len(specs)) // reuse the architect's chart/field validation
	return outline, spec, total, nil
}

func planSkeleton(ctx context.Context, router llm.Router, in OutlineParams, want int) (*deckSkeleton, llm.Usage, error) {
	var user strings.Builder
	fmt.Fprintf(&user, "Topic: %s\n", in.Topic)
	if strings.TrimSpace(in.Audience) != "" {
		fmt.Fprintf(&user, "Audience: %s\n", in.Audience)
	}
	if strings.TrimSpace(in.Style) != "" {
		fmt.Fprintf(&user, "Style/tone: %s\n", in.Style)
	}
	fmt.Fprintf(&user, "TOTAL slides (section slide_counts MUST sum to this): %d\n", want)
	if ref := strings.TrimSpace(in.ReferenceText); ref != "" {
		fmt.Fprintf(&user, "\nReference material to ground the structure in:\n%s\n", truncate(ref, 16000))
	}
	user.WriteString("\nProduce the skeleton JSON now.")

	client := router.For("planner")
	var skel deckSkeleton
	resp, err := askWithRetry(ctx, client, "svg-skeleton", llm.AskToolRequest{
		Model:             router.ModelFor("planner"),
		SystemPrompt:      prompts.SVGSkeleton,
		Messages:          []schema.Message{schema.NewUser(user.String())},
		MaxTokens:         2200,
		EnablePromptCache: true,
	}, func(content string) error {
		skel = deckSkeleton{}
		if jerr := json.Unmarshal(stripFences(content), &skel); jerr != nil {
			return fmt.Errorf("parse skeleton json: %w; raw=%q", jerr, truncate(content, 160))
		}
		if len(skel.Sections) == 0 {
			return fmt.Errorf("skeleton has no sections")
		}
		return nil
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}
	return &skel, resp.Usage, nil
}

func planSection(ctx context.Context, router llm.Router, in OutlineParams, skel *deckSkeleton, sec skeletonSection, secIdx int) ([]plannedSlide, llm.Usage, error) {
	n := sec.SlideCount
	if n < 1 {
		n = 1
	}
	var user strings.Builder
	fmt.Fprintf(&user, "Deck title: %s\nSubtitle: %s\n\nFull section list (narrative context — keep your section coherent with the arc, but plan ONLY yours):\n", skel.Title, skel.Subtitle)
	for i, s := range skel.Sections {
		marker := "  "
		if i == secIdx {
			marker = "▶ "
		}
		fmt.Fprintf(&user, "%s%d. %s\n", marker, i+1, s.Title)
	}
	fmt.Fprintf(&user, "\n── PLAN ONLY THIS SECTION (section %d of %d) ──\nTitle: %s\nFocus: %s\nProduce EXACTLY %d slides.\n",
		secIdx+1, len(skel.Sections), sec.Title, sec.Focus, n)
	if ref := strings.TrimSpace(in.ReferenceText); ref != "" {
		fmt.Fprintf(&user, "\nReference material (ground your numbers in this):\n%s\n", truncate(ref, 6000))
	}
	user.WriteString("\nProduce the section JSON now.")

	client := router.For("planner")
	var sp sectionPlanResult
	resp, err := askWithRetry(ctx, client, "svg-section", llm.AskToolRequest{
		Model:             router.ModelFor("planner"),
		SystemPrompt:      prompts.SVGSection,
		Messages:          []schema.Message{schema.NewUser(user.String())},
		MaxTokens:         2000 + n*900,
		EnablePromptCache: true,
	}, func(content string) error {
		sp = sectionPlanResult{}
		if jerr := json.Unmarshal(stripFences(content), &sp); jerr != nil {
			return fmt.Errorf("parse section json: %w; raw=%q", jerr, truncate(content, 160))
		}
		if len(sp.Slides) == 0 {
			return fmt.Errorf("section produced 0 slides")
		}
		return nil
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}
	return sp.Slides, resp.Usage, nil
}

// addUsage accumulates one call's usage into a running total.
func addUsage(total *llm.Usage, u llm.Usage) {
	total.InputTokens += u.InputTokens
	total.OutputTokens += u.OutputTokens
	total.CacheReadTokens += u.CacheReadTokens
	total.CacheCreationTokens += u.CacheCreationTokens
}
