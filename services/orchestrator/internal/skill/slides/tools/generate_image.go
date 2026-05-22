package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// GenerateImage swaps out the hero image of ONE slide for a fresh
// AI-generated picture (or a stock photo when no AI provider is
// configured). Uses the composite image.Searcher main.go already wires
// — so when GOOGLE_API_KEY is set the call goes to nano-banana
// (Gemini 2.5 Flash Image); otherwise it falls through to Unsplash; if
// neither is configured the tool returns a clean error rather than
// silently doing nothing.
//
// Cheaper than RegenerateSlide because no worker-LLM call is needed —
// the tool only touches image bytes + re-renders the single slide.
type GenerateImage struct {
	State    SessionAccessor
	Images   image.Searcher
	Renderer IncrementalRenderer
}

func (*GenerateImage) Name() string { return "generate_image" }

func (*GenerateImage) Description() string {
	return "Generate (or search for) a hero image for ONE slide and swap it in. " +
		"Use when the user asks for a specific image (\"给第 3 页加一张电动车电池的图\", " +
		"\"换张更未来感的封面图\"). The instruction should be a short visual description, " +
		"ideally in English (2–8 words like 'urban skyline night' or 'lithium battery cad'); " +
		"Chinese also works but English yields better results from both Gemini and Unsplash. " +
		"Re-renders just the target slide. No LLM call."
}

func (*GenerateImage) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index", "instruction"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based slide whose hero image to replace."},
			"instruction": {"type": "string", "description": "Short visual description (English preferred). E.g. 'quantum computing chip', 'team collaborating office', 'urban skyline night'."}
		}
	}`)
}

type generateImageArgs struct {
	SlideIndex  int    `json:"slide_index"`
	Instruction string `json:"instruction"`
}

func (t *GenerateImage) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a generateImageArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	idx := a.SlideIndex - 1
	_, count := t.State.Snapshot()
	if idx < 0 || idx >= count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index out of range (have %d slides)", count)}, nil
	}
	query := strings.TrimSpace(a.Instruction)
	if query == "" {
		return schema.ToolResult{Error: "instruction is required"}, nil
	}

	if t.Images == nil {
		return schema.ToolResult{Error: "no image provider configured — set GOOGLE_API_KEY (AI generation) or UNSPLASH_ACCESS_KEY (stock photos)"}, nil
	}
	r, err := t.Images.Search(ctx, query)
	if err != nil {
		return schema.ToolResult{Error: "image provider: " + err.Error()}, nil
	}
	if r == nil {
		return schema.ToolResult{Error: "no provider returned an image — verify GOOGLE_API_KEY is set and the prompt isn't safety-blocked"}, nil
	}

	// Persist URL + attribution + the user's query (so subsequent edits
	// or re-renders can trace where the picture came from).
	if err := t.State.UpdateSlide(idx, func(s *schema.Slide) {
		s.Data.Image = r.URL
		s.Data.ImageCredit = r.Credit
		s.Data.ImageQuery = query
	}); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	t.State.MarkDirty(idx)

	updated, _ := t.State.Snapshot()
	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updated, []int{idx})
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"slide_index": a.SlideIndex,
		"image_url":   r.URL,
		"credit":      r.Credit,
		"pptx_path":   pptxPath,
		"message":     fmt.Sprintf("Slide %d hero image regenerated via %s.", a.SlideIndex, providerName(r.Credit)),
	})
	return schema.ToolResult{Output: string(out)}, nil
}

// providerName teases out a friendly label from the Result.Credit
// string so the success message reads naturally. Strictly cosmetic.
func providerName(credit string) string {
	switch {
	case strings.Contains(credit, "Gemini"):
		return "Gemini"
	case strings.Contains(credit, "Unsplash"):
		return "Unsplash"
	default:
		return "image provider"
	}
}
