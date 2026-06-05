package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// EditSVGSlide revises ONE bespoke-SVG slide (mode=svg decks) from a natural-
// language instruction — restyle, re-lay-out, declutter, reword, recolour,
// resize ("把第3页重排清爽点", "封面标题改成 X", "强调这页的数字").
//
// The other edit tools (edit_slide_text / regenerate_slide / convert_layout /
// …) target HTML-template fields (title / bullets / layout) and do nothing
// useful on an SVG slide, whose whole content lives in raw <svg> markup. This
// is the one tool that actually edits SVG slides — so users can iterate on an
// SVG deck in chat instead of regenerating the whole thing.
//
// The re-author step is INJECTED (Revise) so this package needn't import the
// stages package (which owns the SVG author): agent_runner wires it to
// stages.ReviseSVGSlide.
type EditSVGSlide struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
	// Revise re-authors the slide's SVG given (ctx, theme, deckTitle, page,
	// total, currentSVG, instruction) → newSVG. Injected with
	// stages.ReviseSVGSlide.
	Revise func(ctx context.Context, theme, deckTitle string, page, total int, currentSVG, instruction string) (string, error)
}

func (*EditSVGSlide) Name() string { return "edit_svg_slide" }

func (*EditSVGSlide) Description() string {
	return "Revise ONE slide of a bespoke-SVG deck (layout=svg) from a natural-language instruction — restyle, re-lay-out, declutter, reword, recolour, resize text. THIS is the tool for svg-layout slides; the title/bullets/layout/density/style tools do NOT work on them. Re-renders just that slide."
}

func (*EditSVGSlide) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index", "instruction"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based slide to revise."},
			"instruction": {"type": "string", "description": "Plain-language change (Chinese OK), e.g. '这页太挤，重排清爽一点', '把标题改成 X', '强调数字', '配色再克制点'."}
		}
	}`)
}

type editSVGSlideArgs struct {
	SlideIndex  int    `json:"slide_index"`
	Instruction string `json:"instruction"`
}

func (t *EditSVGSlide) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	if t.Revise == nil {
		return schema.ToolResult{Error: "edit_svg_slide is not wired (no reviser)"}, nil
	}
	var a editSVGSlideArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	idx := a.SlideIndex - 1
	deck, count := t.State.Snapshot()
	if idx < 0 || idx >= count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index out of range (have %d slides)", count)}, nil
	}
	cur := deck.Slides[idx]
	if cur.Layout != schema.LayoutSVG || cur.Data.SVG == "" {
		return schema.ToolResult{Error: fmt.Sprintf("slide %d is not an SVG slide — use the regular edit tools for it", a.SlideIndex)}, nil
	}
	theme := string(deck.Theme)
	if theme == "" {
		theme = cur.Template
	}

	newSVG, err := t.Revise(ctx, theme, deck.Title, a.SlideIndex, count, cur.Data.SVG, a.Instruction)
	if err != nil {
		return schema.ToolResult{Error: "revise: " + err.Error()}, nil
	}

	if err := t.State.UpdateSlide(idx, func(s *schema.Slide) {
		s.Layout = schema.LayoutSVG
		s.Data.SVG = newSVG
	}); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	t.State.MarkDirty(idx)

	updatedDeck, _ := t.State.Snapshot()
	pptxPath, rerr := t.Renderer.RenderIncremental(ctx, *updatedDeck, []int{idx})
	if rerr != nil {
		return schema.ToolResult{Error: "rerender: " + rerr.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"slide_index": a.SlideIndex,
		"pptx_path":   pptxPath,
		"message":     fmt.Sprintf("Slide %d revised and re-rendered.", a.SlideIndex),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
