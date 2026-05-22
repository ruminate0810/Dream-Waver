package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// SetFooter sets the footer text on one slide OR all slides. The
// "all" form is the common case — users typically want the same
// footer ("Confidential · Q1 2026") on every slide; we expose a
// per-slide variant for cases like "only the title page should show
// the company name". 0 LLM calls.
type SetFooter struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*SetFooter) Name() string { return "set_footer" }

func (*SetFooter) Description() string {
	return "Set the footer text. Omit slide_index to apply to every slide (the common case: \"页脚加 'Q1 2026'\"). Provide slide_index to scope it to one slide. Use empty text to clear."
}

func (*SetFooter) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["text"],
		"properties": {
			"text":        {"type": "string", "description": "Footer text. Empty string clears the footer."},
			"slide_index": {"type": "integer", "minimum": 1, "description": "Optional 1-based slide to scope to. Omit to apply to every slide."}
		}
	}`)
}

type setFooterArgs struct {
	Text       string `json:"text"`
	SlideIndex int    `json:"slide_index"` // 0 = all slides (Go's zero value works because we treat <1 as "all")
}

func (t *SetFooter) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a setFooterArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	deck, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck"}, nil
	}
	text := strings.TrimSpace(a.Text)

	var dirty []int
	if a.SlideIndex >= 1 {
		// Single slide
		idx := a.SlideIndex - 1
		if idx >= count {
			return schema.ToolResult{Error: fmt.Sprintf("slide_index %d out of range (have %d)", a.SlideIndex, count)}, nil
		}
		if err := t.State.UpdateSlide(idx, func(s *schema.Slide) { s.Data.Footer = text }); err != nil {
			return schema.ToolResult{Error: err.Error()}, nil
		}
		dirty = []int{idx}
	} else {
		// All slides
		for i := 0; i < count; i++ {
			if err := t.State.UpdateSlide(i, func(s *schema.Slide) { s.Data.Footer = text }); err != nil {
				return schema.ToolResult{Error: err.Error()}, nil
			}
			dirty = append(dirty, i)
		}
	}
	t.State.MarkDirty(dirty...)

	updatedDeck, _ := t.State.Snapshot()
	_ = deck // ensure tool side-effects ordering is intuitive
	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	scope := "all slides"
	if a.SlideIndex >= 1 {
		scope = fmt.Sprintf("slide %d", a.SlideIndex)
	}
	out, _ := json.Marshal(map[string]any{
		"text":        text,
		"scope":       scope,
		"slide_count": count,
		"pptx_path":   pptxPath,
		"message":     fmt.Sprintf("Footer set on %s.", scope),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
