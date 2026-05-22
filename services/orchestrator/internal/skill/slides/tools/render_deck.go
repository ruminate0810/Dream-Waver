package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// RenderDeck is the terminal initial-generation tool. It accepts the
// outline + content the previous tools produced, assembles them into a
// schema.Deck, resolves optional hero images via Unsplash, then hands off
// to the IncrementalRenderer (same path the edit tools take) to do the
// heavy chromedp + unioffice work.
//
// After a successful render, the assembled Deck is stored on
// SessionState so follow-up edit tools see the same deck instance.
//
// The agent receives back a small JSON envelope with pptx_path,
// slide_count, and title — enough to decide it can call terminate.
type RenderDeck struct {
	Renderer IncrementalRenderer
	Images   image.Searcher // optional; NoopSearcher = no hero images
	State    SessionAccessor // optional
}

func (*RenderDeck) Name() string { return "render_deck" }

func (*RenderDeck) Description() string {
	return "Render the final .pptx from the outline + content. " +
		"Call this AFTER write_content. Both arguments must be the JSON objects returned by the previous two tools, verbatim. " +
		"After this returns successfully, call terminate to finish."
}

func (*RenderDeck) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["outline", "content"],
		"properties": {
			"outline":      {"type": "object", "description": "Outline JSON from plan_outline."},
			"content":      {"type": "object", "description": "Content JSON from write_content."},
			"force_theme":  {"type": "string", "description": "Optional theme override that wins over the planner's pick. One of: minimalist | corporate | pitch-deck | academic | playful."}
		}
	}`)
}

type renderDeckArgs struct {
	Outline    stages.OutlineResult `json:"outline"`
	Content    stages.ContentResult `json:"content"`
	ForceTheme string               `json:"force_theme,omitempty"`
}

type renderDeckResult struct {
	PptxPath   string `json:"pptx_path"`
	Title      string `json:"title"`
	SlideCount int    `json:"slide_count"`
}

func (t *RenderDeck) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a renderDeckArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if len(a.Outline.Slides) == 0 || len(a.Content.Slides) == 0 {
		return schema.ToolResult{Error: "outline and content must both have slides"}, nil
	}
	if a.ForceTheme != "" {
		a.Outline.Theme = schema.Theme(a.ForceTheme)
	}

	deck := stages.Assemble(&a.Outline, &a.Content)
	// Hero-image resolution mirrors Pipeline behaviour — silent no-op when
	// no Unsplash key is configured.
	resolveImagesShim(ctx, t.Images, &deck)

	// Initial render = "all slides are dirty". The IncrementalRenderer
	// abstraction collapses initial generation and follow-up edits into
	// one call site; the adapter handles cache.
	pptxPath, err := t.Renderer.RenderIncremental(ctx, deck, allSlideIndices(len(deck.Slides)))
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	if t.State != nil {
		t.State.SetDeck(&deck)
		t.State.SetPptxPath(pptxPath)
	}

	out, err := json.Marshal(renderDeckResult{
		PptxPath:   pptxPath,
		Title:      a.Outline.Title,
		SlideCount: len(deck.Slides),
	})
	if err != nil {
		return schema.ToolResult{Error: fmt.Sprintf("marshal result: %v", err)}, nil
	}
	return schema.ToolResult{Output: string(out)}, nil
}
