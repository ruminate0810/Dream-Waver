package tools

import (
	"context"
	"encoding/json"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// CriticDeck is the Sprint O edit-turn reflection tool. The agent
// calls it AFTER any content-changing edit tool (regenerate_slide,
// add_slide, edit_slide_text, delete_slide, etc.) to check whether
// the edit accomplished the user's goal AND that nothing else
// regressed.
//
// Unlike critic_outline / critic_content (which review just-written
// LLM output before render), critic_deck operates on the live
// SessionState.Deck snapshot — so it sees the actual post-edit deck.
// The user_instruction + agent_summary args tell the critic what
// goal it's checking against.
//
// Returns a JSON array of {slide, category, issue, fix} notes.
// Empty array = the edit landed cleanly, the agent should terminate.
// Non-empty = the agent should address each issue (typically with a
// follow-up regenerate_slide or apply_brand call) before terminate.
type CriticDeck struct {
	State  SessionAccessor
	Router llm.Router
}

func (*CriticDeck) Name() string { return "critic_deck" }

func (*CriticDeck) Description() string {
	return "Review the full deck AFTER an edit to verify the change satisfied the user's " +
		"request AND didn't cause a regression elsewhere (lost brand, voice drift, broken " +
		"visual rhythm, etc.). ALWAYS call this AFTER any content-changing tool — " +
		"regenerate_slide, add_slide, delete_slide, edit_slide_text, apply_brand — and BEFORE " +
		"terminate. Returns a JSON array of issues; if non-empty, address each fix before " +
		"terminating. Each fix names a specific edit tool with args — call them."
}

func (*CriticDeck) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["user_instruction"],
		"properties": {
			"user_instruction": {"type": "string", "description": "The verbatim user request that triggered this edit turn (e.g. '把第 3 页改成红色')."},
			"agent_summary":    {"type": "string", "description": "1-2 sentence summary of what you've done so far this turn (e.g. 'Called regenerate_slide(3, \"red palette\")')."}
		}
	}`)
}

type criticDeckArgs struct {
	UserInstruction string `json:"user_instruction"`
	AgentSummary    string `json:"agent_summary"`
}

func (t *CriticDeck) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a criticDeckArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if a.UserInstruction == "" {
		return schema.ToolResult{Error: "user_instruction is required so the critic knows what to check against"}, nil
	}
	deck, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck in session — call this AFTER a deck exists"}, nil
	}

	notes, _, err := stages.CriticDeck(ctx, t.Router, deck, a.UserInstruction, a.AgentSummary)
	if err != nil {
		// Advisory degradation — same pattern as the other critics.
		out, _ := json.Marshal(map[string]any{
			"notes":   []any{},
			"warning": "critic_deck call failed; treating as clean. err=" + err.Error(),
		})
		return schema.ToolResult{Output: string(out)}, nil
	}
	out, _ := json.Marshal(map[string]any{
		"notes":    notes,
		"is_clean": len(notes) == 0,
		"message": func() string {
			if len(notes) == 0 {
				return "Deck looks good — safe to terminate."
			}
			return "Critic flagged issues. Address each fix before terminating."
		}(),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
