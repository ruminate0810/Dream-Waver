// Package stages exposes the pure LLM + assembly steps of the slides
// pipeline as package-level functions. Two callers depend on this code:
//
//  1. slides.Pipeline.Run — the deterministic, fast path. Calls
//     Outline → Content → Assemble in sequence.
//  2. slides/tools — the per-tool wrappers used by the SlidesAgent. Each
//     tool calls one stage and returns its JSON result back to the LLM
//     for the next step.
//
// Keeping the logic here (and Pipeline/tools as thin orchestrators) means
// the prompt → JSON → struct contract has a single source of truth.
package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
)

// askWithRetry wraps a single LLM call so that two recurring DeepSeek
// failure modes don't bubble up as opaque errors:
//
//   1. Empty content (deepseek provider now returns "empty content
//      (finish_reason=…)" — we treat as transient)
//   2. JSON parse failure on response (LLM dropped a partial JSON or
//      returned prose; one retry usually recovers)
//
// One retry only. Backoff 800ms. If the second attempt also fails, the
// error includes both failure reasons so the user can see what's going
// wrong (network? prompt-too-long? model glitch?).
//
// `parse` runs against the LLM's content string. Return non-nil only
// if the content is unusable; that triggers the retry. Provider-level
// errors (network drops, empty-content guard, rate limits) ALWAYS
// trigger the retry regardless of parse.
func askWithRetry(
	ctx context.Context,
	client llm.Client,
	stage string,
	req llm.AskToolRequest,
	parse func(content string) error,
) (*llm.AskToolResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		resp, err := client.AskTool(ctx, req)
		if err == nil {
			if perr := parse(resp.Content); perr == nil {
				return resp, nil
			} else {
				err = perr
			}
		}
		lastErr = err
		// Don't sleep on the last attempt.
		if attempt < 2 {
			slog.Warn("llm transient failure — retrying",
				"stage", stage, "attempt", attempt, "err", err.Error())
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(800 * time.Millisecond):
			}
		}
	}
	return nil, fmt.Errorf("%s failed after 2 attempts: %w", stage, lastErr)
}

// ─── Input / output structures ──────────────────────────────────────

// OutlineParams is the fixed input shape Outline expects. Pipeline maps
// its richer Input struct down to this; tools accept it directly via JSON.
type OutlineParams struct {
	Topic         string `json:"topic"`
	Audience      string `json:"audience,omitempty"`
	SlideCount    int    `json:"slide_count,omitempty"`
	Style         string `json:"style,omitempty"`
	ReferenceText string `json:"reference_text,omitempty"`
}

// OutlineSlide is one row in the deck's table of contents.
type OutlineSlide struct {
	Index        int      `json:"index"`
	Type         string   `json:"type"`
	Headline     string   `json:"headline"`
	KeyPoints    []string `json:"key_points"`
	SpeakerNotes string   `json:"speaker_notes"`
}

// OutlineResult is what the planner LLM produces. The Theme field carries
// forward into Content + Assemble unless an explicit override wins.
type OutlineResult struct {
	Title    string         `json:"title"`
	Subtitle string         `json:"subtitle"`
	Theme    schema.Theme   `json:"theme"`
	Slides   []OutlineSlide `json:"slides"`
}

// ContentSlide is the per-page payload the worker LLM produces. Layout is
// the typed enum the templates render against; Data carries the actual
// strings (title, bullets, etc).
type ContentSlide struct {
	Index        int                `json:"index"`
	Template     string             `json:"template"`
	Layout       schema.SlideLayout `json:"layout"`
	Data         schema.SlideData   `json:"data"`
	SpeakerNotes string             `json:"speaker_notes,omitempty"`
}

// ContentResult is the full deck's content payload.
type ContentResult struct {
	Slides []ContentSlide `json:"slides"`
}

// ─── Stage 1: Outline ────────────────────────────────────────────────

// Outline asks the planner LLM to produce a structured table of contents
// for the given topic/audience/length/style. Returns the parsed
// OutlineResult, the LLM's token-usage stats, and any error.
func Outline(ctx context.Context, router llm.Router, in OutlineParams) (*OutlineResult, llm.Usage, error) {
	if in.SlideCount <= 0 {
		in.SlideCount = 8
	}
	if in.SlideCount > 40 {
		in.SlideCount = 40
	}
	user := fmt.Sprintf(
		"Topic: %s\nAudience: %s\nSlide count: %d\nStyle preference: %s\nReference material:\n%s",
		in.Topic,
		defaultStr(in.Audience, "general business"),
		in.SlideCount,
		defaultStr(in.Style, "auto"),
		in.ReferenceText,
	)
	client := router.For("planner")
	var out OutlineResult
	resp, err := askWithRetry(ctx, client, "outline", llm.AskToolRequest{
		Model:             router.ModelFor("planner"),
		SystemPrompt:      prompts.Outline,
		Messages:          []schema.Message{schema.NewUser(user)},
		MaxTokens:         3000,
		EnablePromptCache: true,
	}, func(content string) error {
		// Reset the struct on each retry so a partial unmarshal from a
		// prior attempt doesn't leak through.
		out = OutlineResult{}
		if err := json.Unmarshal(stripFences(content), &out); err != nil {
			return fmt.Errorf("parse outline json: %w; raw=%q", err, truncate(content, 200))
		}
		return nil
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}
	if out.Theme == "" {
		out.Theme = schema.ThemeMinimalist
	}
	return &out, resp.Usage, nil
}

// ─── Stage 2: Content ────────────────────────────────────────────────

// Content asks the worker LLM to fill in the per-slide content based on
// an already-produced outline. The outline is serialised as JSON in the
// user message so the model has the full plan in context (Claude / DeepSeek
// prompt caching makes this cheap on repeated calls within a session).
func Content(ctx context.Context, router llm.Router, outline *OutlineResult) (*ContentResult, llm.Usage, error) {
	outlineJSON, _ := json.Marshal(outline)
	user := fmt.Sprintf("Outline (JSON):\n%s\n\nProduce the final slide content.", string(outlineJSON))

	client := router.For("worker")
	var out ContentResult
	resp, err := askWithRetry(ctx, client, "content", llm.AskToolRequest{
		Model:             router.ModelFor("worker"),
		SystemPrompt:      prompts.Content,
		Messages:          []schema.Message{schema.NewUser(user)},
		MaxTokens:         6000,
		EnablePromptCache: true,
	}, func(content string) error {
		out = ContentResult{}
		if err := json.Unmarshal(stripFences(content), &out); err != nil {
			return fmt.Errorf("parse content json: %w; raw=%q", err, truncate(content, 200))
		}
		return nil
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}
	return &out, resp.Usage, nil
}

// ─── Single-slide writer (used by add_slide tool) ───────────────────

// WriteOneParams describes one new slide to be inserted into an
// existing deck. NeighborTitles carries the (at most) two adjacent
// slide titles so the worker LLM can match tone — both can be empty
// when inserting at the very start or end of the deck.
type WriteOneParams struct {
	DeckTitle      string
	DeckTheme      schema.Theme
	Layout         schema.SlideLayout
	Position       int // 1-based position the new slide will occupy
	Instruction    string
	NeighborTitles []string
}

// WriteOneSlide asks the worker LLM to produce a single slide's
// SlideData payload. Used by the add_slide tool — significantly cheaper
// than re-running stages.Content for the whole deck.
func WriteOneSlide(ctx context.Context, router llm.Router, p WriteOneParams) (*schema.SlideData, llm.Usage, error) {
	if p.Layout == "" {
		p.Layout = schema.LayoutContent
	}
	user := fmt.Sprintf(
		"Deck title: %s\nDeck theme: %s\nInsert position: %d (1-based)\nLayout: %s\nNeighbor titles: %s\n\nInstruction:\n%s",
		p.DeckTitle, p.DeckTheme, p.Position, p.Layout,
		strings.Join(p.NeighborTitles, " · "),
		p.Instruction,
	)

	client := router.For("worker")
	var data schema.SlideData
	resp, err := askWithRetry(ctx, client, "slide_one", llm.AskToolRequest{
		Model:             router.ModelFor("worker"),
		SystemPrompt:      prompts.SlideOne,
		Messages:          []schema.Message{schema.NewUser(user)},
		MaxTokens:         1200,
		EnablePromptCache: true,
	}, func(content string) error {
		data = schema.SlideData{}
		if err := json.Unmarshal(stripFences(content), &data); err != nil {
			return fmt.Errorf("parse slide_one json: %w; raw=%q", err, truncate(content, 200))
		}
		return nil
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}
	return &data, resp.Usage, nil
}

// ─── Stage 3: Assemble ───────────────────────────────────────────────

// Assemble joins outline-level metadata (title, theme) with the per-slide
// content into the typed schema.Deck the renderer consumes. Slides without
// an explicit template fall back to the deck-level theme so the renderer
// can always resolve a template file.
func Assemble(o *OutlineResult, c *ContentResult) schema.Deck {
	slides := make([]schema.Slide, 0, len(c.Slides))
	for _, s := range c.Slides {
		tpl := s.Template
		if tpl == "" {
			tpl = string(o.Theme)
		}
		slides = append(slides, schema.Slide{
			Template:     tpl,
			Layout:       s.Layout,
			Data:         s.Data,
			SpeakerNotes: s.SpeakerNotes,
		})
	}
	return schema.Deck{
		Title:  o.Title,
		Theme:  o.Theme,
		Slides: slides,
	}
}

// ─── Shared helpers ──────────────────────────────────────────────────

func defaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// stripFences peels off the ```json ... ``` wrappers LLMs sometimes return
// despite being told to emit raw JSON. Idempotent on already-bare input.
func stripFences(s string) []byte {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return []byte(strings.TrimSpace(s))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
