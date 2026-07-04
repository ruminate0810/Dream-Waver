package claw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides"
)

// GenerateDeck is the producer worker's tool. It turns the finished report
// into a slide deck by calling the existing slides Pipeline (deterministic
// path) and records the resulting .pptx as the work package's deck artifact,
// emitting claw.artifact.updated(kind="deck"). The pipeline's own slides.*
// progress events are suppressed (run under an empty session id) so they
// don't clutter the claw event stream — the producer's own tool.start/end
// already signals progress. Missing pipeline degrades gracefully.
type GenerateDeck struct {
	Pipeline *slides.Pipeline
	Session  *Session
	Emitter  event.Emitter
}

func (*GenerateDeck) Name() string { return "generate_deck" }

func (*GenerateDeck) Description() string {
	return "Turn the finished report into a slide deck. Pass the topic and (optionally) audience " +
		"and slide_count; the .pptx is added to the work package. Call once, then terminate."
}

func (*GenerateDeck) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["topic"],
		"properties": {
			"topic":       {"type": "string", "description": "Deck subject — the report's topic. Chinese OK."},
			"audience":    {"type": "string", "description": "Who the deck is for (optional)."},
			"slide_count": {"type": "integer", "minimum": 3, "maximum": 20, "description": "How many slides (optional; defaults ~8)."}
		}
	}`)
}

func (t *GenerateDeck) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Topic      string `json:"topic"`
		Audience   string `json:"audience"`
		SlideCount int    `json:"slide_count"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	topic := strings.TrimSpace(p.Topic)
	if topic == "" {
		return schema.ToolResult{Error: "topic is required"}, nil
	}
	if t.Pipeline == nil {
		return schema.ToolResult{Output: "幻灯片生成未启用,跳过 deck。"}, nil
	}

	// Suppress the slides pipeline's own slides.* events on the claw session
	// — the producer worker's tool.start/end already shows progress.
	pctx := event.WithSessionID(ctx, "")
	// previewID doubles as the slides-session id — the work package embeds
	// the live per-page HTML preview (/api/v1/slides/{id}/page/{n}.html).
	previewID := uuid.NewString()
	out, err := t.Pipeline.Run(pctx, previewID, slides.Input{
		Topic:      topic,
		Audience:   strings.TrimSpace(p.Audience),
		SlideCount: p.SlideCount,
	})
	if err != nil {
		return schema.ToolResult{Error: "deck generation failed: " + err.Error()}, nil
	}
	if out == nil || strings.TrimSpace(out.PptxPath) == "" {
		return schema.ToolResult{Output: "deck 生成未产出文件,跳过。"}, nil
	}

	t.Session.SetDeck(out.PptxPath, out.Title, out.SlideCount, previewID)
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("deck", 1, out.SlideCount))
	}
	return schema.ToolResult{Output: fmt.Sprintf("已生成 deck:%s(%d 页)。", out.Title, out.SlideCount)}, nil
}

// DeckEditor is the producer's deck-editing capability (V26 批三): apply a
// natural-language edit to an already-generated deck, keyed by its slides
// session id (the deck's PreviewID). Backed by the slides AgentRunner's
// Continue path via an adapter in cmd/server, so one claw tool inherits the
// full slides edit toolset (换主题/增删页/密度重写/评审…) for free. nil when
// the slides agent runner is absent, which unwires the tool.
type DeckEditor interface {
	EditDeck(ctx context.Context, previewID, instruction string) (path, title string, slideCount int, err error)
}

// EditDeck lets the producer revise the EXISTING work-package deck instead of
// regenerating it from scratch. The instruction is free-form ("把整个 deck 换成
// corporate 风", "第 3 页太满,精简一下", "在结尾加一页总结") — the slides edit
// agent picks the right internal tool. Requires a deck to exist.
type EditDeck struct {
	Editor  DeckEditor
	Session *Session
	Emitter event.Emitter
}

func (*EditDeck) Name() string { return "edit_deck" }

func (*EditDeck) Description() string {
	return "Revise the EXISTING slide deck (do not regenerate). Pass a natural-language instruction: " +
		"change theme, add/delete/reorder/merge/split a slide, tighten a dense slide, restyle, or fix " +
		"content. Only works after generate_deck. Call once per edit, then terminate."
}

func (*EditDeck) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["instruction"],
		"properties": {
			"instruction": {"type": "string", "description": "What to change, in natural language (Chinese OK), e.g. 把整个 deck 换成 corporate 风 / 第 3 页太满，精简 / 在结尾加一页总结。"}
		}
	}`)
}

func (t *EditDeck) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Instruction string `json:"instruction"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	instr := strings.TrimSpace(p.Instruction)
	if instr == "" {
		return schema.ToolResult{Error: "instruction is required"}, nil
	}
	deck := t.Session.DeckArtifact()
	if deck == nil || strings.TrimSpace(deck.PreviewID) == "" {
		return schema.ToolResult{Error: "还没有可编辑的 deck — 先用 generate_deck 生成。"}, nil
	}

	// Suppress the slides edit agent's own slides.* events (run under an
	// empty session id) — the producer's tool.start/end already shows
	// progress, same as GenerateDeck.
	ectx := event.WithSessionID(ctx, "")
	path, title, count, err := t.Editor.EditDeck(ectx, deck.PreviewID, instr)
	if err != nil {
		return schema.ToolResult{Error: "deck 编辑失败: " + err.Error()}, nil
	}

	t.Session.SetDeck(path, title, count, deck.PreviewID)
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("deck", 1, count))
	}
	return schema.ToolResult{Output: fmt.Sprintf("已改好 deck:%s(%d 页)。", title, count)}, nil
}
