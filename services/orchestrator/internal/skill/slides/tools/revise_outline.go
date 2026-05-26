package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// ReviseOutline is the Sprint O outline-loop tool the agent calls
// AFTER critic_outline returns a non-empty notes array. It re-runs
// stages.Outline (the planner) with the critic notes appended to the
// user prompt as "Reviewer notes to address" — the new outline is
// the planner's best attempt at fixing each issue.
//
// Returns the revised outline JSON in the same shape as plan_outline.
// Saves to State.SetOutline so the rest of the agent loop reads the
// fresh version.
type ReviseOutline struct {
	Router  llm.Router
	Emitter event.Emitter
	State   SessionAccessor
}

func (*ReviseOutline) Name() string { return "revise_outline" }

func (*ReviseOutline) Description() string {
	return "Re-plan the outline with critic notes addressed. Pass the SAME args you gave " +
		"plan_outline (topic, audience, slide_count, style) PLUS the critic notes from " +
		"critic_outline (the verbatim `notes` array from its output). The planner returns " +
		"a fresh outline JSON that addresses each note. Use AFTER critic_outline returns " +
		"a non-empty notes array; SKIP if critic_outline returned []."
}

func (*ReviseOutline) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["topic", "critic_notes_json"],
		"properties": {
			"topic":             {"type": "string", "description": "Same as plan_outline."},
			"audience":          {"type": "string"},
			"slide_count":       {"type": "integer", "minimum": 3, "maximum": 40},
			"style":             {"type": "string"},
			"reference_text":    {"type": "string"},
			"critic_notes_json": {"type": "string", "description": "The verbatim JSON array of critic notes from critic_outline's output.notes field."}
		}
	}`)
}

type reviseOutlineArgs struct {
	stages.OutlineParams
	CriticNotesJSON string `json:"critic_notes_json"`
}

func (t *ReviseOutline) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a reviseOutlineArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if a.Topic == "" {
		return schema.ToolResult{Error: "topic is required"}, nil
	}
	if a.CriticNotesJSON == "" {
		return schema.ToolResult{Error: "critic_notes_json is required — pass the verbatim `notes` array from critic_outline"}, nil
	}
	var notes []stages.CriticNote
	if err := json.Unmarshal([]byte(a.CriticNotesJSON), &notes); err != nil {
		return schema.ToolResult{Error: "parse critic_notes_json: " + err.Error()}, nil
	}
	if len(notes) == 0 {
		return schema.ToolResult{Error: "critic_notes_json is empty — there's nothing to revise. Skip this tool when critic returned []."}, nil
	}

	params := a.OutlineParams
	params.CriticNotes = notes
	outline, _, err := stages.Outline(ctx, t.Router, params)
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	// Re-emit the outline event so the frontend's count + title update.
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewOutline(outline.Title, len(outline.Slides)))
	}
	if t.State != nil {
		t.State.SetOutline(outline)
	}

	out, err := json.Marshal(outline)
	if err != nil {
		return schema.ToolResult{Error: fmt.Sprintf("marshal revised outline: %v", err)}, nil
	}
	return schema.ToolResult{Output: string(out)}, nil
}
