package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// AuthorSVG is the Sprint SV-1 stage: given an approved outline + a
// theme, it asks the planner LLM to author one bespoke <svg> per slide.
// Returns a ContentResult whose slides each carry Layout=svg and
// Data.SVG set; the rest of the pipeline (Assemble → render) treats it
// like any other deck.
//
// The theme's concrete tokens are substituted into the prompt so the
// LLM picks palette-correct colors + the right font stacks. The SV-2
// measure-repair loop wraps this; SV-1 ships the single-pass version.
func AuthorSVG(ctx context.Context, router llm.Router, outline *OutlineResult, theme string) (*ContentResult, llm.Usage, error) {
	if outline == nil || len(outline.Slides) == 0 {
		return nil, llm.Usage{}, fmt.Errorf("AuthorSVG: outline is empty")
	}
	tok := themetokens.Get(theme)
	sys := renderSVGPrompt(tok)

	var user strings.Builder
	fmt.Fprintf(&user, "Deck title: %s\nTheme: %s\n\nSlides:\n", outline.Title, theme)
	for _, s := range outline.Slides {
		fmt.Fprintf(&user, "\n%d. [%s] %s\n", s.Index, s.Type, s.Headline)
		if len(s.KeyPoints) > 0 && string(s.KeyPoints) != "null" {
			fmt.Fprintf(&user, "   points: %s\n", string(s.KeyPoints))
		}
		if strings.TrimSpace(s.SpeakerNotes) != "" {
			fmt.Fprintf(&user, "   note: %s\n", s.SpeakerNotes)
		}
	}

	client := router.For("planner")
	var slides []ContentSlide
	resp, err := askWithRetry(ctx, client, "svg", llm.AskToolRequest{
		Model:        router.ModelFor("planner"),
		SystemPrompt: sys,
		Messages:     []schema.Message{schema.NewUser(user.String())},
		// SVG markup is verbose — one slide can run 1.5K chars. Budget
		// generously so a 10-slide deck doesn't trip finish_reason=length.
		MaxTokens:         16000,
		EnablePromptCache: true,
	}, func(content string) error {
		slides = nil
		var parsed struct {
			Slides []struct {
				SVG string `json:"svg"`
			} `json:"slides"`
		}
		if err := json.Unmarshal(stripFences(content), &parsed); err != nil {
			return fmt.Errorf("parse svg json: %w; raw=%q", err, truncate(content, 200))
		}
		if len(parsed.Slides) == 0 {
			return fmt.Errorf("svg author produced 0 slides")
		}
		for i, s := range parsed.Slides {
			svg := strings.TrimSpace(s.SVG)
			if svg == "" {
				return fmt.Errorf("slide %d: empty svg", i+1)
			}
			slides = append(slides, ContentSlide{
				Index:    i + 1,
				Template: theme,
				Layout:   schema.LayoutSVG,
				Data:     schema.SlideData{SVG: svg},
			})
		}
		return nil
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}
	return &ContentResult{Slides: slides}, resp.Usage, nil
}

// renderSVGPrompt substitutes the theme's concrete tokens into the
// prompts.SVG template's {{TOKEN}} placeholders.
func renderSVGPrompt(t themetokens.Tokens) string {
	darkness := "LIGHT (dark text on light background)"
	contrast := "Use " + t.FG + " for text on the light background; reserve the accent for emphasis."
	if t.Dark {
		darkness = "DARK (light text on dark background)"
		contrast = "This is a DARK slide — ALL text must be light (" + t.FG + " or " + t.FGMuted + "). NEVER put dark text on the dark background."
	}
	r := strings.NewReplacer(
		"{{BG}}", t.BG,
		"{{FG}}", t.FG,
		"{{FG_MUTED}}", t.FGMuted,
		"{{ACCENT}}", t.Accent,
		"{{DARKNESS}}", darkness,
		"{{CONTRAST_RULE}}", contrast,
		"{{FONT_DISPLAY}}", t.FontDisp,
		"{{FONT_BODY}}", t.FontBody,
		"{{FONT_MONO}}", t.FontMono,
	)
	return r.Replace(prompts.SVG)
}
