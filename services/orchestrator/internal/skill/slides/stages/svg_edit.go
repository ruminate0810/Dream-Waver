package stages

import (
	"context"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/svgicons"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// ReviseSVGSlide re-authors ONE slide's bespoke SVG to satisfy a user edit
// instruction ("这页太挤，重排清爽点", "把标题改成 X", "强调数字") while keeping
// the rest of the slide intact, then re-inlines icons and re-applies the
// deterministic chrome (gradient background + uniform footer) so the edited
// page still matches the deck. Returns the revised SVG, or an error (the
// caller keeps the original on error).
//
// This is what lets users iterate on an SVG-mode deck in chat: the edit-turn
// tool edit_svg_slide injects this as its re-author function (keeping the
// tools package free of a stages import).
func ReviseSVGSlide(ctx context.Context, router llm.Router, theme, deckTitle string, page, total int, currentSVG, instruction string) (string, error) {
	if strings.TrimSpace(currentSVG) == "" {
		return "", fmt.Errorf("slide has no SVG to revise")
	}
	if strings.TrimSpace(instruction) == "" {
		return "", fmt.Errorf("empty instruction")
	}
	tok := themetokens.Get(theme)
	sys := renderSVGPrompt(tok)

	var user strings.Builder
	user.WriteString("Revise THIS existing slide to satisfy the user's request below, keeping its skeleton, palette, and all UNRELATED content exactly the same. Make a focused, surgical change — do NOT redesign the whole slide unless the request explicitly asks to re-lay-out / declutter / 重排.\n\nUser request:\n")
	user.WriteString(instruction)
	user.WriteString("\n\nReturn STRICT JSON — NO markdown fences:\n{\"svg\":\"<svg viewBox='0 0 1920 1080' xmlns='http://www.w3.org/2000/svg' width='1920' height='1080'>…</svg>\"}\nThe full revised SVG.\n\nCurrent SVG:\n")
	user.WriteString(currentSVG)

	client := router.For("svg_author")
	resp, err := client.AskTool(ctx, llm.AskToolRequest{
		Model:             router.ModelFor("svg_author"),
		SystemPrompt:      sys,
		Messages:          []schema.Message{schema.NewUser(user.String())},
		MaxTokens:         12000,
		EnablePromptCache: true,
	})
	if err != nil {
		return "", err
	}
	svg := parseOneSVG(resp.Content)
	if strings.TrimSpace(svg) == "" {
		return "", fmt.Errorf("revise returned no <svg>")
	}
	svg = svgicons.Inline(svg, tok.FGMuted)
	svg = finalizeSVGChrome(svg, tok, deckTitle, page, total)
	if !QASlideUsable(svg) {
		return "", fmt.Errorf("revised svg failed QA")
	}
	return svg, nil
}
