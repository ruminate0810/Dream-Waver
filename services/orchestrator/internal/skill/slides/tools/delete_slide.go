package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// DeleteSlide removes one slide from the deck. No LLM call needed; the
// renderer just re-assembles the PPTX with one fewer slide. Indices of
// subsequent slides shift down by 1, so we mark every slide >= the
// removed index as dirty (their preview URLs effectively renumber).
type DeleteSlide struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*DeleteSlide) Name() string { return "delete_slide" }

func (*DeleteSlide) Description() string {
	return "Remove one slide from the deck. After deletion every later slide shifts up by one position. " +
		"Use sparingly — most edits should mutate a slide via edit_slide_text or regenerate_slide instead."
}

func (*DeleteSlide) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based slide to remove."}
		}
	}`)
}

type deleteSlideArgs struct {
	SlideIndex int `json:"slide_index"`
}

func (t *DeleteSlide) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a deleteSlideArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	idx := a.SlideIndex - 1
	_, count := t.State.Snapshot()
	if idx < 0 || idx >= count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index out of range (have %d slides)", count)}, nil
	}

	if err := t.State.DeleteSlide(idx); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	// Every slide from `idx` onward effectively moves up — their preview
	// pages need to be re-numbered, so mark them all dirty.
	updatedDeck, newCount := t.State.Snapshot()
	dirty := make([]int, 0, newCount-idx)
	for i := idx; i < newCount; i++ {
		dirty = append(dirty, i)
	}
	t.State.MarkDirty(dirty...)

	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	// Always make sure schema.Deck reflects post-deletion state.
	_ = updatedDeck

	out, _ := json.Marshal(map[string]any{
		"deleted_index": a.SlideIndex,
		"slide_count":   newCount,
		"pptx_path":     pptxPath,
		"message":       fmt.Sprintf("Slide %d deleted; deck now has %d slides.", a.SlideIndex, newCount),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
