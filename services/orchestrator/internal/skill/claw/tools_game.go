package claw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// GameMaker is the producer's playable-game capability (V26 批二): one
// natural-language brief in, a hosted play URL out. Backed by the games
// vertical's Pipeline via an adapter in cmd/server that also registers the
// game with the games session store (so /api/v1/games/{id}/play serves it)
// and best-effort persists a game_jobs row. nil when the games pipeline is
// down, which unwires the tool.
type GameMaker interface {
	MakeGame(ctx context.Context, prompt, genre string) (playURL, title, description string, err error)
}

// GenerateGame turns a brief into a playable self-contained HTML mini-game
// and lands the reference in the work package (games tab). The heavy
// artifact (the HTML) lives with the games vertical; claw stores the
// play URL + title, mirroring how decks keep a PreviewID.
type GenerateGame struct {
	Game    GameMaker
	Session *Session
	Emitter event.Emitter
}

func (*GenerateGame) Name() string { return "generate_game" }

func (*GenerateGame) Description() string {
	return "Generate a PLAYABLE self-contained HTML mini-game from a brief (e.g. 贪吃蛇/打砖块/跑酷 " +
		"with a twist). Slow + billed — call once, only when the user explicitly asked for a game. " +
		"The game lands in the work package with a play URL."
}

func (*GenerateGame) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["prompt"],
		"properties": {
			"prompt": {"type": "string", "description": "The game brief in natural language (Chinese OK): mechanics, twist, win/lose condition."},
			"genre":  {"type": "string", "description": "Optional genre hint: arcade | puzzle | platformer."}
		}
	}`)
}

func (t *GenerateGame) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Prompt string `json:"prompt"`
		Genre  string `json:"genre"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return schema.ToolResult{Error: "prompt is required"}, nil
	}
	playURL, title, desc, err := t.Game.MakeGame(ctx, p.Prompt, strings.TrimSpace(p.Genre))
	if err != nil {
		return schema.ToolResult{Error: "generate game: " + err.Error()}, nil
	}
	n := t.Session.AddGame(Game{PlayURL: playURL, Title: title, Description: desc})
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("game", n, 0))
	}
	return schema.ToolResult{Output: fmt.Sprintf(
		"游戏已生成:「%s」(%s) — 已放进作品包,可直接试玩。生成后 terminate。", title, desc)}, nil
}
