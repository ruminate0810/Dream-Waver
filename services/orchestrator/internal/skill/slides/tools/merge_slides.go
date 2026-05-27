package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// MergeSlides folds slide [slide_index+1] into [slide_index] and
// removes the trailing slide. Purely mechanical — no LLM call.
// Triggered by user requests like "把第 3 和第 4 页合成一页" or
// "merge slide 3 into 4".
//
// HARD constraint: both slides MUST be layout=bullets OR layout=content.
// Specialised layouts (data / quote / timeline etc) have field shapes
// that don't compose cleanly. The tool returns an error pointing the
// agent at convert_layout + retry, OR delete_slide + edit_slide_text.
type MergeSlides struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*MergeSlides) Name() string { return "merge_slides" }

func (*MergeSlides) Description() string {
	return "Merge slide [slide_index+1] INTO slide [slide_index], then remove the trailing one. The resulting slide takes [slide_index]'s title and visual treatment; bodies are concatenated; bullets are appended (capped at 8). No LLM call — purely a struct mutation. Use for \"把第 3 和第 4 页合成一页\" or when two adjacent bullet lists should become one denser page. CONSTRAINT: both slides must be bullets / content layout — for specialised layouts use convert_layout first."
}

func (*MergeSlides) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based index of the FIRST slide. The slide at slide_index+1 will be merged into it and then deleted."}
		}
	}`)
}

type mergeSlidesArgs struct {
	SlideIndex int `json:"slide_index"`
}

func (t *MergeSlides) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a mergeSlidesArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	_, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck to merge"}, nil
	}
	if a.SlideIndex < 1 || a.SlideIndex >= count {
		return schema.ToolResult{Error: fmt.Sprintf(
			"slide_index out of range (need 1..%d for an %d-slide deck — the slide at slide_index+1 must exist)",
			count-1, count,
		)}, nil
	}
	idx0 := a.SlideIndex - 1

	if err := t.State.MergeSlides(idx0); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	// Every slide from idx0 onwards now has its 1-based position
	// shifted by -1 (since one was removed). Mark idx0 + all the
	// subsequent ones dirty so footers / page numbers re-render.
	updatedDeck, newCount := t.State.Snapshot()
	dirty := make([]int, 0, newCount-idx0)
	for i := idx0; i < newCount; i++ {
		dirty = append(dirty, i)
	}
	t.State.MarkDirty(dirty...)

	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"merged_into":  a.SlideIndex,
		"removed":      a.SlideIndex + 1,
		"slide_count":  newCount,
		"pptx_path":    pptxPath,
		"message": fmt.Sprintf(
			"Slide %d folded into slide %d. Deck is now %d pages (was %d).",
			a.SlideIndex+1, a.SlideIndex, newCount, count,
		),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
