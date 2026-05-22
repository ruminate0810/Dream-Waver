package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// SessionAccessor is the minimum the slides-domain tools need from
// SessionState. Defined as an interface here (instead of importing
// slides.SessionState directly) so this package stays a leaf of the
// import graph — slides imports tools, never the other way round.
//
// Implementations must be safe to mutate concurrently; SessionState uses
// an internal mutex to guarantee that.
type SessionAccessor interface {
	// Snapshot returns the current deck + slide count. Caller may read
	// freely but should not mutate the returned slices.
	Snapshot() (deck *schema.Deck, slideCount int)
	Title() string

	// Mutators used by edit tools.
	UpdateSlide(index int, fn func(s *schema.Slide)) error
	DeleteSlide(index int) error
	InsertSlide(index int, s schema.Slide) error
	MarkDirty(indices ...int)

	// SetTheme rewrites Deck.Theme and every slide.Template to the new
	// theme name so the next render picks up the new template family.
	SetTheme(theme schema.Theme)
	// SetBrand replaces Deck.Brand with the given pointer (nil clears
	// previously applied brand). The renderer injects the brand as CSS
	// variables at the top of every served slide HTML.
	SetBrand(b *schema.Brand)

	// Setters used by the initial-generation tools to persist intermediate
	// LLM outputs and the final assembled deck — so a follow-up turn can
	// re-read them without re-running planning.
	SetOutline(o any)
	SetContent(c any)
	SetDeck(d *schema.Deck)
	SetPptxPath(path string)
}

// EditSlideText is the cheapest, most direct edit: overwrite a single
// SlideData field (title / subtitle / body / metric / quote / attribution /
// footer or a specific bullet) on one slide. No LLM call — the change is
// purely mechanical. Best when the user types something like
//   "把第 3 页的标题改成 'X'"
// or wants to fix a typo.
type EditSlideText struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*EditSlideText) Name() string { return "edit_slide_text" }

func (*EditSlideText) Description() string {
	return "Overwrite a single text field on one slide and re-render just that page. " +
		"Use for direct edits like changing a title, fixing a typo, or rewriting a single bullet. " +
		"Fields: title | subtitle | body | metric | quote | attribution | footer. " +
		"For bullets, pass field='bullets' and bullet_index (0-based)."
}

func (*EditSlideText) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index", "field", "value"],
		"properties": {
			"slide_index":  {"type": "integer", "minimum": 1, "description": "1-based index of the slide to edit."},
			"field":        {"type": "string", "enum": ["title","subtitle","body","metric","quote","attribution","footer","bullets"]},
			"value":        {"type": "string", "description": "New text content for the field (or for the specific bullet when field='bullets')."},
			"bullet_index": {"type": "integer", "minimum": 0, "description": "Required when field='bullets'. 0-based index into the bullets array."}
		}
	}`)
}

type editSlideTextArgs struct {
	SlideIndex  int    `json:"slide_index"`
	Field       string `json:"field"`
	Value       string `json:"value"`
	BulletIndex int    `json:"bullet_index"`
}

func (t *EditSlideText) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a editSlideTextArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	idx := a.SlideIndex - 1 // convert to 0-based
	field := strings.ToLower(strings.TrimSpace(a.Field))

	err := t.State.UpdateSlide(idx, func(s *schema.Slide) {
		switch field {
		case "title":
			s.Data.Title = a.Value
		case "subtitle":
			s.Data.Subtitle = a.Value
		case "body":
			s.Data.Body = a.Value
		case "metric":
			s.Data.Metric = a.Value
		case "quote":
			s.Data.Quote = a.Value
		case "attribution":
			s.Data.Attribution = a.Value
		case "footer":
			s.Data.Footer = a.Value
		case "bullets":
			// Pad if needed so callers can extend via this same tool.
			for len(s.Data.Bullets) <= a.BulletIndex {
				s.Data.Bullets = append(s.Data.Bullets, "")
			}
			s.Data.Bullets[a.BulletIndex] = a.Value
		}
	})
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	t.State.MarkDirty(idx)
	deck, _ := t.State.Snapshot()
	pptxPath, err := t.Renderer.RenderIncremental(ctx, *deck, []int{idx})
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"slide_index": a.SlideIndex,
		"field":       field,
		"pptx_path":   pptxPath,
		"message":     fmt.Sprintf("Slide %d %s updated; re-rendered.", a.SlideIndex, field),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
