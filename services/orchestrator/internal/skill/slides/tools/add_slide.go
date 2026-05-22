package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// AddSlide inserts a new slide into the deck at a 1-based position.
// One worker-LLM call (via stages.WriteOneSlide) produces the new
// slide's SlideData; the rest is mechanical.
//
// Used when the user says things like "在第 3 页后加一页讲风险".
type AddSlide struct {
	State    SessionAccessor
	Router   llm.Router
	Renderer IncrementalRenderer
}

func (*AddSlide) Name() string { return "add_slide" }

func (*AddSlide) Description() string {
	return "Insert a new slide into the deck at the given 1-based position. Calls the worker LLM once to write the new slide's content from the user's instruction. position=1 prepends, position=slide_count+1 appends. Use when the user asks for an additional slide (\"加一页\", \"在第 3 页后插一页\")."
}

func (*AddSlide) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["position", "instruction"],
		"properties": {
			"position": {
				"type": "integer",
				"minimum": 1,
				"description": "1-based position the new slide will occupy. 1 = prepend; slide_count+1 = append."
			},
			"instruction": {
				"type": "string",
				"description": "What this slide should be about / contain."
			},
			"layout": {
				"type": "string",
				"enum": ["title", "section", "bullets", "content", "quote", "two-column", "data", "closing"],
				"description": "Optional layout variant. Defaults to \"content\" for body-text slides."
			}
		}
	}`)
}

type addSlideArgs struct {
	Position    int    `json:"position"`
	Instruction string `json:"instruction"`
	Layout      string `json:"layout"`
}

func (t *AddSlide) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a addSlideArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if strings.TrimSpace(a.Instruction) == "" {
		return schema.ToolResult{Error: "instruction is required — describe what the new slide should contain"}, nil
	}
	deck, count := t.State.Snapshot()
	if deck == nil {
		return schema.ToolResult{Error: "no deck to insert into — generate the deck first"}, nil
	}
	if a.Position < 1 || a.Position > count+1 {
		return schema.ToolResult{
			Error: fmt.Sprintf("position %d out of range (valid: 1..%d)", a.Position, count+1),
		}, nil
	}

	layout := schema.SlideLayout(strings.TrimSpace(a.Layout))
	if layout == "" {
		layout = schema.LayoutContent
	}

	// Pick the up-to-two neighboring titles so the worker can match tone.
	neighbors := make([]string, 0, 2)
	insertIdx := a.Position - 1
	if insertIdx-1 >= 0 && insertIdx-1 < len(deck.Slides) {
		neighbors = append(neighbors, deck.Slides[insertIdx-1].Data.Title)
	}
	if insertIdx >= 0 && insertIdx < len(deck.Slides) {
		neighbors = append(neighbors, deck.Slides[insertIdx].Data.Title)
	}

	data, _, err := stages.WriteOneSlide(ctx, t.Router, stages.WriteOneParams{
		DeckTitle:      deck.Title,
		DeckTheme:      deck.Theme,
		Layout:         layout,
		Position:       a.Position,
		Instruction:    a.Instruction,
		NeighborTitles: neighbors,
	})
	if err != nil {
		return schema.ToolResult{Error: "write_one: " + err.Error()}, nil
	}

	newSlide := schema.Slide{
		Template: string(deck.Theme),
		Layout:   layout,
		Data:     *data,
	}
	if err := t.State.InsertSlide(insertIdx, newSlide); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	updatedDeck, newCount := t.State.Snapshot()
	// Everything at or after the insertion point needs a fresh chromedp
	// pass — the inserted slide because it's new, the rest because
	// their 1-based numbering shifted (matters for footers / page
	// numbers if a template renders them).
	dirty := make([]int, 0, newCount-insertIdx)
	for i := insertIdx; i < newCount; i++ {
		dirty = append(dirty, i)
	}
	t.State.MarkDirty(dirty...)

	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"inserted_index": a.Position,
		"slide_count":    newCount,
		"title":          data.Title,
		"pptx_path":      pptxPath,
		"message":        fmt.Sprintf("Inserted slide %d (%q); deck now has %d slides.", a.Position, data.Title, newCount),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
