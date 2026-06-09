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
	"os"
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
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	EstimatedUSD        float64 `json:"estimated_usd"`
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

// RunSVG is the Sprint SV-1 generation path: outline → AuthorSVG →
// assemble → render. Deterministic like Run (no agent loop, no wizard),
// but every slide is a bespoke LLM-authored <svg> instead of a typed
// template. Produces the same Output shape; the renderer's svg branch
// rasterizes each slide full-bleed and assembles a PNG-background .pptx
// (SV-3 will upgrade assembly to native editable shapes).
func (p *Pipeline) RunSVG(ctx context.Context, jobID string, in Input) (*Output, error) {
	var cost Cost
	// SVQ Phase 0: per-stage token ledger feeding the `svg token budget` log.
	var planCost, authorCost, critCost, repairCost, cohCost Cost

	// Parallel planning (skeleton → N parallel section-planners → merge)
	// produces the outline AND the architect spec together, far faster than the
	// serial Outline + ArchitectDeck (the planning bottleneck on big decks). On
	// ANY failure we fall back to the proven serial path, so a flaky parallel
	// plan never blocks a deck. A blueprint forces the serial path (its fixed
	// slide skeleton is honoured only by stages.Outline).
	var (
		outline  *stages.OutlineResult
		planSpec *stages.DeckSpec
		err      error
	)
	if in.BlueprintID == "" {
		var uPlan llm.Usage
		outline, planSpec, uPlan, err = stages.PlanDeckParallel(ctx, p.Router, stages.OutlineParams{
			Topic:         in.Topic,
			Audience:      in.Audience,
			SlideCount:    in.SlideCount,
			Style:         in.Style,
			ReferenceText: in.ReferenceText,
		}, in.ForceTheme)
		if err != nil {
			slog.WarnContext(ctx, "parallel plan failed — falling back to serial outline+architect", "err", err)
			outline, planSpec = nil, nil
		} else {
			cost.add(uPlan)
			planCost.add(uPlan)
		}
	}
	if outline == nil {
		var u1 llm.Usage
		outline, u1, err = stages.Outline(ctx, p.Router, stages.OutlineParams{
			Topic:         in.Topic,
			Audience:      in.Audience,
			SlideCount:    in.SlideCount,
			Style:         in.Style,
			ReferenceText: in.ReferenceText,
			BlueprintID:   in.BlueprintID,
		})
		if err != nil {
			return nil, fmt.Errorf("svg outline: %w", err)
		}
		cost.add(u1)
	}
	if in.ForceTheme != "" {
		outline.Theme = schema.Theme(in.ForceTheme)
	}
	p.emit(ctx, event.NewOutline(outline.Title, len(outline.Slides)))

	theme := string(outline.Theme)
	var content *stages.ContentResult
	var u2 llm.Usage
	if os.Getenv("DW_SVG_ENGINE") == "grid" {
		// Sprint SV-5 fallback — deterministic block-tree layout (no
		// overlap by construction, but "templated" looking). Behind a flag.
		content, u2, err = stages.PlanSlideLayout(ctx, p.Router, outline, theme)
		if err != nil {
			return nil, fmt.Errorf("svg plan: %w", err)
		}
	} else {
		// Sprint PM (default) — ppt-master-grade free-SVG, authored PER
		// SLIDE IN PARALLEL (option A). Each slide is its own LLM call, so
		// wall-time ≈ the slowest single slide (not the sum) AND each
		// slide streams into the live preview the moment its <svg> returns.
		// We seed the session with N blank placeholder slides up front so
		// /page/N.html resolves; onSlide fills each in by index + emits.
		var streamMu sync.Mutex
		placeholders := make([]stages.ContentSlide, len(outline.Slides))
		for i := range placeholders {
			placeholders[i] = stages.ContentSlide{
				Index: i + 1, Template: theme, Layout: schema.LayoutSVG,
				Data: schema.SlideData{SVG: ""},
			}
		}
		streamed := stages.Assemble(outline, &stages.ContentResult{Slides: placeholders})
		streamed.Theme = schema.Theme(theme)
		putStream := func() {
			if p.Sessions != nil && jobID != "" {
				p.Sessions.Put(&SessionState{
					JobID: jobID, Input: in, Outline: outline,
					Deck: &streamed, SlideCount: len(outline.Slides),
				})
			}
		}
		putStream()

		// PM-3: per-slide image resolver — the author marks atmospheric
		// images with href="dw-img://<prompt>"; this generates each via the
		// image searcher (NanoBanana) and returns a served URL.
		imgResolve := func(c context.Context, prompt string) string {
			if p.Images == nil || strings.TrimSpace(prompt) == "" {
				return ""
			}
			r, err := p.Images.Search(c, prompt)
			if err != nil || r == nil {
				return ""
			}
			return r.URL
		}

		// MoA — the Deck Architect plans every slide (layout + chart type +
		// pinned numbers) BEFORE authoring, so the per-slide designers execute
		// ONE consistent plan instead of each improvising layout + inventing
		// (often self-contradictory) numbers. Best-effort: any failure → nil
		// plan → free authoring (pre-MoA behaviour), so it never blocks a deck.
		// The parallel plan already produced the spec; only run the standalone
		// Architect when we fell back to the serial outline (planSpec == nil).
		deckSpec := planSpec
		if deckSpec == nil {
			var uArch llm.Usage
			var archErr error
			deckSpec, uArch, archErr = stages.ArchitectDeck(ctx, p.Router, outline, theme)
			if archErr != nil {
				slog.WarnContext(ctx, "deck architect failed — authoring without a plan", "err", archErr)
				deckSpec = nil
			} else {
				cost.add(uArch)
				planCost.add(uArch)
			}
		}
		if deckSpec != nil {
			charts := 0
			for _, s := range deckSpec.Slides {
				if s.Chart != nil {
					charts++
				}
			}
			slog.InfoContext(ctx, "deck planned", "slides", len(deckSpec.Slides), "charts", charts)
		}

		content, u2, err = stages.AuthorSVGPerSlide(ctx, p.Router, outline, theme, deckSpec, imgResolve, func(idx0 int, cs *stages.ContentSlide) {
			streamMu.Lock()
			if idx0 >= 0 && idx0 < len(streamed.Slides) {
				streamed.Slides[idx0] = schema.Slide{Template: theme, Layout: schema.LayoutSVG, Data: cs.Data}
			}
			streamMu.Unlock()
			putStream()
			// Tell the live-preview iframe stack page idx0+1 is ready (it
			// fetches /page/N.html, which renders the now-stored SVG).
			p.emit(ctx, event.NewSlideRendered(idx0+1, len(cs.Data.SVG)))
			p.emit(ctx, event.NewSlideUpdated(idx0+1))
		})
		if err != nil {
			return nil, fmt.Errorf("svg author: %w", err)
		}
		// Refine: up to 2 visual-review rounds to fix any overflow/overlap.
		// Round 2 RE-MEASURES the slides round 1 touched, so an LLM fix that
		// didn't fully resolve the overrun gets a second pass instead of
		// shipping with it (maxRounds=1 had no verification step).
		var uRepair llm.Usage
		content, uRepair, _ = stages.RepairSVGSlides(ctx, p.Router, theme, content, 2, nil)
		cost.add(uRepair)
		repairCost.add(uRepair)
		// PMQ A4: optional per-slide DESIGN self-critique (assertion titles,
		// data-context, accent restraint, dead space) — the soft rules the
		// geometric repair can't see. No-op unless DW_SVG_SELF_CRITIQUE is
		// set; never regresses a slide. Refined slides are pushed back into
		// the live-preview stream so the user sees the polish land.
		var u3 llm.Usage
		content, u3, _ = stages.CritiqueRefineSVGSlides(ctx, p.Router, theme, content, func(idx0 int, refined string) {
			streamMu.Lock()
			if idx0 >= 0 && idx0 < len(streamed.Slides) {
				streamed.Slides[idx0] = schema.Slide{Template: theme, Layout: schema.LayoutSVG, Data: schema.SlideData{SVG: refined}}
			}
			streamMu.Unlock()
			putStream()
			p.emit(ctx, event.NewSlideUpdated(idx0+1))
		})
		cost.add(u3)
		critCost.add(u3)
		// MoA-3: Coherence Editor — one whole-deck pass that fixes the
		// cross-slide problems no single-slide agent can see (a number that
		// disagrees between slides, a broken narrative, 3+ identical layouts
		// in a row). ON by default (opt out DW_SVG_COHERENCE=off); applies
		// ≤3 targeted fixes, never regresses.
		var u4 llm.Usage
		content, u4, _ = stages.CoherenceReviewSVG(ctx, p.Router, theme, outline.Title, content, func(idx0 int, refined string) {
			streamMu.Lock()
			if idx0 >= 0 && idx0 < len(streamed.Slides) {
				streamed.Slides[idx0] = schema.Slide{Template: theme, Layout: schema.LayoutSVG, Data: schema.SlideData{SVG: refined}}
			}
			streamMu.Unlock()
			putStream()
			p.emit(ctx, event.NewSlideUpdated(idx0+1))
		})
		cost.add(u4)
		cohCost.add(u4)
	}
	cost.add(u2)
	authorCost.add(u2)
	slog.InfoContext(ctx, "svg token budget",
		"slides", len(content.Slides),
		"plan_in", planCost.InputTokens, "plan_out", planCost.OutputTokens,
		"author_in", authorCost.InputTokens, "author_out", authorCost.OutputTokens,
		"critique_out", critCost.OutputTokens,
		"repair_in", repairCost.InputTokens, "repair_out", repairCost.OutputTokens,
		"coherence_out", cohCost.OutputTokens,
		"total_in", cost.InputTokens, "total_out", cost.OutputTokens,
		"cache_read", cost.CacheReadTokens, "cache_create", cost.CacheCreationTokens)

	deck := stages.Assemble(outline, content)
	deck.Theme = schema.Theme(theme)
	// No resolveImages — SVG slides are self-contained vector (no
	// ImageQuery fanout in SV-1; embedded bitmaps are out of scope).

	pptxPath, err := p.Renderer.RenderDeck(ctx, deck)
	if err != nil {
		return nil, fmt.Errorf("svg render: %w", err)
	}
	cost.EstimatedUSD = estimateCost(cost)

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
	c.CacheCreationTokens += u.CacheCreationTokens
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
