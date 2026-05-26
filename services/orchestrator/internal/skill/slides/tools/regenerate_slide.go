package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// RegenerateSlide rewrites the content of one slide via the worker LLM,
// using a natural-language instruction the user gave (e.g. "make it more
// data-driven", "tone down the marketing language"). Cheaper than
// re-running the whole pipeline because we only ask the model for ONE
// slide, not the whole deck.
type RegenerateSlide struct {
	State    SessionAccessor
	Router   llm.Router
	Renderer IncrementalRenderer
}

func (*RegenerateSlide) Name() string { return "regenerate_slide" }

func (*RegenerateSlide) Description() string {
	return "Ask the LLM to rewrite ONE slide based on a natural-language instruction. " +
		"Use when the user wants a tonal shift, a different angle, or 'make it punchier' — " +
		"anything that needs creative rewriting rather than a direct field overwrite. " +
		"Re-renders just that slide after the rewrite."
}

func (*RegenerateSlide) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index", "instruction"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based slide to rewrite."},
			"instruction": {"type": "string", "description": "Plain-language instruction (Chinese OK). E.g. 'add a concrete statistic', 'shorter bullets', 'switch to quote layout'."}
		}
	}`)
}

type regenerateSlideArgs struct {
	SlideIndex  int    `json:"slide_index"`
	Instruction string `json:"instruction"`
}

// regenerateSystemPrompt teaches the worker model the per-slide rewrite
// contract. Returned JSON shape is identical to one element of the
// content_result["slides"] array used elsewhere — so we can drop it
// directly back into SessionState.Content.Slides.
const regenerateSystemPrompt = `You are revising ONE slide of an existing presentation.

You receive the slide's current state as JSON and a natural-language instruction from the user. Produce the new slide as strict JSON with the same shape.

# Output schema (exactly this shape, no fences, no prose)
{
  "index": <number>,
  "template": "<template name>",
  "layout":  "title | section | bullets | content | quote | two-column | data | closing | timeline | comparison | multi-metric | comparison-table | toc | swot | photo-essay | split-image | image-grid | process-flow | bento-grid | pull-quote | before-after | icon-grid | team-roster | code | checklist",
  "data": {
    "title": "...",
    "subtitle": "optional",
    "body":  "optional",
    "bullets": ["..."],
    "quote": "optional",
    "attribution": "optional",
    "metric": "optional",
    "footer": "optional",
    "image_query": "optional 2-5 English words",
    // Layout-specific fields — emit only the ones the chosen layout needs:
    "events":              [{"date":"2024","label":"...","note":"..."}],     // for layout=timeline
    "left_header":         "...", "right_header": "...",                       // for layout=comparison
    "left_items":          ["..."], "right_items":  ["..."],                   // for layout=comparison
    "metrics":             [{"value":"67%","label":"...","delta":"+12%"}],   // for layout=multi-metric
    "table_headers":       ["..."], "table_rows":   [{"cells":[{"text":"..."}]}], // for layout=comparison-table
    "sections":            [{"number":"01","title":"..."}],                    // for layout=toc
    "swot_cells":          {"strengths":["..."],"weaknesses":["..."],"opportunities":["..."],"threats":["..."]}, // for layout=swot
    "image_queries":       ["..."],                                            // for layout=image-grid (3-4 items)
    "steps":               [{"label":"...","description":"..."}],              // for layout=process-flow (3-5 items)
    "bento_cards":         [{"size":"large","title":"...","body":"..."}],     // for layout=bento-grid (1 large + 3-4 small)
    "citation":            "...",                                              // for layout=pull-quote
    "before_image_query":  "...", "after_image_query": "...",                 // for layout=before-after
    "before_label":        "...", "after_label": "...",                       // for layout=before-after (optional)
    "features":            [{"icon":"🚀","label":"...","description":"..."}],// for layout=icon-grid (3-6 items)
    "team_members":        [{"name":"...","role":"...","avatar_query":"...","bio":"..."}], // for layout=team-roster
    "code":                "raw code with real newlines",                      // for layout=code
    "language":            "go|ts|py|sh|sql|json|yaml|rust|css|html",         // for layout=code
    "tasks":               ["审核 X","完成 Y"]                                  // for layout=checklist (3-7 imperative items)
  },
  "speaker_notes": "..."
}

# Rules
- Keep the same index and template. CHANGE the layout when the instruction asks for it (e.g. "改成 code 布局", "用 checklist 显示", "换成 timeline").
- Honour the instruction precisely. If the user says "in Chinese", reply in Chinese; if they say "shorter", make it shorter.
- bullets: ≤ 5 items, each ≤ 18 words.
- When changing layout, EMIT the new layout's required fields AND remove fields the new layout doesn't use (otherwise the renderer shows stale data alongside the new layout).
- Output JSON only.`

func (t *RegenerateSlide) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a regenerateSlideArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	idx := a.SlideIndex - 1
	deck, count := t.State.Snapshot()
	if idx < 0 || idx >= count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index out of range (have %d slides)", count)}, nil
	}

	currentSlideJSON, _ := json.Marshal(deck.Slides[idx])
	user := fmt.Sprintf(
		"Current slide (JSON):\n%s\n\nInstruction:\n%s",
		string(currentSlideJSON), a.Instruction,
	)

	client := t.Router.For("worker")
	resp, err := client.AskTool(ctx, llm.AskToolRequest{
		Model:             t.Router.ModelFor("worker"),
		SystemPrompt:      regenerateSystemPrompt,
		Messages:          []schema.Message{schema.NewUser(user)},
		MaxTokens:         2000,
		EnablePromptCache: true,
	})
	if err != nil {
		return schema.ToolResult{Error: "llm: " + err.Error()}, nil
	}

	var newSlide schema.Slide
	// Reuse the same fence-stripping behaviour stages.Outline uses — we
	// can't import the helper from stages here without a cycle, so inline
	// the trim manually.
	raw := trimJSONFences(resp.Content)
	if err := json.Unmarshal(raw, &newSlide); err != nil {
		return schema.ToolResult{Error: fmt.Sprintf("parse rewritten slide: %v; raw=%s", err, truncate(string(raw), 400))}, nil
	}
	// Preserve the original template if the LLM dropped it.
	if newSlide.Template == "" {
		newSlide.Template = deck.Slides[idx].Template
	}

	// Mutate state under its own lock.
	if err := t.State.UpdateSlide(idx, func(s *schema.Slide) { *s = newSlide }); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	t.State.MarkDirty(idx)

	updatedDeck, _ := t.State.Snapshot()
	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, []int{idx})
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"slide_index": a.SlideIndex,
		"pptx_path":   pptxPath,
		"message":     fmt.Sprintf("Slide %d rewritten and re-rendered.", a.SlideIndex),
	})
	return schema.ToolResult{Output: string(out)}, nil
}

func trimJSONFences(s string) []byte {
	// minimal version of stages.stripFences kept here to avoid cycle
	r := []byte(s)
	for len(r) > 0 && (r[0] == ' ' || r[0] == '\n' || r[0] == '\t' || r[0] == '\r') {
		r = r[1:]
	}
	if len(r) > 7 && string(r[:7]) == "```json" {
		r = r[7:]
	} else if len(r) > 3 && string(r[:3]) == "```" {
		r = r[3:]
	}
	for len(r) > 0 && (r[len(r)-1] == ' ' || r[len(r)-1] == '\n' || r[len(r)-1] == '\t' || r[len(r)-1] == '\r') {
		r = r[:len(r)-1]
	}
	if len(r) > 3 && string(r[len(r)-3:]) == "```" {
		r = r[:len(r)-3]
	}
	return r
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
