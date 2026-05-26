package games

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// Sprint O.5 — pre-generation planning for the games skill.
//
// Before the worker LLM writes 12k tokens of HTML, a quick planner
// call produces a structured design brief (pitch / mechanics /
// controls / win-condition / art) and emits it as a `game.plan`
// event. The frontend renders this as a card so the user sees
// "what is the agent building" instead of staring at a blank
// loading state for 30-60 seconds.
//
// MVP — informational only, no approval gate. Generation continues
// straight after the plan emits. Adding an approve_plan HILT gate
// would mean copying slides' PendingUserAction state machine
// (worth doing as a separate sprint when users actually want to
// edit the plan before HTML generation).

// GamePlanView is what the planner LLM produces. The frontend
// mirrors this shape in transport.tsx (GamePlanView). Keep them
// in sync — drift = silent UI breakage.
type GamePlanView struct {
	// One-line statement of what the game IS. Italic in the card.
	// Example: "Snake, but each food eaten speeds up by 10% and the
	// map edges wrap."
	Pitch string `json:"pitch"`
	// Bullet-style strings. 2-5 entries. Each is a short imperative
	// describing one mechanic.
	Mechanics []string `json:"mechanics"`
	// Bullet-style strings. 1-3 entries. Keyboard / touch controls.
	Controls []string `json:"controls"`
	// One-line description of win condition. Optional — some games
	// (sandbox, endless) don't have one.
	WinCondition string `json:"win_condition,omitempty"`
	// One-line description of how the player loses.
	LossCondition string `json:"loss_condition,omitempty"`
	// Palette / mood / visual references. One short paragraph.
	ArtDirection string `json:"art_direction,omitempty"`
	// Planner's settled-on genre + difficulty (passes through from
	// user input or "auto" if the LLM picked).
	Genre      string `json:"genre,omitempty"`
	Difficulty string `json:"difficulty,omitempty"`
}

// planSystemPrompt steers the planner LLM toward a tight JSON
// response. We use the worker tier (already in-process) rather
// than spinning up a separate planner tier — saves a model dial
// and the planner output is short.
func planSystemPrompt(genre, aesthetic, difficulty string) string {
	hints := []string{}
	if genre != "" {
		hints = append(hints, "用户选了 genre = "+genre)
	}
	if aesthetic != "" {
		hints = append(hints, "用户选了 aesthetic = "+aesthetic)
	}
	if difficulty != "" {
		hints = append(hints, "难度 = "+difficulty)
	}
	hintsLine := ""
	if len(hints) > 0 {
		hintsLine = "\n\nHints from the user: " + strings.Join(hints, "； ")
	}

	return `You are a senior game designer planning a single-file HTML5 web game (canvas + JS, no external deps) before the author writes any code.

The user has described what they want. Your output is a tight design brief — strictly JSON, no prose around it, no markdown fences. Schema:

{
  "pitch": string,           // ONE sentence. What is this game in plain language? Mention the twist.
  "mechanics": [string],     // 2-5 bullets. Core gameplay loop, special mechanics, edge cases.
  "controls": [string],      // 1-3 bullets. Keys / touch. Example: "Arrow keys: move snake"
  "win_condition": string,   // optional. Empty string if endless / sandbox.
  "loss_condition": string,  // optional. Empty string if no fail state.
  "art_direction": string,   // optional. Palette + mood + reference. 1-2 sentences.
  "genre": string,           // pitch genre if user didn't specify ("arcade" | "puzzle" | "platformer" | "shooter" | "rogue").
  "difficulty": string       // "easy" | "normal" | "hard". Pass through if user specified.
}

Rules:
- Be SPECIFIC. "Snake game" is bad; "Snake with 10% speed-up per food and edge-wraparound" is good.
- Mechanics are gameplay rules, not technical notes. Don't say "use requestAnimationFrame".
- Controls are what the PLAYER does, in one short imperative each.
- Output JSON only. No markdown. No commentary. No surrounding text.` + hintsLine
}

// planGame runs the planner LLM call and returns a structured plan.
// On error we log + return nil; the caller falls through to
// generation without emitting a plan event (better silent than
// blocking the user behind a planner blip).
func (p *Pipeline) planGame(ctx context.Context, history []schema.Message, genre, aesthetic, difficulty string) *GamePlanView {
	worker := p.Router.For("worker")
	model := p.Router.ModelFor("worker")

	resp, err := worker.AskTool(ctx, llm.AskToolRequest{
		Model:        model,
		SystemPrompt: planSystemPrompt(genre, aesthetic, difficulty),
		Messages:     history,
		// Small cap — plan is structured + short. Burning a full
		// context here is wasteful.
		MaxTokens:   1024,
		Temperature: 0.4,
	})
	if err != nil {
		slog.WarnContext(ctx, "game plan call failed; skipping plan event", "err", err)
		return nil
	}

	plan, perr := parsePlan(resp.Content)
	if perr != nil {
		slog.WarnContext(ctx, "game plan parse failed; skipping plan event",
			"err", perr, "raw", truncateForEvent(resp.Content, 200))
		return nil
	}

	// Pass through user-specified fields if the LLM dropped them.
	// Belt + braces — the system prompt asks for these but the LLM
	// sometimes returns empty strings.
	if plan.Genre == "" && genre != "" {
		plan.Genre = genre
	}
	if plan.Difficulty == "" && difficulty != "" {
		plan.Difficulty = difficulty
	}
	return plan
}

// parsePlan tolerates a stray markdown fence — the planner is told
// NOT to wrap output, but model occasionally backslides.
func parsePlan(raw string) (*GamePlanView, error) {
	s := strings.TrimSpace(raw)
	// Strip ```json ... ``` if present.
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}
	// Grab first {...} block if there's leading prose.
	if i := strings.Index(s, "{"); i > 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}

	var v GamePlanView
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("plan json: %w", err)
	}
	if strings.TrimSpace(v.Pitch) == "" && len(v.Mechanics) == 0 {
		return nil, fmt.Errorf("plan is effectively empty")
	}
	return &v, nil
}

// emitPlan ships the plan as a game.plan event. Caller decides
// whether to call this (only on cold-start, not on follow-up
// edits — those already have full conversational context).
func (p *Pipeline) emitPlan(ctx context.Context, plan *GamePlanView) {
	if plan == nil {
		return
	}
	p.emit(ctx, event.NewGamePlan(plan))
}
