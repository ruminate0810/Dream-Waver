package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// SplitSlide cuts slide [slide_index] into two pages at bullet
// boundary [split_after]. The first half keeps the title/body/image
// + bullets[:split_after]; the second half is a clone with bullets[
// split_after:] and " · 续" (or " (cont.)" for English decks) appended
// to the title. No LLM call.
//
// Use for "把第 3 页拆成两页" or "split slide 4 after the 3rd bullet".
// CONSTRAINT: source must be bullets / content layout with ≥ 2 bullets.
type SplitSlide struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*SplitSlide) Name() string { return "split_slide" }

func (*SplitSlide) Description() string {
	return "Split ONE slide into two consecutive pages at a bullet boundary. The first page keeps the title/body/image + bullets[:split_after]; the second page clones the styling with bullets[split_after:] and ' · 续' (Chinese) or ' (cont.)' (English) appended to the title. No LLM call. Use for \"把第 3 页拆成两页\" or when a slide has too many bullets to read comfortably. CONSTRAINT: source must be bullets / content layout with at least 2 bullets."
}

func (*SplitSlide) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index", "split_after"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based index of the slide to split."},
			"split_after": {"type": "integer", "minimum": 1, "description": "Number of bullets to keep on the FIRST resulting page. Must be between 1 and (bullet_count - 1) so both halves get at least one bullet."}
		}
	}`)
}

type splitSlideArgs struct {
	SlideIndex int `json:"slide_index"`
	SplitAfter int `json:"split_after"`
}

func (t *SplitSlide) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a splitSlideArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	_, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck to split"}, nil
	}
	if a.SlideIndex < 1 || a.SlideIndex > count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index out of range (have %d slides)", count)}, nil
	}
	idx0 := a.SlideIndex - 1

	if err := t.State.SplitSlide(idx0, a.SplitAfter); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	updatedDeck, newCount := t.State.Snapshot()
	// New slide landed at idx0+1; everything from idx0 onwards has
	// shifted (well, idx0 has new bullet trim, idx0+1 is the new
	// slide, and everything after has shifted +1). Mark all dirty.
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
		"original_index":  a.SlideIndex,
		"new_second_page": a.SlideIndex + 1,
		"split_after":     a.SplitAfter,
		"slide_count":     newCount,
		"pptx_path":       pptxPath,
		"message": fmt.Sprintf(
			"Slide %d split into pages %d + %d. First page keeps %d bullet(s); second page gets the rest. Deck is now %d pages.",
			a.SlideIndex, a.SlideIndex, a.SlideIndex+1, a.SplitAfter, newCount,
		),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
