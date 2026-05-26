package tools

import (
	"context"
	"encoding/json"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// CriticContent is the Sprint O content-loop tool the agent calls
// AFTER write_content (and before render_deck). It runs
// stages.CriticContent against the just-produced content + the
// original outline so it can flag headline-vs-body mismatches and
// per-slide specificity/voice issues.
//
// Returns a JSON array of {slide, category, issue, fix} notes;
// empty array means "ship the content as-is". The agent uses the
// output to decide whether to call revise_slide on flagged slides
// before render_deck.
type CriticContent struct {
	Router llm.Router
}

func (*CriticContent) Name() string { return "critic_content" }

func (*CriticContent) Description() string {
	return "Review the just-written per-slide content for specificity, voice, completeness, " +
		"or visual-fit issues. Pass the OUTLINE JSON (the same one passed to write_content) and " +
		"the CONTENT JSON returned by write_content. Returns a JSON array of issues (or [] if " +
		"content is solid). Call AFTER write_content. For each non-empty issue's slide, call " +
		"revise_slide(slide_index, ...) before render_deck."
}

func (*CriticContent) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["outline_json", "content_json"],
		"properties": {
			"outline_json": {"type": "string", "description": "The outline JSON (verbatim) the content was written from."},
			"content_json": {"type": "string", "description": "The content JSON returned by write_content."}
		}
	}`)
}

type criticContentArgs struct {
	OutlineJSON string `json:"outline_json"`
	ContentJSON string `json:"content_json"`
}

func (t *CriticContent) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a criticContentArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if a.OutlineJSON == "" || a.ContentJSON == "" {
		return schema.ToolResult{Error: "both outline_json and content_json are required"}, nil
	}
	var outline stages.OutlineResult
	if err := json.Unmarshal([]byte(a.OutlineJSON), &outline); err != nil {
		return schema.ToolResult{Error: "parse outline_json: " + err.Error()}, nil
	}
	var content stages.ContentResult
	if err := json.Unmarshal([]byte(a.ContentJSON), &content); err != nil {
		return schema.ToolResult{Error: "parse content_json: " + err.Error()}, nil
	}

	notes, _, err := stages.CriticContent(ctx, t.Router, &outline, &content)
	if err != nil {
		// Advisory degradation — same pattern as critic_outline.
		out, _ := json.Marshal(map[string]any{
			"notes":   []any{},
			"warning": "critic_content call failed; treating as no issues. err=" + err.Error(),
		})
		return schema.ToolResult{Output: string(out)}, nil
	}
	out, _ := json.Marshal(map[string]any{
		"notes":    notes,
		"is_clean": len(notes) == 0,
	})
	return schema.ToolResult{Output: string(out)}, nil
}
