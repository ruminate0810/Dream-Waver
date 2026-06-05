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
// MoA-2: ON by default (opt out with DW_SVG_SELF_CRITIQUE=off). Per non-fallback
// slide it runs an author↔critic LOOP of up to svgCritiqueMaxRounds rounds:
// critique → (if issues) refine → re-critique, stopping early when the critic is
// clean. Both calls run on the fast svg_author tier and the refine reuses the
// (prompt-cached) author system prompt, so the added cost stays modest. Fallback
// stand-in slides are skipped; a refine that fails QA never regresses the slide.

// fallbackMarker tags FallbackSVG output so the critique pass can skip plain
// stand-in slides (nothing to polish there).
const fallbackMarker = "<!--dw-fallback-->"

func isFallbackSVG(svg string) bool { return strings.Contains(svg, fallbackMarker) }

// svgSelfCritiqueEnabled gates the whole pass. MoA-2: the author↔critic loop
// is now CORE to quality, so it runs by DEFAULT. Opt OUT (e.g. for a fast
// cheap draft) with DW_SVG_SELF_CRITIQUE=off.
func svgSelfCritiqueEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DW_SVG_SELF_CRITIQUE"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

const svgCritiqueConcurrency = 3

// svgCritiqueMaxRounds bounds the per-slide author↔critic loop (MoA-2): each
// round = one critique + (if it flags issues) one refine, then re-critique. 2
// rounds catches the common case (a fix that introduces a smaller issue) while
// staying cost-bounded; a slide still flagged after 2 ships as-is.
const svgCritiqueMaxRounds = 2

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

			// MoA-2: author↔critic LOOP. Each round critiques the CURRENT
			// SVG; if the critic flags issues, refine once and loop to
			// re-critique. Stop early when clean, when refine fails QA (keep
			// the last good version — never regress), or after maxRounds.
			cur := svg
			improved := false
			for round := 1; round <= svgCritiqueMaxRounds; round++ {
				notes, u1, err := critiqueOneSVG(ctx, client, model, cur)
				mu.Lock()
				total.InputTokens += u1.InputTokens
				total.OutputTokens += u1.OutputTokens
				mu.Unlock()
				if err != nil || len(notes) == 0 {
					break // critic failed or the slide is clean — done
				}
				refined, u2, rerr := refineOneSVGDesign(ctx, client, model, sys, cur, notes)
				mu.Lock()
				total.InputTokens += u2.InputTokens
				total.OutputTokens += u2.OutputTokens
				mu.Unlock()
				if rerr != nil || !QASlideUsable(refined) {
					break // refine failed — keep the last good version
				}
				cur = svgicons.Inline(refined, tok.FGMuted) // re-resolve any <use> icons
				improved = true
				slog.Info("svg slide critic-refined", "slide", i+1, "round", round, "fixes", strings.Join(notes, "; "))
			}
			if !improved {
				return // nothing changed
			}
			mu.Lock()
			content.Slides[i].Data.SVG = cur
			mu.Unlock()
			if onRefined != nil {
				onRefined(i, cur)
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
