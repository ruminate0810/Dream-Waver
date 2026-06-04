package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/svgicons"
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
		// Rich SVG markup is verbose — a ppt-master-grade slide (gradients,
		// shadows, decorative shapes) can run 3-4K chars. Budget generously
		// so a 10-slide deck doesn't trip finish_reason=length.
		MaxTokens:         28000,
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
			svg = svgicons.Inline(svg, tok.FGMuted) // PM-1: resolve <use data-icon>
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

// svgRepairSystem is the focused fix prompt: given one slide's SVG +
// its measured layout violations, return a corrected SVG. Kept terse —
// the LLM already knows the SVG conventions from authoring; this just
// asks for a surgical fix.
const svgRepairSystem = `You are fixing layout bugs in ONE slide's SVG (1920×1080 canvas). You will receive the current SVG and a list of measured layout problems (text overflowing safe margins, or two texts overlapping).

Return STRICT JSON, no fences: { "svg": "<svg …>…</svg>" }

Rules:
- Fix EVERY listed problem. Overflow → shorten the text, reduce its font-size, or hand-break it into more <tspan x="…" dy="1.2em"> lines that stay within x:[120,1800] y:[120,1000]. Overlap → move one element so the two no longer collide.
- Change as LITTLE as possible — keep the same composition, colors, fonts, and overall design. Only adjust what's needed to resolve the problems.
- Preserve the full-bleed background rect and the viewBox="0 0 1920 1080".
- Same forbidden tags as before: no <image>/<foreignObject>/<script>.`

// RepairSVGSlides runs the SV-2 measure-repair loop: render every slide,
// measure text bboxes, and for any slide with violations, ask the LLM to
// surgically fix that slide's SVG. Repeats up to maxRounds; each round
// only re-measures the slides it just repaired. Best-effort — a slide
// that still has violations after maxRounds ships as-is (better a slight
// overrun than a failed deck). Returns the same ContentResult with
// repaired SVGs written back in place.
//
// onRound (optional) is called once per round with a short status string
// for event-stream surfacing; nil disables it.
func RepairSVGSlides(ctx context.Context, router llm.Router, theme string, content *ContentResult, maxRounds int, onRound func(string)) (*ContentResult, error) {
	if content == nil || len(content.Slides) == 0 {
		return content, nil
	}
	if maxRounds <= 0 {
		maxRounds = 2
	}
	// indices we still need to check; start with all.
	pending := make([]int, len(content.Slides))
	for i := range content.Slides {
		pending[i] = i
	}

	for round := 1; round <= maxRounds && len(pending) > 0; round++ {
		svgs := make([]string, len(pending))
		for k, idx := range pending {
			svgs[k] = content.Slides[idx].Data.SVG
		}
		viol, err := MeasureSVGs(ctx, svgs, theme)
		if err != nil {
			// measurement infra failed — don't block the deck.
			return content, nil
		}
		var stillBad []int
		repaired := 0
		for k, idx := range pending {
			vs := viol[k]
			if len(vs) == 0 {
				continue
			}
			fixed, rerr := repairOneSVG(ctx, router, theme, content.Slides[idx].Data.SVG, vs)
			if rerr != nil || strings.TrimSpace(fixed) == "" {
				continue // keep original; will not re-check
			}
			content.Slides[idx].Data.SVG = fixed
			stillBad = append(stillBad, idx)
			repaired++
		}
		if onRound != nil {
			onRound(fmt.Sprintf("svg repair round %d: %d slide(s) fixed", round, repaired))
		}
		if repaired == 0 {
			break // nothing fixable left
		}
		// Re-measure only the slides we just changed next round.
		pending = stillBad
	}
	return content, nil
}

// repairOneSVG asks the LLM to fix a single slide's SVG given its
// violations. Returns the corrected SVG markup.
func repairOneSVG(ctx context.Context, router llm.Router, theme, svg string, vs []Violation) (string, error) {
	tok := themetokens.Get(theme)
	user := fmt.Sprintf(
		"Theme palette: bg=%s fg=%s muted=%s accent=%s\n\nCurrent SVG:\n%s\n\nMeasured layout problems to fix:\n%s",
		tok.BG, tok.FG, tok.FGMuted, tok.Accent, svg, formatViolations(vs),
	)
	client := router.For("planner")
	var out string
	_, err := askWithRetry(ctx, client, "svg-repair", llm.AskToolRequest{
		Model:        router.ModelFor("planner"),
		SystemPrompt: svgRepairSystem,
		Messages:     []schema.Message{schema.NewUser(user)},
		MaxTokens:    4000,
	}, func(content string) error {
		var parsed struct {
			SVG string `json:"svg"`
		}
		if err := json.Unmarshal(stripFences(content), &parsed); err != nil {
			return fmt.Errorf("parse repair json: %w", err)
		}
		if strings.TrimSpace(parsed.SVG) == "" {
			return fmt.Errorf("repair returned empty svg")
		}
		out = parsed.SVG
		return nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

// renderSVGPrompt substitutes the theme's full spec_lock tokens into the
// ppt-master-grade prompts.SVGMaster template (Sprint PM). The richer
// token set (surface / border / accent-2 / pattern / mood) lets the LLM
// author gradients, depth, and motifs that match the theme.
func renderSVGPrompt(t themetokens.Tokens) string {
	darkness := "LIGHT (dark text on light background)"
	contrast := "Use " + t.FG + " for text on the light background; reserve the accent for emphasis."
	if t.Dark {
		darkness = "DARK (light text on dark background)"
		contrast = "This is a DARK slide — ALL text must be light (" + t.FG + " or " + t.FGMuted + "). NEVER put dark text on the dark background. Black drop-shadows vanish on dark — use a subtle accent-hue glow or a 1px light stroke for elevation."
	}
	r := strings.NewReplacer(
		"{{BG}}", t.BG,
		"{{SURFACE}}", t.Surface,
		"{{BORDER}}", t.Border,
		"{{FG}}", t.FG,
		"{{FG_MUTED}}", t.FGMuted,
		"{{ACCENT}}", t.Accent,
		"{{ACCENT2}}", t.Accent2,
		"{{DARKNESS}}", darkness,
		"{{CONTRAST_RULE}}", contrast,
		"{{FONT_DISPLAY}}", t.FontDisp,
		"{{FONT_BODY}}", t.FontBody,
		"{{FONT_MONO}}", t.FontMono,
		"{{PATTERN}}", t.Pattern,
		"{{MOOD}}", t.Mood,
	)
	return r.Replace(prompts.SVGMaster)
}
