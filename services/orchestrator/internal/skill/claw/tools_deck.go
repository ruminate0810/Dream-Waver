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
