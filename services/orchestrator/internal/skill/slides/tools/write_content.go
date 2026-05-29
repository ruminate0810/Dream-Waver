package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// WriteContent is the second tool. Sprint AD upgraded it from a single
// blocking Content() call to a streaming ContentStream — as each slide
// JSON lands the tool:
//   - appends it to State.Deck.Slides (so live-preview iframes can
//     fetch /page/N.html mid-stream and get real content)
//   - emits NewSlideRendered(N) event (so the frontend ComposeStrip
//     and chat thread tick off progress in real time)
//   - spawns a chromedp render goroutine for that slide (so the PNG
//     asset is ready by the time render_deck assembles the PPTX —
//     wall-time win when LLM is slow + chromedp is parallelisable)
//
// Falls back to non-streaming Content() automatically when the stream
// can't be parsed — same final result, just less interactive.
type WriteContent struct {
	Router  llm.Router
	State   SessionAccessor // optional, but required for streaming benefits
	Emitter event.Emitter   // optional; nil = no per-slide events
	// Renderer was originally planned for per-slide chromedp goroutines
	// (full Sprint AD parallelism). Deferred to AD v2 — see comment in
	// Execute() above the onSlide callback for the race-condition
	// rationale. The field is left here as documentation of intent.
	Renderer IncrementalRenderer
}

func (*WriteContent) Name() string { return "write_content" }

func (*WriteContent) Description() string {
	return "Fill in the final per-slide content (title/bullets/body/quote/metric) for an outline produced by plan_outline. " +
		"Call this AFTER plan_outline. Pass the outline JSON verbatim as the `outline` argument. " +
		"Returns a content JSON object that must be passed to render_deck. " +
		"Streams progress per-slide — the live preview pane and ComposeStrip update as each slide is written, not at the end."
}

func (*WriteContent) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["outline"],
		"properties": {
			"outline": {
				"type": "object",
				"description": "The full outline JSON returned by plan_outline. Do NOT modify or summarise; pass it verbatim."
			}
		}
	}`)
}

type writeContentArgs struct {
	Outline stages.OutlineResult `json:"outline"`
}

func (t *WriteContent) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a writeContentArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if len(a.Outline.Slides) == 0 {
		return schema.ToolResult{Error: "outline.slides is empty"}, nil
	}

	// Sprint AD v1: only stream LLM + emit per-slide events; skip the
	// per-slide chromedp goroutine because the existing GetAssets /
	// SetAssets pattern in sessionRenderer is read-modify-write on the
	// entire []SlideAsset slice — concurrent per-slide renders would
	// race and stomp each other's assets. The proper fix needs a
	// SessionState.SetSlideAsset(idx, asset) method with slot-level
	// locking; deferring to AD v2.
	//
	// What we DO get from v1:
	//   - LLM streams slide JSON one at a time → ContentStream onSlide
	//     fires per slide → State.AppendSlide populates state.Deck →
	//     live-preview iframe can fetch /page/N.html with REAL content
	//     immediately (instead of seeing the 404 fallback card)
	//   - per-slide NewSlideRendered events tick the ComposeStrip /
	//     chat progress in real time
	//   - the eventual render_deck batch handles chromedp + PPTX in
	//     one shot (unchanged from pre-AD behaviour)
	//
	// Wall-time end-to-end ≈ unchanged; perceived progress dramatically
	// better.
	onSlide := func(slide *stages.ContentSlide) {
		// Map ContentSlide → schema.Slide for the deck row.
		// Template default is the outline's theme — same convention
		// as stages.Assemble.
		tpl := slide.Template
		if tpl == "" || !isKnownThemeName(tpl) {
			tpl = string(a.Outline.Theme)
		}
		sch := schema.Slide{
			Template:     tpl,
			Layout:       slide.Layout,
			Data:         slide.Data,
			SpeakerNotes: slide.SpeakerNotes,
		}

		var idx0 int
		if t.State != nil {
			// Use the State's AppendSlide if available (via interface
			// extension). Falls back to manual length-tracking otherwise.
			if app, ok := t.State.(slideAppender); ok {
				idx0 = app.AppendSlide(sch)
			} else {
				// Defensive — older SessionAccessor without AppendSlide.
				// Just emit the progress event; live preview iframe will
				// 404 until render_deck populates Deck.
				_, count := t.State.Snapshot()
				idx0 = count
			}
		}

		// Emit per-slide progress event so the frontend ComposeStrip
		// + chat thread tick off as each slide lands.
		if t.Emitter != nil {
			t.Emitter.Emit(ctx, event.NewSlideRendered(idx0+1, len(slide.Data.Title)))
		}
	}

	content, _, err := stages.ContentStream(ctx, t.Router, &a.Outline, onSlide)
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	if t.State != nil {
		t.State.SetContent(content)
	}

	out, err := json.Marshal(content)
	if err != nil {
		return schema.ToolResult{Error: fmt.Sprintf("marshal content: %v", err)}, nil
	}
	return schema.ToolResult{Output: string(out)}, nil
}

// slideAppender is an optional interface extension for SessionAccessor
// — concrete SessionState implements it (Sprint AD). Tools that need
// the streaming append path type-assert and fall through gracefully
// when the concrete state doesn't support it.
type slideAppender interface {
	AppendSlide(s schema.Slide) int
}

// isKnownThemeName mirrors stages.isKnownTheme without importing it
// (would create a cycle since stages doesn't expose it). Keep this
// list in lockstep with stages/stages.go isKnownTheme.
func isKnownThemeName(name string) bool {
	switch name {
	case "minimalist", "corporate", "pitch-deck", "academic", "playful",
		"editorial", "retro", "tech", "zen", "warm", "noir", "azure":
		return true
	}
	return false
}
