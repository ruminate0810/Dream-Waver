package design

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// Cross-session memory extraction + consolidation (Sprint BB), adapted
// from mem0's two-phase Extract → Update(A.U.D.N.) design.
//
// mem0 splits into (1) extract candidate facts then (2) for each
// candidate, vector-retrieve similar memories and pick ADD/UPDATE/
// DELETE/NOOP. At mem0's scale (thousands of memories) that retrieval
// is essential. At OURS (dozens of design facts per workspace) we fold
// both phases into ONE LLM call: hand the model the recent conversation
// AND the full existing memory set, and have it return the reconciled
// actions directly. No embeddings, no vector store — we already inject
// every memory into context, so there's nothing to retrieve.
//
// What counts as a design memory: durable preferences + constraints +
// recurring subjects — "prefers minimalist", "brand color is #FF6B6B",
// "the mascot is a fox named Pip", "always 16:9 for social". NOT
// one-off prompt content ("a sunset over mountains" is not a memory).

// MemoryFact is one existing memory row handed to the consolidator.
type MemoryFact struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	// Source "manual" facts are user-pinned — the model is told never
	// to DELETE or UPDATE them (it may still ADD related auto facts).
	Source string `json:"source"`
}

// MemoryActions is the reconciliation result. IDs in Update/Delete
// reference existing MemoryFact.ID values; Add is a list of brand-new
// fact strings.
type MemoryActions struct {
	Add    []string            `json:"add"`
	Update []MemoryUpdateAction `json:"update"`
	Delete []string            `json:"delete"`
}

type MemoryUpdateAction struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// ExtractAndConsolidate runs the single-call pass. Returns the actions
// the caller should apply to the store. On any LLM/parse failure it
// returns an empty MemoryActions (no-op) and logs — memory is a
// best-effort enhancement, never a hard dependency on the user's flow.
func ExtractAndConsolidate(
	ctx context.Context,
	router llm.Router,
	recentPrompts []string,
	existing []MemoryFact,
) MemoryActions {
	none := MemoryActions{}
	if router == nil || len(recentPrompts) == 0 {
		return none
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	existingJSON, _ := json.Marshal(existing)
	req := llm.AskToolRequest{
		Model:        router.ModelFor("planner"),
		SystemPrompt: memorySystemPrompt,
		Messages: []schema.Message{
			schema.NewUser(memoryUserMessage(recentPrompts, string(existingJSON))),
		},
		Tools:       []schema.ToolSchema{memoryReconcileTool()},
		ToolChoice:  "auto",
		MaxTokens:   700,
		Temperature: 0.1,
	}
	client := router.For("planner")
	resp, err := client.AskTool(ctx, req)
	if err != nil {
		slog.WarnContext(ctx, "design memory extract failed", "err", err)
		return none
	}
	if len(resp.ToolCalls) == 0 {
		return none // model decided nothing to change
	}
	var actions MemoryActions
	if len(resp.ToolCalls[0].Args) > 0 {
		if err := json.Unmarshal(resp.ToolCalls[0].Args, &actions); err != nil {
			slog.WarnContext(ctx, "design memory decode failed", "err", err)
			return none
		}
	}
	return sanitizeActions(actions, existing)
}

// sanitizeActions enforces the invariants the prompt asks for, defen-
// sively (LLMs drift): never delete/update a manual fact, drop empty
// adds, cap total adds per pass to keep memory from ballooning.
func sanitizeActions(a MemoryActions, existing []MemoryFact) MemoryActions {
	manual := map[string]bool{}
	known := map[string]bool{}
	for _, f := range existing {
		known[f.ID] = true
		if f.Source == "manual" {
			manual[f.ID] = true
		}
	}
	out := MemoryActions{}
	for _, s := range a.Add {
		s = strings.TrimSpace(s)
		if s != "" && len(out.Add) < 8 {
			out.Add = append(out.Add, s)
		}
	}
	for _, u := range a.Update {
		if known[u.ID] && !manual[u.ID] && strings.TrimSpace(u.Content) != "" {
			out.Update = append(out.Update, MemoryUpdateAction{ID: u.ID, Content: strings.TrimSpace(u.Content)})
		}
	}
	for _, id := range a.Delete {
		if known[id] && !manual[id] {
			out.Delete = append(out.Delete, id)
		}
	}
	return out
}

const memorySystemPrompt = `You maintain a small, durable memory of a user's DESIGN preferences across sessions — the way ChatGPT remembers facts about you.

A memory is a SHORT, durable fact that should influence FUTURE image generations:
  - Style preferences: "prefers minimalist, flat illustration", "dislikes gradients"
  - Brand constraints: "brand color is #FF6B6B", "logo font is Inter", "always 16:9 for social cards"
  - Recurring subjects: "designing for a coffee shop called Acme", "mascot is a fox named Pip"
  - Workflow preferences: "usually wants 4 variants", "prefers the Pro model"

NOT memories (ignore these):
  - One-off prompt content ("a sunset over mountains") — that's a request, not a durable fact.
  - Anything transient or specific to a single image.

You are given the user's RECENT prompts and the EXISTING memory set (with ids + source).
Decide the minimal set of changes and call the reconcile tool:
  - add:    brand-new durable facts not already covered. Keep each ONE short sentence.
  - update: when a recent prompt refines/extends an existing AUTO fact (give its id + new content).
  - delete: when an existing AUTO fact is now contradicted or obsolete (give its id).

Hard rules:
  - NEVER update or delete a fact whose source is "manual" — those are user-pinned.
  - Prefer NOOP: if the recent prompts contain no durable new preference, return empty arrays.
  - Don't duplicate an existing fact. Merge instead of adding near-duplicates.
  - At most a few changes per call. Quality over quantity.`

func memoryUserMessage(recentPrompts []string, existingJSON string) string {
	sb := strings.Builder{}
	sb.WriteString("RECENT PROMPTS (newest last):\n")
	for _, p := range recentPrompts {
		sb.WriteString("  - ")
		sb.WriteString(p)
		sb.WriteString("\n")
	}
	sb.WriteString("\nEXISTING MEMORY (json array of {id, content, source}):\n")
	sb.WriteString(existingJSON)
	sb.WriteString("\n\nReturn the minimal reconcile actions. If nothing durable changed, return empty arrays.")
	return sb.String()
}

func memoryReconcileTool() schema.ToolSchema {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"add": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Brand-new durable facts (each one short sentence).",
			},
			"update": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":      map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
					},
					"required": []string{"id", "content"},
				},
				"description": "Refinements to existing AUTO facts (id + new content).",
			},
			"delete": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Ids of existing AUTO facts now obsolete/contradicted.",
			},
		},
		"required": []string{"add", "update", "delete"},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		panic(fmt.Sprintf("memory tool schema marshal: %v", err))
	}
	return schema.ToolSchema{
		Name:        "reconcile_memory",
		Description: "Apply the minimal Add/Update/Delete changes to the user's design memory.",
		Parameters:  raw,
	}
}
