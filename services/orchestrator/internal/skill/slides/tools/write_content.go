package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// WriteContent is the second tool. It accepts the outline JSON produced by
// plan_outline (un-modified) and asks the worker LLM to fill in each slide's
// final content. The returned JSON is what render_deck consumes; if State
// is set the content is also stashed on SessionState so a follow-up turn
// can mutate a single slide without re-running the whole writer pass.
type WriteContent struct {
	Router llm.Router
	State  SessionAccessor // optional
}

func (*WriteContent) Name() string { return "write_content" }

func (*WriteContent) Description() string {
	return "Fill in the final per-slide content (title/bullets/body/quote/metric) for an outline produced by plan_outline. " +
		"Call this AFTER plan_outline. Pass the outline JSON verbatim as the `outline` argument. " +
		"Returns a content JSON object that must be passed to render_deck."
}

func (*WriteContent) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["outline"],
		"properties": {
			"outline": {
				"type": "object",
				"description": "The full outline JSON returned by plan_outline. Do NOT modify or summarise; pass it verbatim."
			}
		}
	}`)
}

type writeContentArgs struct {
	Outline stages.OutlineResult `json:"outline"`
}

func (t *WriteContent) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a writeContentArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if len(a.Outline.Slides) == 0 {
		return schema.ToolResult{Error: "outline.slides is empty"}, nil
	}

	content, _, err := stages.Content(ctx, t.Router, &a.Outline)
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	if t.State != nil {
		t.State.SetContent(content)
	}

	out, err := json.Marshal(content)
	if err != nil {
		return schema.ToolResult{Error: fmt.Sprintf("marshal content: %v", err)}, nil
	}
	return schema.ToolResult{Output: string(out)}, nil
}
