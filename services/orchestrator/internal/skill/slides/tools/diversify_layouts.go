package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// DiversifyLayouts is the batch "the deck looks too template-y, break
// it up" tool. Scans the deck for consecutive runs of bullets/content
// slides (the "every page looks the same" smell) and concurrently
// rewrites a subset of them via the worker LLM with explicit
// instruction to pick a MORE SPECIALISED layout (data / quote /
// comparison / multi-metric / pull-quote / icon-grid / etc).
//
// Directly answers "排版太模板化 / 每页长得都一样 / 千篇一律".
// Cheaper than the agent calling regenerate_slide N times one at a
// time — single tool call, concurrent (cap 3), one critic round after.
type DiversifyLayouts struct {
	State    SessionAccessor
	Router   llm.Router
	Renderer IncrementalRenderer
}

func (*DiversifyLayouts) Name() string { return "diversify_layouts" }

func (*DiversifyLayouts) Description() string {
	return "Bulk-rewrite repetitive bullets/content slides into more varied specialised layouts. By default targets any slide in a run of 3+ consecutive bullets/content (the 'every page looks the same' smell). Pass slide_indices to override. The worker LLM is told to PICK a more specialised layout (data / quote / comparison / multi-metric / pull-quote / icon-grid / timeline) that fits each slide's content. Concurrent (cap 3). Use for \"排版太模板化 / 每页都一样\" requests — one tool call replaces N regenerate_slide invocations."
}

func (*DiversifyLayouts) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"slide_indices": {
				"type": "array",
				"items": {"type": "integer", "minimum": 1},
				"description": "Optional explicit list of 1-based slide indices to diversify. When omitted, auto-targets every slide in a run of 3+ consecutive bullets/content layouts (excluding opening title/section/closing)."
			},
			"target_layouts": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Optional explicit list of preferred target layouts (e.g. [\"data\", \"comparison\", \"pull-quote\"]). When omitted, the worker LLM picks from the full specialised set per slide."
			}
		}
	}`)
}

type diversifyLayoutsArgs struct {
	SlideIndices  []int    `json:"slide_indices,omitempty"`
	TargetLayouts []string `json:"target_layouts,omitempty"`
}

func (t *DiversifyLayouts) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a diversifyLayoutsArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	deck, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck to diversify"}, nil
	}

	// Build target list.
	var targets []int
	if len(a.SlideIndices) > 0 {
		for _, oneBased := range a.SlideIndices {
			if oneBased < 1 || oneBased > count {
				return schema.ToolResult{Error: fmt.Sprintf("slide_index %d out of range (have %d slides)", oneBased, count)}, nil
			}
			targets = append(targets, oneBased-1)
		}
	} else {
		targets = findRepetitiveRuns(deck.Slides)
	}

	if len(targets) == 0 {
		out, _ := json.Marshal(map[string]any{
			"slide_count": count,
			"diversified": []int{},
			"message":     "No repetitive layout runs found — deck already has enough variety. Use explicit slide_indices to force-diversify specific pages.",
		})
		return schema.ToolResult{Output: string(out)}, nil
	}

	// Build the preferred-layouts hint for the prompt.
	allowedHint := "data, quote, comparison, multi-metric, pull-quote, icon-grid, timeline, bento-grid, swot"
	if len(a.TargetLayouts) > 0 {
		allowedHint = strings.Join(a.TargetLayouts, ", ")
	}

	type slot struct {
		index0 int
		data   *schema.SlideData
		layout schema.SlideLayout
		err    error
	}
	results := make([]slot, len(targets))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup

	for i, idx0 := range targets {
		wg.Add(1)
		go func(i, idx0 int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i] = slot{index0: idx0, err: ctx.Err()}
				return
			}

			src := deck.Slides[idx0]
			neighborTitles := []string{}
			if idx0 > 0 {
				neighborTitles = append(neighborTitles, deck.Slides[idx0-1].Data.Title)
			}
			if idx0+1 < len(deck.Slides) {
				neighborTitles = append(neighborTitles, deck.Slides[idx0+1].Data.Title)
			}

			instruction := diversifyInstruction(src, allowedHint)
			// Send Layout="" so the worker LLM PICKS the new layout
			// itself rather than being forced into the current one.
			// WriteOneSlide defaults to LayoutContent in that case, but
			// the prompt directive in `instruction` overrides that.
			params := stages.WriteOneParams{
				DeckTitle:      deck.Title,
				DeckTheme:      deck.Theme,
				Layout:         "",
				Position:       idx0 + 1,
				Instruction:    instruction,
				NeighborTitles: neighborTitles,
			}
			data, _, err := stages.WriteOneSlide(ctx, t.Router, params)
			if err != nil {
				results[i] = slot{index0: idx0, err: err}
				return
			}
			// The worker LLM may not set Data.Style fields; preserve any
			// existing per-slide style override. Layout comes from the
			// worker LLM's response shape, inferred from which fields it
			// populated. If the worker LLM provided a layout enum
			// elsewhere (it doesn't on the SlideData type directly), we'd
			// pick it up here; for now infer from populated fields.
			layout := inferLayoutFromData(*data, src.Layout)
			results[i] = slot{index0: idx0, data: data, layout: layout}
		}(i, idx0)
	}
	wg.Wait()

	var (
		dirty   []int
		ok      []map[string]any
		failed  []map[string]any
	)
	for _, r := range results {
		oneBased := r.index0 + 1
		if r.err != nil {
			failed = append(failed, map[string]any{"slide": oneBased, "error": r.err.Error()})
			continue
		}
		if r.data == nil {
			failed = append(failed, map[string]any{"slide": oneBased, "error": "worker LLM returned nil data"})
			continue
		}
		newData := *r.data
		// Preserve resolved images so we don't reset hero photos.
		if newData.Image == "" {
			newData.Image = deck.Slides[r.index0].Data.Image
		}
		if newData.ImageCredit == "" {
			newData.ImageCredit = deck.Slides[r.index0].Data.ImageCredit
		}
		newLayout := r.layout

		if err := t.State.UpdateSlide(r.index0, func(s *schema.Slide) {
			s.Data = newData
			if newLayout != "" {
				s.Layout = newLayout
			}
		}); err != nil {
			failed = append(failed, map[string]any{"slide": oneBased, "error": "update slide: " + err.Error()})
			continue
		}
		dirty = append(dirty, r.index0)
		ok = append(ok, map[string]any{
			"slide":      oneBased,
			"old_layout": string(deck.Slides[r.index0].Layout),
			"new_layout": string(newLayout),
		})
	}

	if len(dirty) == 0 {
		out, _ := json.Marshal(map[string]any{
			"slide_count": count,
			"failed":      failed,
			"message":     fmt.Sprintf("diversify_layouts: all %d candidate slides failed; no changes applied.", len(targets)),
		})
		return schema.ToolResult{Error: "all diversify attempts failed; see failed list", Output: string(out)}, nil
	}

	t.State.MarkDirty(dirty...)
	updatedDeck, _ := t.State.Snapshot()
	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	rerErr := ""
	if err != nil {
		rerErr = "rerender: " + err.Error()
	}

	msg := fmt.Sprintf("Diversified %d slide(s).", len(ok))
	if len(failed) > 0 {
		msg += fmt.Sprintf(" %d failed.", len(failed))
	}
	if rerErr != "" {
		msg += " Render had partial failure: " + rerErr
	}

	out, _ := json.Marshal(map[string]any{
		"slide_count": count,
		"diversified": ok,
		"failed":      failed,
		"pptx_path":   pptxPath,
		"message":     msg,
	})
	return schema.ToolResult{Output: string(out)}, nil
}

// findRepetitiveRuns returns indices of slides inside any run of 3+
// consecutive bullets/content layouts. Opening title/section and
// closing slides are skipped from candidacy. Returned indices are
// 0-based.
func findRepetitiveRuns(slides []schema.Slide) []int {
	if len(slides) < 3 {
		return nil
	}
	isCandidate := func(l schema.SlideLayout) bool {
		return l == schema.LayoutBullets || l == schema.LayoutContent
	}
	isStructural := func(l schema.SlideLayout) bool {
		return l == schema.LayoutTitle || l == schema.LayoutSection || l == schema.LayoutClosing
	}

	var out []int
	runStart := -1
	for i := 0; i <= len(slides); i++ {
		var l schema.SlideLayout
		if i < len(slides) {
			l = slides[i].Layout
		}
		if i < len(slides) && isCandidate(l) && !isStructural(l) {
			if runStart < 0 {
				runStart = i
			}
		} else {
			// End of a candidate run.
			if runStart >= 0 && i-runStart >= 3 {
				for j := runStart; j < i; j++ {
					out = append(out, j)
				}
			}
			runStart = -1
		}
	}
	return out
}

// inferLayoutFromData picks a layout from a SlideData based on which
// fields the worker LLM populated. Used because stages.WriteOneSlide
// returns just SlideData (no layout enum), and the worker LLM is
// supposed to pick a fitting layout via the populated fields.
func inferLayoutFromData(d schema.SlideData, fallback schema.SlideLayout) schema.SlideLayout {
	switch {
	case d.HTML != "":
		return schema.LayoutHTML
	case d.Code != "":
		return schema.LayoutCode
	case len(d.Tasks) >= 3:
		return schema.LayoutChecklist
	case d.Quote != "" && d.Body != "":
		return schema.LayoutPullQuote
	case d.Quote != "":
		return schema.LayoutQuote
	case len(d.Metrics) >= 2:
		return schema.LayoutMultiMetric
	case d.Metric != "":
		return schema.LayoutData
	case len(d.Events) >= 3:
		return schema.LayoutTimeline
	case len(d.LeftItems) > 0 && len(d.RightItems) > 0:
		return schema.LayoutComparison
	case len(d.TableHeaders) > 0:
		return schema.LayoutComparisonTable
	case len(d.Sections) > 0:
		return schema.LayoutTOC
	case len(d.Strengths) > 0 || len(d.Weaknesses) > 0:
		return schema.LayoutSWOT
	case len(d.Steps) >= 3:
		return schema.LayoutProcessFlow
	case len(d.BentoCards) >= 4:
		return schema.LayoutBentoGrid
	case len(d.Features) >= 3:
		return schema.LayoutIconGrid
	case len(d.TeamMembers) >= 3:
		return schema.LayoutTeamRoster
	case d.BeforeImageQuery != "" && d.AfterImageQuery != "":
		return schema.LayoutBeforeAfter
	case len(d.ImageQueries) >= 3:
		return schema.LayoutImageGrid
	case d.ImageQuery != "" && len(d.Bullets) == 0 && d.Body == "":
		return schema.LayoutPhotoEssay
	case d.ImageQuery != "":
		return schema.LayoutSplitImage
	case d.Body != "" && len(d.Bullets) > 0:
		return schema.LayoutContent
	case len(d.Bullets) > 0:
		return schema.LayoutBullets
	}
	return fallback
}

// diversifyInstruction builds the per-slide rewrite prompt asking the
// worker LLM to pick a more specialised layout.
func diversifyInstruction(s schema.Slide, allowedHint string) string {
	var sb strings.Builder
	sb.WriteString("Rewrite this slide using a MORE SPECIALISED layout. Keep the same TOPIC and KEY MESSAGE — only the visual treatment changes.\n\n")
	sb.WriteString("CURRENT slide (boring " + string(s.Layout) + " layout — that's the problem):\n")
	if s.Data.Title != "" {
		sb.WriteString("  title: " + s.Data.Title + "\n")
	}
	if s.Data.Body != "" {
		sb.WriteString("  body: " + s.Data.Body + "\n")
	}
	if len(s.Data.Bullets) > 0 {
		sb.WriteString("  bullets:\n")
		for _, b := range s.Data.Bullets {
			sb.WriteString("    - " + b + "\n")
		}
	}
	sb.WriteString("\nPICK ONE of these layouts (whichever BEST fits the content):\n")
	sb.WriteString("  Preferred set: " + allowedHint + "\n\n")
	sb.WriteString("Selection guidance:\n")
	sb.WriteString("  - Has a key number or % → data (1 big metric) OR multi-metric (2-4)\n")
	sb.WriteString("  - Has a strong quotable line → pull-quote (with context body)\n")
	sb.WriteString("  - Has 'A vs B' / 'before/after' / 'pros/cons' → comparison\n")
	sb.WriteString("  - Has a timeline / steps → timeline (with dates) OR process-flow (no dates)\n")
	sb.WriteString("  - Has 3+ similar feature bullets → icon-grid\n")
	sb.WriteString("  - Has mixed quick-look items → bento-grid (1 large + 3-4 small)\n")
	sb.WriteString("  - Has SW/OT strategic content → swot\n\n")
	sb.WriteString("Fill the new layout's REQUIRED fields properly (4-5 items where applicable). NEVER respond with bullets/content as the new layout.\n")
	return sb.String()
}
