package tools

import (
	"context"
	"encoding/json"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// CriticOutline is the Sprint O outline-loop tool the agent calls
// AFTER plan_outline. It runs stages.CriticOutline against the just-
// produced outline and returns a JSON array of {slide, category,
// issue, fix} notes — empty array means "ship the outline as-is".
//
// The agent uses the output to decide whether to call revise_outline
// next (when notes is non-empty) or terminate the outline phase
// (when notes is []).
type CriticOutline struct {
	Router llm.Router
}

func (*CriticOutline) Name() string { return "critic_outline" }

func (*CriticOutline) Description() string {
	return "Review the just-planned outline for structural, specificity, voice, visual-fit, " +
		"or completeness issues. Pass the OUTLINE JSON from plan_outline verbatim plus the " +
		"original topic/audience. Returns a JSON array of issues (or [] if the outline is solid). " +
		"Call this AFTER plan_outline. If the array is non-empty, call revise_outline to address " +
		"each one before moving on to write_content."
}

func (*CriticOutline) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["outline_json"],
		"properties": {
			"outline_json": {"type": "string", "description": "The verbatim JSON outline returned by plan_outline."},
			"topic":        {"type": "string", "description": "The original deck topic (for audience-alignment checks)."},
			"audience":     {"type": "string", "description": "The intended audience."},
			"slide_count":  {"type": "integer", "description": "Requested slide count (for completeness check)."},
			"style":        {"type": "string", "description": "Style preference."}
		}
	}`)
}

type criticOutlineArgs struct {
	OutlineJSON string `json:"outline_json"`
	Topic       string `json:"topic"`
	Audience    string `json:"audience"`
	SlideCount  int    `json:"slide_count"`
	Style       string `json:"style"`
}

func (t *CriticOutline) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a criticOutlineArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if a.OutlineJSON == "" {
		return schema.ToolResult{Error: "outline_json is required"}, nil
	}
	var outline stages.OutlineResult
	if err := json.Unmarshal([]byte(a.OutlineJSON), &outline); err != nil {
		return schema.ToolResult{Error: "parse outline_json: " + err.Error()}, nil
	}

	notes, _, err := stages.CriticOutline(ctx, t.Router, stages.OutlineParams{
		Topic:      a.Topic,
		Audience:   a.Audience,
		SlideCount: a.SlideCount,
		Style:      a.Style,
	}, &outline)
	if err != nil {
		// Critic is advisory — surface the failure but return [] so the
		// agent treats it as "no issues" and ships the outline.
		out, _ := json.Marshal(map[string]any{
			"notes":   []any{},
			"warning": "critic_outline call failed; treating as no issues. err=" + err.Error(),
		})
		return schema.ToolResult{Output: string(out)}, nil
	}
	out, _ := json.Marshal(map[string]any{
		"notes":    notes,
		"is_clean": len(notes) == 0,
	})
	return schema.ToolResult{Output: string(out)}, nil
}
