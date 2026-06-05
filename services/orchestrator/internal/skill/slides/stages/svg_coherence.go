package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/svgicons"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// MoA-3 — the Coherence Editor (the final whole-deck reviewer).
//
// Every per-slide agent (author, critic) saw ONLY its own slide, so none of
// them can catch a number that disagrees between slides, a broken narrative, or
// three identical layouts in a row. This pass reads a text digest of the WHOLE
// deck once, asks one strong-tier critic for ≤3 targeted cross-slide fixes, and
// applies each by re-authoring just that slide. Best-effort and non-regressing:
// any failure leaves the deck as-is.
//
// ON by default; opt out with DW_SVG_COHERENCE=off.

// svgCoherenceMaxFixes caps how many slides one coherence pass rewrites — keeps
// the final stage bounded in cost + wall-time.
const svgCoherenceMaxFixes = 3

func svgCoherenceEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DW_SVG_COHERENCE"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

var (
	svgTextBlockRe = regexp.MustCompile(`(?s)<text\b[^>]*>(.*?)</text>`)
	svgInnerTagRe  = regexp.MustCompile(`<[^>]+>`)
	svgLayoutRe    = regexp.MustCompile(`<!--\s*layout:\s*([a-z0-9-]+)\s*-->`)
)

// extractSlideText pulls the visible words out of a slide's SVG (all <text>
// blocks, with nested <tspan> tags stripped) into a compact one-line digest so
// the coherence critic reads content, not markup.
func extractSlideText(svg string) string {
	var b strings.Builder
	for _, m := range svgTextBlockRe.FindAllStringSubmatch(svg, -1) {
		inner := strings.TrimSpace(svgInnerTagRe.ReplaceAllString(m[1], ""))
		inner = strings.Join(strings.Fields(inner), " ") // collapse whitespace
		if inner != "" {
			b.WriteString(inner)
			b.WriteString(" · ")
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(b.String()), "·"))
}

// extractLayout reads the `<!-- layout: x -->` hint the author leaves as the
// first child; "?" when absent.
func extractLayout(svg string) string {
	if m := svgLayoutRe.FindStringSubmatch(svg); m != nil {
		return m[1]
	}
	return "?"
}

// CoherenceReviewSVG runs the MoA-3 whole-deck pass: digest → critic → apply
// ≤svgCoherenceMaxFixes targeted per-slide fixes. Returns the (possibly
// updated) content + token usage. onRefined(index0, svg) fires per fixed slide
// so the live preview refreshes. No-op for decks under 3 slides, when disabled,
// or on any critic/refine failure (never regresses).
func CoherenceReviewSVG(ctx context.Context, router llm.Router, theme, deckTitle string, content *ContentResult, onRefined func(index0 int, svg string)) (*ContentResult, llm.Usage, error) {
	var total llm.Usage
	if content == nil || len(content.Slides) < 3 || !svgCoherenceEnabled() {
		return content, total, nil
	}

	// Build the whole-deck digest the critic reads.
	var digest strings.Builder
	for i := range content.Slides {
		svg := content.Slides[i].Data.SVG
		txt := extractSlideText(svg)
		if isFallbackSVG(svg) {
			txt = "(placeholder — skip)"
		}
		fmt.Fprintf(&digest, "Slide %d [%s]: %s\n", i+1, extractLayout(svg), truncate(txt, 260))
	}

	critic := router.For("critic")
	resp, err := critic.AskTool(ctx, llm.AskToolRequest{
		Model:             router.ModelFor("critic"),
		SystemPrompt:      prompts.SVGCoherence,
		Messages:          []schema.Message{schema.NewUser("Deck title: " + deckTitle + "\n\nSlides:\n" + digest.String())},
		MaxTokens:         1400,
		EnablePromptCache: true,
	})
	if err != nil {
		return content, total, nil // critic glitch — ship the deck as-is
	}
	total.InputTokens += resp.Usage.InputTokens
	total.OutputTokens += resp.Usage.OutputTokens

	var parsed struct {
		Fixes []struct {
			Slide int    `json:"slide"`
			Fix   string `json:"fix"`
		} `json:"fixes"`
	}
	if jerr := json.Unmarshal(stripFences(resp.Content), &parsed); jerr != nil || len(parsed.Fixes) == 0 {
		return content, total, nil // unparseable or coherent — done
	}

	tok := themetokens.Get(theme)
	sys := renderSVGPrompt(tok)
	rclient := router.For("svg_author")
	rmodel := router.ModelFor("svg_author")

	applied := 0
	for _, f := range parsed.Fixes {
		if applied >= svgCoherenceMaxFixes {
			break
		}
		idx0 := f.Slide - 1
		if idx0 < 0 || idx0 >= len(content.Slides) || strings.TrimSpace(f.Fix) == "" {
			continue
		}
		if isFallbackSVG(content.Slides[idx0].Data.SVG) {
			continue // nothing to fix on a placeholder
		}
		refined, u, rerr := refineOneSVGDesign(ctx, rclient, rmodel, sys, content.Slides[idx0].Data.SVG, []string{f.Fix})
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		if rerr != nil || !QASlideUsable(refined) {
			continue // never regress
		}
		content.Slides[idx0].Data.SVG = svgicons.Inline(refined, tok.FGMuted)
		applied++
		slog.Info("coherence fix applied", "slide", f.Slide, "fix", f.Fix)
		if onRefined != nil {
			onRefined(idx0, content.Slides[idx0].Data.SVG)
		}
	}
	return content, total, nil
}
