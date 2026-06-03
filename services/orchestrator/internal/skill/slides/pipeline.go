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
	"sync/atomic"

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
//
// Sessions is OPTIONAL but recommended: when non-nil, Run() registers the
// rendered deck into the store keyed by jobID so the live-preview HTML
// endpoint and follow-up edit endpoint work on pipeline-mode decks just
// like they do on agent-mode decks. Without it, those endpoints 404.
type Pipeline struct {
	Router      llm.Router
	Renderer    *tool.SlideRender
	Emitter     event.Emitter
	Images      image.Searcher // optional; NoopSearcher disables hero images
	TemplateDir string
	Sessions    *SessionStore // optional; nil disables live preview + edits for pipeline-mode decks
}

// Input is the request shape both Pipeline.Run and AgentRunner.Run accept.
// ForceTheme overrides whatever theme the planner picked; ReferenceText is
// fed into the outline prompt as supplementary material the LLM should
// quote from. Sprint T2 — Brand is optional brand-colour overrides applied
// to the deck before content writing, so a user-template's brand carries
// through end-to-end without a separate apply_brand turn.
type Input struct {
	Topic         string        `json:"topic"`
	Audience      string        `json:"audience,omitempty"`
	SlideCount    int           `json:"slide_count,omitempty"`
	Style         string        `json:"style,omitempty"`
	ReferenceText string        `json:"reference_text,omitempty"`
	ForceTheme    string        `json:"force_theme,omitempty"`
	Brand         *schema.Brand `json:"brand,omitempty"`
	// Sprint BR.2 — when set, the user has picked this blueprint at the
	// wizard's blueprint-pick step. Threaded through agent_runner →
	// PlanOutline tool → stages.Outline as a HARD slide-sequence
	// constraint. Empty = free-form generation (no blueprint).
	BlueprintID string `json:"blueprint_id,omitempty"`
}

// Output is the shared result envelope. Both runners write the same shape
// so callers (the API layer) don't branch on which path produced the deck.
type Output struct {
	PptxPath   string `json:"pptx_path"`
	Title      string `json:"title"`
	SlideCount int    `json:"slide_count"`
	Cost       Cost   `json:"cost"`

	// Sprint L1 — Status is "" (defaults to "finished") for completed
	// runs, or one of "awaiting_clarification" / "awaiting_outline_approval"
	// when the agent-mode initial-gen flow has paused at a HILT gate.
	// The API handler reads this to set the job's public status.
	Status string `json:"status,omitempty"`
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
//
// jobID is used to register the final SessionState into Sessions (when
// configured) so the live-preview and edit endpoints can reach this
// deck after Run returns. Pass an empty string from contexts that
// don't need post-render access (CLI tests, etc.).
func (p *Pipeline) Run(ctx context.Context, jobID string, in Input) (*Output, error) {
	var cost Cost

	outline, u1, err := stages.Outline(ctx, p.Router, stages.OutlineParams{
		Topic:         in.Topic,
		Audience:      in.Audience,
		SlideCount:    in.SlideCount,
		Style:         in.Style,
		ReferenceText: in.ReferenceText,
		BlueprintID:   in.BlueprintID, // BR.2 — pipeline mode honours blueprint too
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

	// Register the session so live-preview HTML + follow-up edits can
	// reach this deck after Run() returns. Mirrors AgentRunner.Run's
	// behaviour at agent_runner.go — without this, pipeline-mode decks
	// hit 404 on /api/v1/slides/{id}/page/{n}.html. No agent memory to
	// persist (no LLM turns happened on the agent loop), so Memory
	// stays nil — follow-up edits start a fresh agent against the
	// existing Deck/Outline/Content.
	if p.Sessions != nil && jobID != "" {
		state := &SessionState{
			JobID:      jobID,
			Input:      in,
			Outline:    outline,
			Content:    content,
			Deck:       &deck,
			PptxPath:   pptxPath,
			SlideCount: len(deck.Slides),
		}
		p.Sessions.Put(state)
	}

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
	jobs := collectImageJobs(deck)
	if len(jobs) == 0 {
		return
	}

	var cacheMu sync.Mutex
	cache := map[string]*image.Result{}

	// Sprint I0.2 — atomic counters so the post-fanout summary log can
	// surface per-job success rate. Per-goroutine warns are still useful
	// for diagnosing WHICH queries failed, but without this aggregate
	// even a 3-of-4-failed grid is invisible at the job-log level.
	var succeeded, failed int64

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
					writeImageResult(deck, j, r)
					atomic.AddInt64(&succeeded, 1)
				} else {
					atomic.AddInt64(&failed, 1)
				}
				return
			}
			cacheMu.Unlock()

			r, err := searcher.Search(ctx, j.query)
			if err != nil {
				slog.WarnContext(ctx, "image search failed", "query", j.query, "err", err)
				atomic.AddInt64(&failed, 1)
				return
			}
			cacheMu.Lock()
			cache[j.query] = r // record nil too so we don't retry empty hits
			cacheMu.Unlock()
			if r != nil {
				writeImageResult(deck, j, r)
				atomic.AddInt64(&succeeded, 1)
			} else {
				atomic.AddInt64(&failed, 1)
			}
		}()
	}
	wg.Wait()

	s, f := atomic.LoadInt64(&succeeded), atomic.LoadInt64(&failed)
	slog.InfoContext(ctx, "image fanout finished",
		"total", len(jobs), "succeeded", s, "failed", f,
		"success_rate", fmt.Sprintf("%.0f%%", 100*float64(s)/float64(len(jobs))),
	)
}

// imageJobKind enumerates every place a slide can store an image URL.
// Routing by typed kind beats the prior gridIdx-int sentinel approach
// once we have ≥ 4 targets (Image / Images[i] / BeforeImage /
// AfterImage / TeamMembers[i].Avatar / BentoCards[i].Image).
type imageJobKind int

const (
	imgJobSingle      imageJobKind = iota // SlideData.Image
	imgJobGrid                            // SlideData.Images[subIdx]
	imgJobBeforeImage                     // SlideData.BeforeImage
	imgJobAfterImage                      // SlideData.AfterImage
	imgJobTeamAvatar                      // SlideData.TeamMembers[subIdx].Avatar
	imgJobBentoCard                       // SlideData.BentoCards[subIdx].Image
)

type imageJob struct {
	slideIdx int
	query    string
	kind     imageJobKind
	subIdx   int // index into Images / TeamMembers / BentoCards (ignored for the others)
}

// collectImageJobs walks every slide and emits one imageJob per pending
// image fetch across all layouts. Pre-allocates the destination slices
// (Images, etc.) so the goroutines can write by index without locking.
func collectImageJobs(deck *Deck) []imageJob {
	var jobs []imageJob
	for i, s := range deck.Slides {
		// Single hero image (title / section / closing / photo-essay / split-image / pull-quote etc.)
		if q := strings.TrimSpace(s.Data.ImageQuery); q != "" {
			jobs = append(jobs, imageJob{i, q, imgJobSingle, 0})
		}
		// Image-grid: N parallel queries → indexed Images slice
		if len(s.Data.ImageQueries) > 0 {
			deck.Slides[i].Data.Images = make([]string, len(s.Data.ImageQueries))
			for gi, q := range s.Data.ImageQueries {
				if q = strings.TrimSpace(q); q != "" {
					jobs = append(jobs, imageJob{i, q, imgJobGrid, gi})
				}
			}
		}
		// Before-after: two distinct queries → BeforeImage / AfterImage
		if q := strings.TrimSpace(s.Data.BeforeImageQuery); q != "" {
			jobs = append(jobs, imageJob{i, q, imgJobBeforeImage, 0})
		}
		if q := strings.TrimSpace(s.Data.AfterImageQuery); q != "" {
			jobs = append(jobs, imageJob{i, q, imgJobAfterImage, 0})
		}
		// Team-roster: one avatar per member with an AvatarQuery
		for mi, m := range s.Data.TeamMembers {
			if q := strings.TrimSpace(m.AvatarQuery); q != "" {
				jobs = append(jobs, imageJob{i, q, imgJobTeamAvatar, mi})
			}
		}
		// Bento-grid: one image per card with an ImageQuery
		for ci, c := range s.Data.BentoCards {
			if q := strings.TrimSpace(c.ImageQuery); q != "" {
				jobs = append(jobs, imageJob{i, q, imgJobBentoCard, ci})
			}
		}
	}
	return jobs
}

// writeImageResult routes one image.Result into the right field of the
// right slide based on the job's typed kind. Centralised so every new
// image-bearing layout extends ONE switch instead of growing if-chains
// throughout the codebase.
func writeImageResult(deck *Deck, j imageJob, r *image.Result) {
	s := &deck.Slides[j.slideIdx]
	switch j.kind {
	case imgJobSingle:
		s.Data.Image = r.URL
		s.Data.ImageCredit = r.Credit
	case imgJobGrid:
		s.Data.Images[j.subIdx] = r.URL
		s.Data.ImageCredit = r.Credit
	case imgJobBeforeImage:
		s.Data.BeforeImage = r.URL
		s.Data.ImageCredit = r.Credit
	case imgJobAfterImage:
		s.Data.AfterImage = r.URL
		s.Data.ImageCredit = r.Credit
	case imgJobTeamAvatar:
		s.Data.TeamMembers[j.subIdx].Avatar = r.URL
		// Avatars don't show per-member credit — too noisy.
	case imgJobBentoCard:
		s.Data.BentoCards[j.subIdx].Image = r.URL
		s.Data.ImageCredit = r.Credit
	}
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
