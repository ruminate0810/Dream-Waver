// Package tools holds the slides-domain tools the SlidesAgent uses to
// drive a deck generation through a ToolCallAgent loop. Each tool wraps
// one stage function from internal/skill/slides/stages so the deterministic
// Pipeline and the agent path share the same underlying primitives.
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

// PlanOutline is the first tool the agent calls. It runs stages.Outline,
// emits a slides.outline event so the frontend can flip the Composition
// phase to "done", persists the outline on SessionState so follow-up
// edits can re-read it, and returns the outline JSON for the LLM to feed
// into write_content next.
type PlanOutline struct {
	Router  llm.Router
	Emitter event.Emitter
	State   SessionAccessor // optional; if set, the outline is also saved here
}

func (*PlanOutline) Name() string { return "plan_outline" }

func (*PlanOutline) Description() string {
	return "Plan the deck's structure: chapters, headlines, and a 3–5 bullet outline per slide. " +
		"Call this FIRST, before any other tool. Returns an outline JSON object that must be " +
		"passed unchanged to write_content next."
}

func (*PlanOutline) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["topic"],
		"properties": {
			"topic":          {"type": "string", "description": "Subject of the deck. May be a phrase or a paragraph; Chinese OK."},
			"audience":       {"type": "string", "description": "Who is reading the deck (e.g. 'VC 投资人', 'undergrad students')."},
			"slide_count":    {"type": "integer", "minimum": 3, "maximum": 40, "description": "How many slides total. Defaults to 8."},
			"style":          {"type": "string", "description": "Style hint, e.g. 'minimalist', 'corporate', 'pitch-deck', 'academic', 'playful'."},
			"reference_text": {"type": "string", "description": "Optional source material the planner should anchor the outline to."},
			"blueprint_id":   {"type": "string", "description": "Optional blueprint id (e.g. 'series-a-pitch') to use as a fixed slide-sequence skeleton. Must come from the user's wizard pick; do not invent. The tool ALSO reads this from session state and will override args if the user picked one — passing it explicitly is best practice but not required."}
		}
	}`)
}

// blueprintProvider is an optional interface extension for SessionAccessor.
// SessionState implements it (Sprint BR.2). Lets the tool defensively
// override blueprint_id from session state in case the LLM forgets to
// pass it on the JSON args — the user-picked blueprint must apply
// regardless of LLM compliance.
type blueprintProvider interface {
	BlueprintID() string
}

func (t *PlanOutline) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p stages.OutlineParams
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if p.Topic == "" {
		return schema.ToolResult{Error: "topic is required"}, nil
	}
	// Sprint BR.2 — defensive blueprint override from session state.
	// If the user picked a blueprint at the wizard gate, we MUST honour
	// it regardless of whether the LLM remembered to pass blueprint_id
	// in args. Session state is the source of truth.
	if t.State != nil {
		if bp, ok := t.State.(blueprintProvider); ok {
			if id := bp.BlueprintID(); id != "" {
				p.BlueprintID = id
			}
		}
	}

	outline, _, err := stages.Outline(ctx, t.Router, p)
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	// Surface the high-level signal the frontend uses to transition phases
	// (it doesn't subscribe to tool.* events for this).
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewOutline(outline.Title, len(outline.Slides)))
	}
	// Persist for follow-up edit tools.
	if t.State != nil {
		t.State.SetOutline(outline)
	}

	out, err := json.Marshal(outline)
	if err != nil {
		return schema.ToolResult{Error: fmt.Sprintf("marshal outline: %v", err)}, nil
	}
	return schema.ToolResult{Output: string(out)}, nil
}
