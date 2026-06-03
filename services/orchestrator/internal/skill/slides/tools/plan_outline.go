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

// ReferenceRetriever is the minimum the PlanOutline tool needs to
// fetch RAG inspiration. Implemented in slides/agent_runner.go as a
// thin adapter over store.ReferenceDecks — keeps the tools package
// import-free of internal/store.
//
// Returns at most K reference outlines, ordered by relevance. Empty
// result is a normal outcome (no corpus match) and the planner falls
// back to non-RAG generation.
type ReferenceRetriever interface {
	RetrieveOutlines(ctx context.Context, topic, scenario, blueprintID string, k int) ([]stages.ReferenceOutline, error)
}

// PlanOutline is the first tool the agent calls. It runs stages.Outline,
// emits a slides.outline event so the frontend can flip the Composition
// phase to "done", persists the outline on SessionState so follow-up
// edits can re-read it, and returns the outline JSON for the LLM to feed
// into write_content next.
type PlanOutline struct {
	Router  llm.Router
	Emitter event.Emitter
	State   SessionAccessor    // optional; if set, the outline is also saved here
	Refs    ReferenceRetriever // optional (BR.3); when set, retrieves top-K reference outlines
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

// refStasher is an optional interface extension on SessionAccessor.
// SessionState implements it (Sprint BR.3). Lets the tool stash the
// retrieved reference slugs so runFromOutline can re-attach them on
// state.Outline after the critic-revise loop wipes the omitempty
// fields (revise_outline's LLM doesn't know about ReferenceSlugs).
type refStasher interface {
	SetReferenceSlugs(slugs []string)
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

	// Sprint BR.3 — RAG inspiration. Fetch top-2 reference outlines
	// matching the topic + scenario + blueprint and let stages.Outline
	// inject them as soft inspiration. Retrieval failure is non-fatal
	// — the planner just runs without RAG context.
	var refSlugs []string
	if t.Refs != nil {
		// Use the blueprint id as the scenario hint too; reference rows'
		// scenario field is typically the same vocabulary
		// (e.g. "pitch"). Keep K small — the planner prompt budget is
		// already crowded with blueprint + critic notes.
		refs, rerr := t.Refs.RetrieveOutlines(ctx, p.Topic, p.BlueprintID, p.BlueprintID, 2)
		if rerr != nil {
			// Log but proceed — references are advisory.
			// (no slog import in this file; the adapter logs detail.)
			refs = nil
		}
		if len(refs) > 0 {
			p.References = refs
			for _, r := range refs {
				refSlugs = append(refSlugs, r.Slug)
			}
			// Stash on session state so runFromOutline can re-attach
			// after the critic-revise loop — the LLM doesn't know about
			// the OutlineResult.ReferenceSlugs field and would otherwise
			// emit an outline JSON that drops it.
			if t.State != nil {
				if rs, ok := t.State.(refStasher); ok {
					rs.SetReferenceSlugs(refSlugs)
				}
			}
		}
	}

	outline, _, err := stages.Outline(ctx, t.Router, p)
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	// Attach attribution AFTER the LLM returns — the LLM didn't see
	// these fields (BR.3) and they're for the FE/critic only.
	if len(refSlugs) > 0 {
		outline.ReferenceSlugs = refSlugs
	}
	if p.BlueprintID != "" {
		outline.BlueprintID = p.BlueprintID
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
