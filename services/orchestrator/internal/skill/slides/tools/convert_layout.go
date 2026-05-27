package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// ConvertLayout switches one slide's layout type without calling the
// worker LLM. Used when the user wants visual variety without
// re-writing content — e.g. "把第 3 页改成 comparison 风格".
//
// Strategy: validate that the new layout's REQUIRED fields are present
// on the current slide's SlideData. If yes, just flip Layout. If no,
// return a ConflictError listing the missing fields so the agent can
// either (a) fall back to regenerate_slide (which writes fresh content
// for the new layout) or (b) ask the user. ~10× faster than regenerate
// because there's no LLM call — just a struct mutation + chromedp
// re-render of the one page.
type ConvertLayout struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*ConvertLayout) Name() string { return "convert_layout" }

func (*ConvertLayout) Description() string {
	return "Switch ONE slide's layout type without calling the LLM. Use when the user wants visual variety on a specific page without rewriting the content (\"把第 3 页改成 comparison\", \"把这页换成 timeline 视图\"). The slide's existing Title/Body/Bullets/etc are preserved; only the visual layout changes. Returns ConflictError when the target layout REQUIRES fields the current slide doesn't have — agent should then use regenerate_slide instead. ~10× faster than regenerate_slide when compatible."
}

func (*ConvertLayout) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index", "new_layout"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based slide index to convert."},
			"new_layout":  {"type": "string", "description": "Target layout. One of: title | section | bullets | content | quote | two-column | data | closing | timeline | comparison | multi-metric | comparison-table | toc | swot | photo-essay | split-image | image-grid | process-flow | bento-grid | pull-quote | before-after | icon-grid | team-roster | code | checklist | html."}
		}
	}`)
}

type convertLayoutArgs struct {
	SlideIndex int    `json:"slide_index"`
	NewLayout  string `json:"new_layout"`
}

func (t *ConvertLayout) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a convertLayoutArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	deck, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck to convert"}, nil
	}
	if a.SlideIndex < 1 || a.SlideIndex > count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index out of range (have %d slides)", count)}, nil
	}
	newLayoutStr := strings.TrimSpace(a.NewLayout)
	if !schema.IsValidSlideLayout(newLayoutStr) {
		return schema.ToolResult{Error: fmt.Sprintf("unknown new_layout %q; see Parameters for the valid list", a.NewLayout)}, nil
	}
	newLayout := schema.SlideLayout(newLayoutStr)

	idx0 := a.SlideIndex - 1
	current := deck.Slides[idx0]

	if current.Layout == newLayout {
		// No-op: same layout. Surface as "did nothing" so the agent
		// doesn't waste a critic_deck round on a non-edit.
		out, _ := json.Marshal(map[string]any{
			"slide_index": a.SlideIndex,
			"new_layout":  string(newLayout),
			"message":     "No-op: slide is already this layout.",
		})
		return schema.ToolResult{Output: string(out)}, nil
	}

	// Compatibility check: does the current slide's data satisfy the
	// new layout's REQUIRED fields?
	missing := missingFieldsForLayout(newLayout, current.Data)
	if len(missing) > 0 {
		// Conflict. Agent should fall back to regenerate_slide which
		// writes fresh content shaped for the new layout.
		out, _ := json.Marshal(map[string]any{
			"slide_index":     a.SlideIndex,
			"new_layout":      string(newLayout),
			"old_layout":      string(current.Layout),
			"missing_fields":  missing,
			"conflict":        true,
			"recommendation":  "regenerate_slide",
			"message": fmt.Sprintf(
				"Cannot convert slide %d from %s → %s without LLM: target layout requires fields %v which the current slide doesn't have. Use regenerate_slide instead — it will write fresh content shaped for %s.",
				a.SlideIndex, current.Layout, newLayout, missing, newLayout,
			),
		})
		// Return as a normal result (Output set, no Error) so the
		// agent's `critic_deck` reasoning loop sees the conflict and
		// can route accordingly. Marking it as Error would short-
		// circuit the loop.
		return schema.ToolResult{Output: string(out)}, nil
	}

	if err := t.State.UpdateSlide(idx0, func(s *schema.Slide) {
		s.Layout = newLayout
	}); err != nil {
		return schema.ToolResult{Error: "update slide: " + err.Error()}, nil
	}
	t.State.MarkDirty(idx0)

	updatedDeck, _ := t.State.Snapshot()
	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, []int{idx0})
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"slide_index": a.SlideIndex,
		"old_layout":  string(current.Layout),
		"new_layout":  string(newLayout),
		"pptx_path":   pptxPath,
		"message": fmt.Sprintf(
			"Slide %d converted from %s → %s. Content fields preserved.",
			a.SlideIndex, current.Layout, newLayout,
		),
	})
	return schema.ToolResult{Output: string(out)}, nil
}

// missingFieldsForLayout returns the names of REQUIRED fields the new
// layout needs but the existing SlideData doesn't carry. Empty slice
// = compatible. Layouts not listed are "free" — they only require
// Title (which every slide has by convention).
//
// This mirrors the per-layout REQUIRED rules documented in
// prompts/content.md. Keep in sync when adding new layout types.
func missingFieldsForLayout(layout schema.SlideLayout, d schema.SlideData) []string {
	var missing []string
	add := func(name string, present bool) {
		if !present {
			missing = append(missing, name)
		}
	}

	switch layout {
	case schema.LayoutQuote, schema.LayoutPullQuote:
		add("quote", d.Quote != "")
		add("attribution", d.Attribution != "")
	case schema.LayoutData:
		add("metric", d.Metric != "")
	case schema.LayoutMultiMetric:
		add("metrics", len(d.Metrics) >= 2)
	case schema.LayoutTimeline:
		add("events", len(d.Events) >= 3)
	case schema.LayoutComparison:
		add("left_header", d.LeftHeader != "")
		add("right_header", d.RightHeader != "")
		add("left_items", len(d.LeftItems) > 0)
		add("right_items", len(d.RightItems) > 0)
	case schema.LayoutComparisonTable:
		add("table_headers", len(d.TableHeaders) > 0)
		add("table_rows", len(d.TableRows) > 0)
	case schema.LayoutTOC:
		add("sections", len(d.Sections) > 0)
	case schema.LayoutSWOT:
		add("strengths", len(d.Strengths) > 0)
		add("weaknesses", len(d.Weaknesses) > 0)
		add("opportunities", len(d.Opportunities) > 0)
		add("threats", len(d.Threats) > 0)
	case schema.LayoutPhotoEssay, schema.LayoutSplitImage:
		add("image_query OR resolved image", d.ImageQuery != "" || d.Image != "")
	case schema.LayoutImageGrid:
		add("image_queries (3 or 4) OR resolved images", len(d.ImageQueries) >= 3 || len(d.Images) >= 3)
	case schema.LayoutProcessFlow:
		add("steps (3-5)", len(d.Steps) >= 3)
	case schema.LayoutBentoGrid:
		add("bento_cards (4-5)", len(d.BentoCards) >= 4)
	case schema.LayoutBeforeAfter:
		add("before_image_query", d.BeforeImageQuery != "" || d.BeforeImage != "")
		add("after_image_query", d.AfterImageQuery != "" || d.AfterImage != "")
	case schema.LayoutIconGrid:
		add("features (3, 4, or 6)", len(d.Features) >= 3)
	case schema.LayoutTeamRoster:
		add("team_members (3-6)", len(d.TeamMembers) >= 3)
	case schema.LayoutCode:
		add("code", d.Code != "")
	case schema.LayoutChecklist:
		add("tasks (3-7)", len(d.Tasks) >= 3)
	case schema.LayoutHTML:
		add("html", d.HTML != "")
	// title / section / closing / bullets / content / two-column —
	// fall through, no required-field check (Title is universal).
	}

	return missing
}
