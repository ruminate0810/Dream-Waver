package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/svgicons"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// PMQ A4 — per-slide DESIGN self-critique + refine.
//
// RepairSVGSlides already fixes GEOMETRY (overflow / overlap) via measured
// bounding boxes. This pass complements it with the SOFT design rules a
// ruler can't see: a topic-word title instead of an assertion, a lone metric
// with no context, the accent colour smeared across the whole slide, or a
// dead empty lower half. A lightweight critic reads each authored slide and,
// only when it flags a concrete violation, one targeted refine pass rewrites
// that single slide.
//
// COST-GATED: off unless DW_SVG_SELF_CRITIQUE is set. When on it adds one
// critic call per non-fallback slide (+ one refine call only for slides that
// fail), which roughly doubles a deck's authoring LLM calls — worth it for a
// final polish, not for every draft. Both calls run on the fast svg_author
// tier and the refine reuses the (prompt-cached) author system prompt, so the
// real added cost is modest. Fallback stand-in slides are skipped.

// fallbackMarker tags FallbackSVG output so the critique pass can skip plain
// stand-in slides (nothing to polish there).
const fallbackMarker = "<!--dw-fallback-->"

func isFallbackSVG(svg string) bool { return strings.Contains(svg, fallbackMarker) }

// svgSelfCritiqueEnabled gates the whole pass.
func svgSelfCritiqueEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DW_SVG_SELF_CRITIQUE"))) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

const svgCritiqueConcurrency = 3

const svgCritiqueSystem = `You are a ruthless presentation-design editor reviewing ONE slide that is supplied as SVG markup. Judge it ONLY against these rules and return concrete, actionable fixes — or an empty list if it is already strong.

Check, in order:
- TITLE is an ASSERTION (a claim or takeaway), not a bare topic word. "成本" or "Overview" is a fail; "推理成本降至行业 1/10" passes.
- Every headline NUMBER carries context (a label AND a "so what" implication). A lone metric floating alone is a fail.
- ACCENT restraint: the accent colour highlights at most ~3 things. If almost everything is the accent colour, that's a fail.
- The composition FILLS the canvas — flag a large empty lower half or content crammed into the top.
- No generic filler title ("Key Points", "Introduction", "The Future", "Thank you").

Return STRICT JSON, NO markdown fences:
{"fixes": ["<one concrete instruction>", ...]}
Each fix is ONE actionable sentence naming what to change and how (e.g. "Rewrite the title '成本' as an assertion such as '推理成本降至行业 1/10'"). Return {"fixes": []} when the slide already satisfies every rule. Be strict, but do NOT invent problems — flag only clear violations. At most 3 fixes.`

// CritiqueRefineSVGSlides runs the A4 design critique over content's slides.
// No-op (returns content unchanged) unless DW_SVG_SELF_CRITIQUE is enabled.
// Never regresses: a failed critique/refine or a refined slide that fails QA
// leaves the original slide in place. Returns the usage it spent.
// onRefined(index0, refinedSVG) fires once per slide that was actually
// improved, so the caller can refresh the live preview for that page.
func CritiqueRefineSVGSlides(ctx context.Context, router llm.Router, theme string, content *ContentResult, onRefined func(index0 int, refinedSVG string)) (*ContentResult, llm.Usage, error) {
	var total llm.Usage
	if content == nil || len(content.Slides) == 0 || !svgSelfCritiqueEnabled() {
		return content, total, nil
	}
	tok := themetokens.Get(theme)
	sys := renderSVGPrompt(tok) // full author prompt (prompt-cached) drives the refine
	client := router.For("svg_author")
	model := router.ModelFor("svg_author")

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	sem := make(chan struct{}, svgCritiqueConcurrency)
	for i := range content.Slides {
		svg := content.Slides[i].Data.SVG
		if strings.TrimSpace(svg) == "" || isFallbackSVG(svg) {
			continue // blank or stand-in slide — nothing to polish
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, svg string) {
			defer wg.Done()
			defer func() { <-sem }()

			notes, u1, err := critiqueOneSVG(ctx, client, model, svg)
			mu.Lock()
			total.InputTokens += u1.InputTokens
			total.OutputTokens += u1.OutputTokens
			mu.Unlock()
			if err != nil || len(notes) == 0 {
				return // critic failed or slide is already good — leave it
			}
			refined, u2, rerr := refineOneSVGDesign(ctx, client, model, sys, svg, notes)
			mu.Lock()
			total.InputTokens += u2.InputTokens
			total.OutputTokens += u2.OutputTokens
			mu.Unlock()
			if rerr != nil || !QASlideUsable(refined) {
				return // refine failed — keep the original (never regress)
			}
			refined = svgicons.Inline(refined, tok.FGMuted) // re-resolve any <use> icons
			mu.Lock()
			content.Slides[i].Data.SVG = refined
			mu.Unlock()
			slog.Info("svg slide self-refined", "slide", i+1, "fixes", strings.Join(notes, "; "))
			if onRefined != nil {
				onRefined(i, refined)
			}
		}(i, svg)
	}
	wg.Wait()
	return content, total, nil
}

// critiqueOneSVG asks the critic for ≤3 concrete design fixes (empty = good).
func critiqueOneSVG(ctx context.Context, client llm.Client, model, svg string) ([]string, llm.Usage, error) {
	resp, err := client.AskTool(ctx, llm.AskToolRequest{
		Model:             model,
		SystemPrompt:      svgCritiqueSystem,
		Messages:          []schema.Message{schema.NewUser("Slide SVG to review:\n" + svg)},
		MaxTokens:         700,
		EnablePromptCache: true,
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}
	var parsed struct {
		Fixes []string `json:"fixes"`
	}
	if jerr := json.Unmarshal(stripFences(resp.Content), &parsed); jerr != nil {
		// Treat an unparseable critique as "no issues" rather than failing
		// the deck — A4 must never block on a critic glitch.
		return nil, resp.Usage, nil
	}
	// Drop blank/oversized noise; cap at 3.
	out := make([]string, 0, 3)
	for _, f := range parsed.Fixes {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
			if len(out) == 3 {
				break
			}
		}
	}
	return out, resp.Usage, nil
}

// refineOneSVGDesign rewrites one slide to address the critic's notes while
// preserving its layout, palette, and content. Uses the full author system
// prompt so the model has all the rules + exemplars to do a quality fix.
func refineOneSVGDesign(ctx context.Context, client llm.Client, model, sys, svg string, notes []string) (string, llm.Usage, error) {
	var user strings.Builder
	user.WriteString("Improve THIS existing slide. Keep its layout, palette, skeleton, and content intact — a reviewer asked only for these specific fixes:\n")
	for i, n := range notes {
		fmt.Fprintf(&user, "  %d. %s\n", i+1, n)
	}
	user.WriteString("\nReturn STRICT JSON — NO markdown fences:\n{\"svg\":\"<svg viewBox='0 0 1920 1080' …>…</svg>\"}\nThe full improved SVG, applying the fixes and nothing else.\n\nCurrent SVG:\n")
	user.WriteString(svg)

	resp, err := client.AskTool(ctx, llm.AskToolRequest{
		Model:             model,
		SystemPrompt:      sys,
		Messages:          []schema.Message{schema.NewUser(user.String())},
		MaxTokens:         12000,
		EnablePromptCache: true,
	})
	if err != nil {
		return "", llm.Usage{}, err
	}
	out := parseOneSVG(resp.Content)
	if strings.TrimSpace(out) == "" {
		return "", resp.Usage, fmt.Errorf("refine returned no <svg>")
	}
	return out, resp.Usage, nil
}
