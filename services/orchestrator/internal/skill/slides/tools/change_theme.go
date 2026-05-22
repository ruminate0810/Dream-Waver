package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// ChangeTheme switches the whole deck to a different template family.
// No LLM call — purely mechanical: write Deck.Theme, override every
// slide.Template, mark every slide dirty, re-render.
//
// Used when the user says things like "把整个 deck 换成 corporate 风".
type ChangeTheme struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*ChangeTheme) Name() string { return "change_theme" }

func (*ChangeTheme) Description() string {
	return "Switch the entire deck to a different template family. Affects all slides at once. Use when the user requests a whole-deck visual style change like \"换成 corporate 风\" or \"用 pitch-deck 模板\". Per-slide layouts are preserved."
}

func (*ChangeTheme) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["theme"],
		"properties": {
			"theme": {
				"type": "string",
				"enum": ["minimalist", "corporate", "pitch-deck", "academic", "playful"],
				"description": "Template family to switch to."
			}
		}
	}`)
}

type changeThemeArgs struct {
	Theme string `json:"theme"`
}

// validThemes mirrors the schema.Theme enum. Hardcoded (not derived from
// the schema package) so the validation error message can be specific.
var validThemes = map[string]schema.Theme{
	"minimalist": schema.ThemeMinimalist,
	"corporate":  schema.ThemeCorporate,
	"pitch-deck": schema.ThemePitchDeck,
	"academic":   schema.ThemeAcademic,
	"playful":    schema.ThemePlayful,
}

func (t *ChangeTheme) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a changeThemeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	theme, ok := validThemes[strings.ToLower(strings.TrimSpace(a.Theme))]
	if !ok {
		return schema.ToolResult{
			Error: fmt.Sprintf("unknown theme %q; valid values: minimalist, corporate, pitch-deck, academic, playful", a.Theme),
		}, nil
	}

	deck, count := t.State.Snapshot()
	if deck == nil || count == 0 {
		return schema.ToolResult{Error: "no deck to re-theme — generate the deck first"}, nil
	}
	if deck.Theme == theme {
		// No-op early return — saves a chromedp render when the user
		// asks for the theme it's already on.
		out, _ := json.Marshal(map[string]any{
			"theme":       string(theme),
			"slide_count": count,
			"pptx_path":   "",
			"message":     fmt.Sprintf("Deck is already using the %s theme; no re-render needed.", theme),
		})
		return schema.ToolResult{Output: string(out)}, nil
	}

	t.State.SetTheme(theme)
	updatedDeck, newCount := t.State.Snapshot()

	dirty := make([]int, newCount)
	for i := 0; i < newCount; i++ {
		dirty[i] = i
	}
	t.State.MarkDirty(dirty...)

	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"theme":       string(theme),
		"slide_count": newCount,
		"pptx_path":   pptxPath,
		"message":     fmt.Sprintf("Deck switched to %s theme; all %d slides re-rendered.", theme, newCount),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
