package stages

import (
	"context"
	"encoding/json"
	"fmt"
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

// svgPerSlidePerCallTimeout bounds a single slide's author call so one
// slow/stuck completion can't stall the whole deck (the rest finish and
// the slow slide is simply skipped, best-effort).
const svgPerSlidePerCallTimeout = 150 * time.Second

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
func AuthorSVGPerSlide(
	ctx context.Context,
	router llm.Router,
	outline *OutlineResult,
	theme string,
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
	client := router.For("planner")
	model := router.ModelFor("planner")

	for i, s := range outline.Slides {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, s OutlineSlide) {
			defer wg.Done()
			defer func() { <-sem }()
			callCtx, cancel := context.WithTimeout(ctx, svgPerSlidePerCallTimeout)
			defer cancel()
			svg, usage, err := authorOneSVG(callCtx, client, model, sys, deckCtx, s, i+1, n)
			if err != nil || strings.TrimSpace(svg) == "" {
				return // best-effort; leave this index empty
			}
			svg = svgicons.Inline(svg, tok.FGMuted) // PM-1: resolve <use data-icon>
			if !QASlideUsable(svg) {              // PM-4: junk/blank → clean fallback
				svg = FallbackSVG(tok, s.Headline)
			}
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
func authorOneSVG(ctx context.Context, client llm.Client, model, sys, deckCtx string, s OutlineSlide, pos, total int) (string, llm.Usage, error) {
	var user strings.Builder
	user.WriteString(deckCtx)
	fmt.Fprintf(&user, "\n\n── AUTHOR ONLY THIS ONE SLIDE (position %d of %d) ──\n[%s] %s\n", pos, total, s.Type, s.Headline)
	if len(s.KeyPoints) > 0 && string(s.KeyPoints) != "null" {
		fmt.Fprintf(&user, "points: %s\n", string(s.KeyPoints))
	}
	if strings.TrimSpace(s.SpeakerNotes) != "" {
		fmt.Fprintf(&user, "note: %s\n", s.SpeakerNotes)
	}
	user.WriteString("\nReturn STRICT JSON for THIS ONE slide only — NO array, NO markdown fences:\n{\"svg\":\"<svg viewBox='0 0 1920 1080' xmlns='http://www.w3.org/2000/svg' width='1920' height='1080'>…</svg>\"}")

	var svg string
	resp, err := askWithRetry(ctx, client, "svg-one", llm.AskToolRequest{
		Model:        model,
		SystemPrompt: sys,
		Messages:     []schema.Message{schema.NewUser(user.String())},
		// One rich slide is ~3-4K chars; 9000 leaves generous headroom.
		MaxTokens:         9000,
		EnablePromptCache: true,
	}, func(content string) error {
		svg = parseOneSVG(content)
		if strings.TrimSpace(svg) == "" {
			return fmt.Errorf("no <svg> in response: %q", truncate(content, 160))
		}
		return nil
	})
	if err != nil {
		return "", llm.Usage{}, err
	}
	if resp != nil {
		return svg, resp.Usage, nil
	}
	return svg, llm.Usage{}, nil
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
