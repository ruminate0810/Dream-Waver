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

// RewriteForDensity is the batch "make every sparse slide fuller"
// tool. Scans the deck for bullets / content layouts whose Data fails
// the density bar (≤ 3 bullets OR body < 40 chars) and concurrently
// rewrites each one via the worker LLM under explicit "fill the
// canvas" instruction (mirroring prompts/content.md density rules).
//
// Directly answers user complaints like "每页太空 / 排版不饱满 /
// 内容少 / 看起来没写完". Cheaper than the agent calling
// regenerate_slide N times one at a time:
//   - Single tool call, one critic_deck round after.
//   - Concurrency 3 caps parallel LLM calls — 5 sparse slides land in
//     ~10-15s wall vs ~30-50s sequential.
//   - Per-slide failures are isolated; partial successes keep the
//     other slides intact.
type RewriteForDensity struct {
	State    SessionAccessor
	Router   llm.Router
	Renderer IncrementalRenderer
}

func (*RewriteForDensity) Name() string { return "rewrite_for_density" }

func (*RewriteForDensity) Description() string {
	return "Bulk-rewrite all sparse slides to be denser, following the per-layout density rules in content.md (bullets layout = 4-5 bullets at 12-18 words; content = body 40-90 words + 2-3 supporting bullets). By default targets every layout=bullets/content slide whose data fails the density bar (≤ 3 bullets OR body < 40 chars). Pass slide_indices to override the auto-target list. Concurrent LLM calls (cap 3) so a 5-sparse-slide deck rewrites in ~10-15s instead of ~30-50s sequential. Use for \"整体太空 / 每页内容少 / 把所有页都填满\" requests — single tool call replaces N regenerate_slide invocations."
}

func (*RewriteForDensity) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"slide_indices": {
				"type": "array",
				"items": {"type": "integer", "minimum": 1},
				"description": "Optional explicit list of 1-based slide indices to rewrite. When omitted, the tool auto-detects every bullets/content slide that fails the density bar."
			},
			"instruction_hint": {
				"type": "string",
				"description": "Optional extra guidance appended to the per-slide rewrite prompt (e.g. \"focus on concrete numbers and product names; avoid generic adjectives\"). Keep it short — long instructions dilute the density rules."
			}
		}
	}`)
}

type rewriteForDensityArgs struct {
	SlideIndices    []int  `json:"slide_indices,omitempty"`
	InstructionHint string `json:"instruction_hint,omitempty"`
}

func (t *RewriteForDensity) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a rewriteForDensityArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	deck, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck to rewrite"}, nil
	}

	// Build target list (0-based indices).
	var targets []int
	if len(a.SlideIndices) > 0 {
		for _, oneBased := range a.SlideIndices {
			if oneBased < 1 || oneBased > count {
				return schema.ToolResult{Error: fmt.Sprintf("slide_index %d out of range (have %d slides)", oneBased, count)}, nil
			}
			targets = append(targets, oneBased-1)
		}
	} else {
		// Auto-detect sparse slides.
		for i, s := range deck.Slides {
			if isSparse(s) {
				targets = append(targets, i)
			}
		}
	}

	if len(targets) == 0 {
		out, _ := json.Marshal(map[string]any{
			"slide_count":   count,
			"rewritten":     []int{},
			"skipped_dense": count,
			"message":       "No sparse slides found — every slide already passes the density bar. Use explicit slide_indices if you want to force-rewrite specific pages.",
		})
		return schema.ToolResult{Output: string(out)}, nil
	}

	// Concurrent rewrite. errgroup not available; semaphore via buffered chan.
	type slot struct {
		index0 int
		data   *schema.SlideData
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

			instruction := densityInstruction(src, a.InstructionHint)
			params := stages.WriteOneParams{
				DeckTitle:      deck.Title,
				DeckTheme:      deck.Theme,
				Layout:         src.Layout,
				Position:       idx0 + 1,
				Instruction:    instruction,
				NeighborTitles: neighborTitles,
			}
			data, _, err := stages.WriteOneSlide(ctx, t.Router, params)
			results[i] = slot{index0: idx0, data: data, err: err}
		}(i, idx0)
	}
	wg.Wait()

	// Apply successful rewrites + track dirty indices.
	var (
		dirty    []int
		ok       []int
		failed   []map[string]any
		writeErr error
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
		// Preserve image fields — the worker LLM doesn't re-pick them.
		// (image_query may be on the new data; resolved Image url stays
		// from the current deck so we don't reset the hero photo.)
		newData := *r.data
		if newData.Image == "" {
			newData.Image = deck.Slides[r.index0].Data.Image
		}
		if newData.ImageCredit == "" {
			newData.ImageCredit = deck.Slides[r.index0].Data.ImageCredit
		}

		if err := t.State.UpdateSlide(r.index0, func(s *schema.Slide) {
			s.Data = newData
		}); err != nil {
			failed = append(failed, map[string]any{"slide": oneBased, "error": "update slide: " + err.Error()})
			continue
		}
		dirty = append(dirty, r.index0)
		ok = append(ok, oneBased)
	}

	if len(dirty) == 0 {
		out, _ := json.Marshal(map[string]any{
			"slide_count": count,
			"failed":      failed,
			"message":     fmt.Sprintf("rewrite_for_density: all %d candidate slides failed; no changes applied.", len(targets)),
		})
		return schema.ToolResult{Error: "all rewrites failed; see failed list", Output: string(out)}, nil
	}

	t.State.MarkDirty(dirty...)
	updatedDeck, _ := t.State.Snapshot()
	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	if err != nil {
		writeErr = fmt.Errorf("rerender: %w", err)
	}

	msg := fmt.Sprintf("Rewrote %d slide(s) for density: %v", len(ok), ok)
	if len(failed) > 0 {
		msg += fmt.Sprintf(". %d failed.", len(failed))
	}
	if writeErr != nil {
		msg += " Render had partial failure: " + writeErr.Error()
	}

	out, _ := json.Marshal(map[string]any{
		"slide_count": count,
		"rewritten":   ok,
		"failed":      failed,
		"pptx_path":   pptxPath,
		"message":     msg,
	})
	return schema.ToolResult{Output: string(out)}, nil
}

// isSparse returns true when a slide's layout is bullets/content AND
// its content falls under the density floor (per content.md):
//   - bullets layout with ≤ 3 bullets
//   - content layout with body < 40 chars AND ≤ 3 bullets
//   - either layout with completely empty data
func isSparse(s schema.Slide) bool {
	if s.Layout != schema.LayoutBullets && s.Layout != schema.LayoutContent {
		return false
	}
	bodyChars := len(strings.TrimSpace(s.Data.Body))
	bulletCount := len(s.Data.Bullets)
	if s.Layout == schema.LayoutBullets {
		return bulletCount <= 3
	}
	// content layout: needs body ≥ 40 OR bullets ≥ 4 to qualify as dense
	return bodyChars < 40 && bulletCount <= 3
}

// densityInstruction builds the per-slide rewrite prompt. Spells out
// the layout-specific density floor explicitly so the worker LLM can't
// fall back to its old habit of writing 2 short bullets.
func densityInstruction(s schema.Slide, extraHint string) string {
	var sb strings.Builder
	sb.WriteString("Rewrite this slide to be DENSER — FILL THE CANVAS. Keep the same TOPIC and layout; only the content shape changes.\n\n")
	sb.WriteString("CURRENT slide content:\n")
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
	sb.WriteString("\nDENSITY RULES (HARD):\n")
	switch s.Layout {
	case schema.LayoutBullets:
		sb.WriteString("  - bullets layout: write EXACTLY 4-5 bullets (not 2-3).\n")
		sb.WriteString("  - Each bullet must be 12-18 words.\n")
		sb.WriteString("  - Each bullet must add NEW information — not a rephrasing of the title.\n")
		sb.WriteString("  - Cite specific numbers, names, comparisons where possible.\n")
	case schema.LayoutContent:
		sb.WriteString("  - content layout: write a body paragraph of 50-90 words.\n")
		sb.WriteString("  - PLUS 2-3 supporting bullets of 10-15 words each.\n")
		sb.WriteString("  - Body sets context; bullets carry proof points.\n")
	}
	if extraHint = strings.TrimSpace(extraHint); extraHint != "" {
		sb.WriteString("\nADDITIONAL GUIDANCE: " + extraHint + "\n")
	}
	return sb.String()
}
