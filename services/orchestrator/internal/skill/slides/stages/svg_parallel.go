package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/svgicons"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// svgPerSlideConcurrency caps how many slide-author LLM calls run at once.
// 3 is the sweet spot: enough parallelism to cut wall-time hard, but few
// enough concurrent planner calls that DeepSeek doesn't rate-limit (5 was
// triggering transient failures + retries that ate the speed win).
const svgPerSlideConcurrency = 3

// svgPerSlideAttemptTimeout bounds ONE author attempt. With the author
// routed to v4-flash (PMQ A1) a rich slide returns in ~15-40s, so 90s is
// generous headroom. Crucially each ATTEMPT gets its own deadline: the old
// design shared a single 150s budget across the retry, so a slow first try
// left no time for the second — retries never actually helped against the
// timeouts that were degrading most slides to a plain fallback page.
const svgPerSlideAttemptTimeout = 90 * time.Second

// svgPerSlideMaxAttempts caps tries per slide before giving up to a clean
// titled fallback. 2 is enough: flash rarely needs a retry, and a slide
// that genuinely keeps failing shouldn't burn the whole deck's wall-time.
const svgPerSlideMaxAttempts = 2

// AuthorSVGPerSlide authors each slide as its OWN parallel LLM call
// (Sprint PM, option A). Two wins over the single-call AuthorSVG:
//   - SPEED: wall-time ≈ the slowest single slide, not the sum. A 7-slide
//     deck drops from ~4-5 min to ~40-70s.
//   - STREAMING: onSlide fires the instant each slide's <svg> returns, so
//     the live preview fills in page-by-page instead of a black box.
//
// The shared spec_lock system prompt is byte-identical across every call
// (prompt-cached → cheap), so colours/fonts/mood stay consistent; each
// slide still picks its own layout skeleton for variety. Every call also
// gets the full deck's headlines as narrative context so section dividers
// and the cover↔closing arc stay coherent.
//
// onSlide(index0, slide) fires once per successfully authored slide,
// possibly OUT OF ORDER (parallel). A slide whose call fails after
// retries is left out — best-effort, one bad slide doesn't kill the deck.
// The returned ContentResult.Slides is in deck order.
// spec (MoA Architect plan) is optional: when non-nil, each slide's brief
// (layout + chart + pinned numbers) is injected into its author prompt so the
// designers execute the plan instead of improvising. nil → free authoring
// (the pre-MoA behaviour), so the Architect can fail without blocking the deck.
func AuthorSVGPerSlide(
	ctx context.Context,
	router llm.Router,
	outline *OutlineResult,
	theme string,
	spec *DeckSpec,
	imgResolve func(context.Context, string) string,
	onSlide func(index0 int, cs *ContentSlide),
) (*ContentResult, llm.Usage, error) {
	if outline == nil || len(outline.Slides) == 0 {
		return nil, llm.Usage{}, fmt.Errorf("AuthorSVGPerSlide: outline is empty")
	}
	tok := themetokens.Get(theme)
	sys := renderSVGPrompt(tok)

	// Narrative context shared by every slide call.
	heads := make([]string, len(outline.Slides))
	for i, s := range outline.Slides {
		heads[i] = fmt.Sprintf("  %d. [%s] %s", i+1, s.Type, s.Headline)
	}
	deckCtx := fmt.Sprintf("Deck title: %s\nTheme: %s\nFull slide list (narrative context — keep the deck coherent):\n%s",
		outline.Title, theme, strings.Join(heads, "\n"))

	n := len(outline.Slides)
	results := make([]ContentSlide, n)
	okFlag := make([]bool, n)
	var (
		mu    sync.Mutex
		total llm.Usage
		wg    sync.WaitGroup
	)
	sem := make(chan struct{}, svgPerSlideConcurrency)
	// PMQ A1: author on the fast tier (v4-flash by default), NOT the slow
	// v4-pro planner. Pro's long-output latency was the root cause of the
	// per-slide timeouts. Configurable via LLM_MODEL_SVG_AUTHOR.
	client := router.For("svg_author")
	model := router.ModelFor("svg_author")

	for i, s := range outline.Slides {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, s OutlineSlide) {
			defer wg.Done()
			defer func() { <-sem }()

			// PMQ A1: retry with a FRESH per-attempt deadline + jittered
			// backoff. Each try gets its own timeout, so a slow first
			// attempt no longer starves the retry (the bug that left most
			// slides degraded to fallback under the old shared-budget design).
			var (
				svg   string
				usage llm.Usage
				err   error
			)
			for attempt := 1; attempt <= svgPerSlideMaxAttempts; attempt++ {
				attemptCtx, cancel := context.WithTimeout(ctx, svgPerSlideAttemptTimeout)
				svg, usage, err = authorOneSVG(attemptCtx, client, model, sys, deckCtx, s, spec.SpecFor(i), i+1, n)
				cancel()
				if err == nil && strings.TrimSpace(svg) != "" {
					break
				}
				if attempt < svgPerSlideMaxAttempts {
					slog.Warn("svg author attempt failed — retrying",
						"slide", i+1, "attempt", attempt, "err", svgErrStr(err))
					// Jittered backoff: 600ms·attempt + 0-400ms. Bail early
					// if the whole deck's context is already cancelled.
					backoff := time.Duration(attempt)*600*time.Millisecond +
						time.Duration(rand.Intn(400))*time.Millisecond
					select {
					case <-ctx.Done():
						attempt = svgPerSlideMaxAttempts // stop retrying
					case <-time.After(backoff):
					}
				}
			}
			if err == nil && strings.TrimSpace(svg) != "" {
				svg = svgicons.Inline(svg, tok.FGMuted)         // PM-1: resolve <use data-icon>
				svg = resolveSVGImageRefs(ctx, svg, imgResolve) // PM-3: resolve dw-img:// → image
			}
			if err != nil || !QASlideUsable(svg) {
				// PM-4: a failed/timed-out/junk slide becomes a clean
				// titled fallback (NOT dropped) so the deck keeps all N
				// pages instead of 404-ing the missing indices.
				slog.Warn("svg slide degraded to fallback",
					"slide", i+1, "err", svgErrStr(err))
				svg = FallbackSVG(tok, s.Headline)
			}
			// PMR: deterministic deck-wide chrome — replace whatever the model
			// drew for the background + footer with a unified gradient canvas
			// and a uniform footer (deck title + page no.), so every page is
			// coherent and free of cross-page drift. Applies to real + fallback.
			svg = finalizeSVGChrome(svg, tok, outline.Title, i+1, n)
			cs := ContentSlide{Index: i + 1, Template: theme, Layout: schema.LayoutSVG, Data: schema.SlideData{SVG: svg}}
			mu.Lock()
			results[i] = cs
			okFlag[i] = true
			total.InputTokens += usage.InputTokens
			total.OutputTokens += usage.OutputTokens
			total.CacheReadTokens += usage.CacheReadTokens
			total.CacheCreationTokens += usage.CacheCreationTokens
			mu.Unlock()
			if onSlide != nil {
				onSlide(i, &cs)
			}
		}(i, s)
	}
	wg.Wait()

	var slides []ContentSlide
	for i := 0; i < n; i++ {
		if okFlag[i] {
			slides = append(slides, results[i])
		}
	}
	if len(slides) == 0 {
		return nil, total, fmt.Errorf("AuthorSVGPerSlide: all %d slides failed", n)
	}
	return &ContentResult{Slides: slides}, total, nil
}

// authorOneSVG authors a single slide's SVG given the shared spec_lock
// system prompt + the deck context + this slide's spec.
func authorOneSVG(ctx context.Context, client llm.Client, model, sys, deckCtx string, s OutlineSlide, spec *SlideSpec, pos, total int) (string, llm.Usage, error) {
	var user strings.Builder
	user.WriteString(deckCtx)
	fmt.Fprintf(&user, "\n\n── AUTHOR ONLY THIS ONE SLIDE (position %d of %d) ──\n[%s] %s\n", pos, total, s.Type, s.Headline)
	if len(s.KeyPoints) > 0 && string(s.KeyPoints) != "null" {
		fmt.Fprintf(&user, "points: %s\n", string(s.KeyPoints))
	}
	if strings.TrimSpace(s.SpeakerNotes) != "" {
		fmt.Fprintf(&user, "note: %s\n", s.SpeakerNotes)
	}
	// MoA: when the Architect produced a plan, its per-slide brief (layout +
	// chart + pinned numbers) is the authority — append it so this designer
	// executes the plan instead of improvising.
	user.WriteString(spec.promptBlock())
	user.WriteString("\nReturn STRICT JSON for THIS ONE slide only — NO array, NO markdown fences:\n{\"svg\":\"<svg viewBox='0 0 1920 1080' xmlns='http://www.w3.org/2000/svg' width='1920' height='1080'>…</svg>\"}")

	// Single attempt — the per-slide loop in AuthorSVGPerSlide owns retries,
	// per-attempt timeouts, and jittered backoff, so this stays a one-shot
	// call. MaxTokens 12000: a rich, JSON-escaped slide SVG genuinely runs
	// large (flash was hitting finish_reason="length" at a 6000 cap → the
	// truncated reply parses as empty → the slide fell back to a plain page).
	// Since the author is on fast v4-flash (PMQ A1), a high cap costs nothing
	// when unused and only the richest slides approach it; the 90s per-attempt
	// budget still bounds a genuine runaway. (A2's prompt slim + compact-SVG
	// few-shots will pull typical output well under this.)
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
	svg := parseOneSVG(resp.Content)
	if strings.TrimSpace(svg) == "" {
		return "", resp.Usage, fmt.Errorf("no <svg> in response: %q", truncate(resp.Content, 160))
	}
	return svg, resp.Usage, nil
}

// svgErrStr renders an author error for logging; nil means the call
// "succeeded" but produced an unusable/QA-failing slide.
func svgErrStr(err error) string {
	if err == nil {
		return "qa-unusable"
	}
	return err.Error()
}

// parseOneSVG pulls the slide SVG from a single-slide response. Tolerant:
// accepts {"svg":"…"}, {"slides":[{"svg":"…"}]} (model slipped into the
// array shape), or a bare <svg>…</svg>.
func parseOneSVG(content string) string {
	raw := stripFences(content)
	var one struct {
		SVG string `json:"svg"`
	}
	if json.Unmarshal(raw, &one) == nil && strings.TrimSpace(one.SVG) != "" {
		return strings.TrimSpace(one.SVG)
	}
	var arr struct {
		Slides []struct {
			SVG string `json:"svg"`
		} `json:"slides"`
	}
	if json.Unmarshal(raw, &arr) == nil && len(arr.Slides) > 0 {
		return strings.TrimSpace(arr.Slides[0].SVG)
	}
	if i := strings.Index(content, "<svg"); i >= 0 {
		if j := strings.LastIndex(content, "</svg>"); j > i {
			return strings.TrimSpace(content[i : j+6])
		}
	}
	return ""
}
