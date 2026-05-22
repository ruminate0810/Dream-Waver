package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// ReorderSlide moves slide [from_position] to [to_position] (both
// 1-based, matching the convention every other slide tool uses).
// Purely mechanical — 0 LLM calls. Triggered by user requests like
// "把第 5 页移到第 2 页前面" or "把封底挪到最后".
type ReorderSlide struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*ReorderSlide) Name() string { return "reorder_slide" }

func (*ReorderSlide) Description() string {
	return "Move one slide to a new position in the deck. Both positions are 1-based. Use for \"把第 5 页移到第 2 页前面\" or \"把第 N 页挪到最后\". The slide's content is preserved; only its position changes."
}

func (*ReorderSlide) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["from_position", "to_position"],
		"properties": {
			"from_position": {"type": "integer", "minimum": 1, "description": "1-based current position of the slide to move."},
			"to_position":   {"type": "integer", "minimum": 1, "description": "1-based target position the slide should occupy after the move."}
		}
	}`)
}

type reorderSlideArgs struct {
	FromPosition int `json:"from_position"`
	ToPosition   int `json:"to_position"`
}

func (t *ReorderSlide) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a reorderSlideArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	_, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck to reorder"}, nil
	}
	from := a.FromPosition - 1
	to := a.ToPosition - 1
	if from < 0 || from >= count || to < 0 || to >= count {
		return schema.ToolResult{Error: fmt.Sprintf("positions out of range (have %d slides)", count)}, nil
	}
	if from == to {
		out, _ := json.Marshal(map[string]any{
			"from_position": a.FromPosition,
			"to_position":   a.ToPosition,
			"slide_count":   count,
			"message":       "No-op: slide already at the target position.",
		})
		return schema.ToolResult{Output: string(out)}, nil
	}

	if err := t.State.ReorderSlide(from, to); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	// Every slide between min(from,to) and max(from,to) inclusive has
	// its 1-based index shifted; mark all of them dirty so footers /
	// page numbers in templates re-render correctly. (Even templates
	// without explicit page numbers benefit — the chromedp pass is
	// idempotent for unchanged content but the WS slides.updated event
	// is what triggers the parent's iframe refresh.)
	updatedDeck, _ := t.State.Snapshot()
	lo, hi := from, to
	if lo > hi {
		lo, hi = hi, lo
	}
	dirty := make([]int, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		dirty = append(dirty, i)
	}
	t.State.MarkDirty(dirty...)

	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"from_position": a.FromPosition,
		"to_position":   a.ToPosition,
		"slide_count":   len(updatedDeck.Slides),
		"pptx_path":     pptxPath,
		"message":       fmt.Sprintf("Slide moved from position %d to %d.", a.FromPosition, a.ToPosition),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
