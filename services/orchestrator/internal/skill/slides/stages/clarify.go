// Package stages — clarify.go
//
// Sprint Q: agent-driven clarification. Replaces the Sprint N1
// hardcoded 3-step wizard (scenario → audience → extra info) with a
// planner LLM call that READS the topic and decides 0–3 questions
// to ask. Each topic gets a tailored question set, not a fixed
// script.
//
//	PlanClarification(ctx, router, in) → []ClarificationQuestion
//
// Empty slice = "no questions needed; go straight to outline". The
// caller treats `nil` (LLM error or malformed JSON) identically —
// clarification is advisory; never block planning on it.
package stages

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
)

// ClarificationQuestion is one dynamic question the LLM decided was
// worth asking before drafting the outline. Mirrors the frontend's
// WizardStepView body shape so the existing WizardCard renders these
// without changes.
//
// Kind drives the WizardCard body:
//   - "select"   → radio-card list of Options (each ≤ 12 chars)
//   - "free-text" → text input with Placeholder
//
// Optional flag enables the 跳过 button (typical for free-text;
// rare for select).
type ClarificationQuestion struct {
	Kind        string   `json:"kind"`        // "select" | "free-text" | "blueprint-pick" (BR.2 synthesised)
	Question    string   `json:"question"`    // headline copy, in the user's language
	Options     []string `json:"options,omitempty"`     // for kind=select — chip labels (what user sees)
	Placeholder string   `json:"placeholder,omitempty"` // for kind=free-text
	Optional    bool     `json:"optional"`

	// Sprint BR.2 — when non-nil and same length as Options, the
	// frontend's wizard chip submits OptionValues[i] (e.g. the
	// blueprint ID "series-a-pitch") rather than Options[i] (the
	// human-readable label). Used by the synthesised blueprint-pick
	// step in agent_runner.go to surface labels like "Series A 路演 ·
	// 11 页" while feeding the runtime a clean enum value. LLM never
	// populates this; the LLM-facing prompts/clarify.md doesn't
	// mention it.
	OptionValues []string `json:"option_values,omitempty"`
}

// PlanClarification asks the planner LLM to read the topic + audience
// (if provided) and return 0–3 high-leverage clarifying questions
// tailored to THAT specific topic. Returns the slice; an empty slice
// means "no clarification needed; go straight to outline".
//
// On LLM error or malformed JSON, returns nil + the error — caller
// should LOG and treat as empty (no clarification). Don't block the
// pipeline on a clarifier hiccup.
func PlanClarification(ctx context.Context, router llm.Router, in OutlineParams) ([]ClarificationQuestion, llm.Usage, error) {
	user := fmt.Sprintf(
		"Topic: %s\nAudience (already known, if any): %s\nSlide count: %d\nStyle: %s\n\nReference material (if any):\n%s",
		in.Topic,
		defaultStr(in.Audience, "(none — infer from topic)"),
		in.SlideCount,
		defaultStr(in.Style, "auto"),
		in.ReferenceText,
	)

	client := router.For("planner")
	var qs []ClarificationQuestion
	resp, err := askWithRetry(ctx, client, "clarify", llm.AskToolRequest{
		Model:        router.ModelFor("planner"),
		SystemPrompt: prompts.Clarify,
		Messages:     []schema.Message{schema.NewUser(user)},
		// 2000 is plenty — output is at most 3 small JSON objects.
		// Smaller cap = faster + cheaper than a full outline call.
		MaxTokens:         2000,
		EnablePromptCache: true,
	}, func(content string) error {
		qs = nil
		stripped := stripFences(content)
		if len(stripped) == 0 {
			// Treat blank as "no questions"; ship empty slice.
			return nil
		}
		if err := json.Unmarshal(stripped, &qs); err != nil {
			return fmt.Errorf("parse clarify json: %w; raw=%q", err, truncate(content, 200))
		}
		return nil
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}

	// Defensive cap — the prompt says max 3, but enforce here too so
	// a misbehaving model can't sprawl the wizard.
	if len(qs) > 3 {
		qs = qs[:3]
	}
	// Drop malformed entries (missing kind / question / wrong kind value).
	out := qs[:0]
	for _, q := range qs {
		if q.Question == "" {
			continue
		}
		if q.Kind != "select" && q.Kind != "free-text" {
			continue
		}
		if q.Kind == "select" && len(q.Options) < 2 {
			// A select with <2 options is useless — drop or downgrade.
			continue
		}
		out = append(out, q)
	}
	return out, resp.Usage, nil
}
