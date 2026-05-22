package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// DuplicateSlide deep-copies slide N (1-based) and inserts the copy
// immediately after, so it occupies position N+1. The deck grows by
// one. Mostly used as a starting point for a near-identical slide the
// user wants to then tweak ("复制第 3 页", "再来一页类似的").
//
// 0 LLM calls. Slides after the insertion point get re-rendered
// because their 1-based indices shifted.
type DuplicateSlide struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*DuplicateSlide) Name() string { return "duplicate_slide" }

func (*DuplicateSlide) Description() string {
	return "Insert a deep copy of slide N immediately after it. Use for \"复制第 3 页\" or \"再来一页类似的\". The user typically then asks for a tweak on the duplicate via edit_slide_text or regenerate_slide."
}

func (*DuplicateSlide) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based slide to duplicate. The copy is inserted at slide_index+1."}
		}
	}`)
}

type duplicateSlideArgs struct {
	SlideIndex int `json:"slide_index"`
}

func (t *DuplicateSlide) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a duplicateSlideArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	_, count := t.State.Snapshot()
	idx := a.SlideIndex - 1
	if idx < 0 || idx >= count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index %d out of range (have %d slides)", a.SlideIndex, count)}, nil
	}

	if err := t.State.DuplicateSlide(idx); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	// Both the duplicate and every following slide need a fresh render
	// (their 1-based numbering shifted).
	updatedDeck, newCount := t.State.Snapshot()
	dirty := make([]int, 0, newCount-idx-1+1) // idx+1 (the dup) ... newCount-1
	for i := idx + 1; i < newCount; i++ {
		dirty = append(dirty, i)
	}
	t.State.MarkDirty(dirty...)

	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"source_index":   a.SlideIndex,
		"inserted_index": a.SlideIndex + 1,
		"slide_count":    newCount,
		"pptx_path":      pptxPath,
		"message":        fmt.Sprintf("Duplicated slide %d to position %d; deck now has %d slides.", a.SlideIndex, a.SlideIndex+1, newCount),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
