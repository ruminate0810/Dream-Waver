// Package slides is the high-level orchestrator that turns a user prompt into
// a downloadable .pptx. Two execution paths share the same underlying
// stages (see internal/skill/slides/stages):
//
//   - Pipeline.Run — deterministic, fastest. Calls Outline → Content →
//     Assemble → resolveImages → RenderDeck in sequence.
//   - AgentRunner.Run — same stages, but driven by a ToolCallAgent so the
//     LLM picks each step (and may optionally insert web_research before
//     planning). See agent_runner.go.
//
// Two paths, one set of stage primitives. The public Input/Output/Cost
// types live here because they are the API surface.
package slides

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

// Re-exported aliases so callers can use the typed domain without importing
// the schema package directly.
type (
	Deck        = schema.Deck
	Slide       = schema.Slide
	SlideData   = schema.SlideData
	SlideLayout = schema.SlideLayout
	Theme       = schema.Theme
)

// Pipeline runs the deterministic, single-shot generation path. For the
// agent-driven path see AgentRunner in agent_runner.go.
type Pipeline struct {
	Router      llm.Router
	Renderer    *tool.SlideRender
	Emitter     event.Emitter
	Images      image.Searcher // optional; NoopSearcher disables hero images
	TemplateDir string
}

// Input is the request shape both Pipeline.Run and AgentRunner.Run accept.
// ForceTheme overrides whatever theme the planner picked; ReferenceText is
// fed into the outline prompt as supplementary material the LLM should
// quote from.
type Input struct {
	Topic         string `json:"topic"`
	Audience      string `json:"audience,omitempty"`
	SlideCount    int    `json:"slide_count,omitempty"`
	Style         string `json:"style,omitempty"`
	ReferenceText string `json:"reference_text,omitempty"`
	ForceTheme    string `json:"force_theme,omitempty"`
}

// Output is the shared result envelope. Both runners write the same shape
// so callers (the API layer) don't branch on which path produced the deck.
type Output struct {
	PptxPath   string `json:"pptx_path"`
	Title      string `json:"title"`
	SlideCount int    `json:"slide_count"`
	Cost       Cost   `json:"cost"`
}

// Cost accumulates LLM usage across every step in a single generation.
// EstimatedUSD is derived (see estimateCost) so the API can show a "this
// run cost $X" badge without re-summing on the frontend.
type Cost struct {
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CacheReadTokens int     `json:"cache_read_tokens"`
	EstimatedUSD    float64 `json:"estimated_usd"`
}

// Run is the fast/deterministic execution path: outline → content →
// assemble → resolve images → render. Token usage is summed across the
// two LLM calls.
func (p *Pipeline) Run(ctx context.Context, in Input) (*Output, error) {
	var cost Cost

	outline, u1, err := stages.Outline(ctx, p.Router, stages.OutlineParams{
		Topic:         in.Topic,
		Audience:      in.Audience,
		SlideCount:    in.SlideCount,
		Style:         in.Style,
		ReferenceText: in.ReferenceText,
	})
	if err != nil {
		return nil, fmt.Errorf("outline: %w", err)
	}
	cost.add(u1)
	p.emit(ctx, event.NewOutline(outline.Title, len(outline.Slides)))

	// Allow the request to pin a theme — overrides whatever the planner picked.
	if in.ForceTheme != "" {
		outline.Theme = schema.Theme(in.ForceTheme)
	}

	content, u2, err := stages.Content(ctx, p.Router, outline)
	if err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	cost.add(u2)

	deck := stages.Assemble(outline, content)
	// Resolve per-slide ImageQuery → Image URL in parallel. NoopSearcher
	// makes this a no-op when no Unsplash key is configured.
	p.resolveImages(ctx, &deck)

	// Direct typed call into the renderer — no JSON round-trip needed when
	// we already hold a typed Deck.
	pptxPath, err := p.Renderer.RenderDeck(ctx, deck)
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}

	cost.EstimatedUSD = estimateCost(cost)

	return &Output{
		PptxPath:   pptxPath,
		Title:      outline.Title,
		SlideCount: len(outline.Slides),
		Cost:       cost,
	}, nil
}

// resolveImages fans out parallel Unsplash searches for every slide that
// emitted an ImageQuery. Failures degrade gracefully — the slide just
// renders without an image. Same query across slides is deduped so we
// don't waste API budget on repeats.
//
// Shared between Pipeline.Run and AgentRunner so both paths get the same
// hero-image behaviour.
func (p *Pipeline) resolveImages(ctx context.Context, deck *Deck) {
	resolveImages(ctx, p.Images, deck)
}

func resolveImages(ctx context.Context, searcher image.Searcher, deck *Deck) {
	if searcher == nil {
		return
	}
	type job struct {
		idx   int
		query string
	}
	jobs := []job{}
	for i, s := range deck.Slides {
		if q := strings.TrimSpace(s.Data.ImageQuery); q != "" {
			jobs = append(jobs, job{i, q})
		}
	}
	if len(jobs) == 0 {
		return
	}

	var cacheMu sync.Mutex
	cache := map[string]*image.Result{}

	var wg sync.WaitGroup
	for _, j := range jobs {
		j := j
		wg.Add(1)
		go func() {
			defer wg.Done()
			cacheMu.Lock()
			if r, ok := cache[j.query]; ok {
				cacheMu.Unlock()
				if r != nil {
					deck.Slides[j.idx].Data.Image = r.URL
					deck.Slides[j.idx].Data.ImageCredit = r.Credit
				}
				return
			}
			cacheMu.Unlock()

			r, err := searcher.Search(ctx, j.query)
			if err != nil {
				slog.WarnContext(ctx, "image search failed", "query", j.query, "err", err)
				return
			}
			cacheMu.Lock()
			cache[j.query] = r // record nil too so we don't retry empty hits
			cacheMu.Unlock()
			if r != nil {
				deck.Slides[j.idx].Data.Image = r.URL
				deck.Slides[j.idx].Data.ImageCredit = r.Credit
			}
		}()
	}
	wg.Wait()
}

// ─── Helpers ────────────────────────────────────────────────────────

func (p *Pipeline) emit(ctx context.Context, ev event.Event) {
	if p.Emitter == nil {
		return
	}
	p.Emitter.Emit(ctx, ev) // SessionID injected from ctx
}

// add accumulates one LLM call's usage into the running Cost total.
func (c *Cost) add(u llm.Usage) {
	c.InputTokens += u.InputTokens
	c.OutputTokens += u.OutputTokens
	c.CacheReadTokens += u.CacheReadTokens
}

// estimateCost is a rough USD estimate of one generation. The constants
// reflect DeepSeek v4-pro discounted rates (¥3/M in, ¥6/M out, ¥0.025/M
// cache) at the 7.1 RMB/USD reference rate. Real billing should use
// per-call usage and the actual model attached, not this blended figure.
func estimateCost(c Cost) float64 {
	const (
		inputPer1k     = 0.00042
		outputPer1k    = 0.00085
		cacheReadPer1k = 0.0000035
	)
	return float64(c.InputTokens)/1000*inputPer1k +
		float64(c.OutputTokens)/1000*outputPer1k +
		float64(c.CacheReadTokens)/1000*cacheReadPer1k
}
