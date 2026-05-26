package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// AnalyzeDeck is the Sprint O edit-turn introspection tool. Pure
// read — no LLM call, no state mutation. Returns a structured
// summary of the live deck so the agent can REASON about its shape
// before deciding what to edit.
//
// Typical use: user says something deck-level like "make the whole
// thing more persuasive". The agent calls analyze_deck → reads the
// per-slide titles + layouts → decides which 2-3 slides actually
// need rewriting → calls regenerate_slide on those specific indices
// instead of blindly regenerating everything.
//
// Output shape:
//   {
//     "title": "DeepSeek V4 — for investors",
//     "subtitle": "...",
//     "theme": "editorial",
//     "brand": {primary_color, accent_color, font_family} | null,
//     "slide_count": 8,
//     "layout_distribution": {"title": 1, "content": 4, "bullets": 2, "closing": 1},
//     "slides": [
//       {"index": 1, "title": "...", "layout": "title", "body_excerpt": "first 60 chars of body"},
//       ...
//     ]
//   }
type AnalyzeDeck struct {
	State SessionAccessor
}

func (*AnalyzeDeck) Name() string { return "analyze_deck" }

func (*AnalyzeDeck) Description() string {
	return "Read-only introspection: returns the current deck's structure (title, theme, brand, " +
		"slide count, layout distribution, per-slide title + body excerpt). Use FIRST in any " +
		"deck-level edit turn (\"make it more X\", \"tighten the whole thing\") so you can pick " +
		"specific slides to revise instead of regenerating blindly. No LLM call; cheap."
}

func (*AnalyzeDeck) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
}

type analyzeSlide struct {
	Index       int    `json:"index"`
	Title       string `json:"title"`
	Layout      string `json:"layout"`
	BodyExcerpt string `json:"body_excerpt,omitempty"`
	BulletCount int    `json:"bullet_count,omitempty"`
	HasImage    bool   `json:"has_image,omitempty"`
	HasMetric   bool   `json:"has_metric,omitempty"`
}

type analyzeResult struct {
	Title              string         `json:"title"`
	Theme              string         `json:"theme"`
	Brand              *schema.Brand  `json:"brand,omitempty"`
	SlideCount         int            `json:"slide_count"`
	LayoutDistribution map[string]int `json:"layout_distribution"`
	Slides             []analyzeSlide `json:"slides"`
}

func (t *AnalyzeDeck) Execute(_ context.Context, _ json.RawMessage) (schema.ToolResult, error) {
	deck, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck in session"}, nil
	}

	dist := map[string]int{}
	slides := make([]analyzeSlide, 0, count)
	for i, s := range deck.Slides {
		layout := string(s.Layout)
		if layout == "" {
			layout = "content"
		}
		dist[layout]++

		row := analyzeSlide{
			Index:       i + 1,
			Title:       s.Data.Title,
			Layout:      layout,
			BulletCount: len(s.Data.Bullets),
			HasImage:    s.Data.Image != "" || s.Data.ImageQuery != "",
			HasMetric:   s.Data.Metric != "",
		}
		if body := strings.TrimSpace(s.Data.Body); body != "" {
			if len(body) > 80 {
				body = body[:80] + "…"
			}
			row.BodyExcerpt = body
		}
		slides = append(slides, row)
	}

	res := analyzeResult{
		Title:              deck.Title,
		Theme:              string(deck.Theme),
		Brand:              deck.Brand,
		SlideCount:         count,
		LayoutDistribution: dist,
		Slides:             slides,
	}
	out, err := json.Marshal(res)
	if err != nil {
		return schema.ToolResult{Error: fmt.Sprintf("marshal analyze result: %v", err)}, nil
	}
	return schema.ToolResult{Output: string(out)}, nil
}
