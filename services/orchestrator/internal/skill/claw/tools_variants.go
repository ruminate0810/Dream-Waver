package claw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// VariantMaker is the designer's multi-option generator (V26 批一): one
// prompt, several visual takes. Backed by the design bridge's
// GenerateVariants (NanoBanana) via an adapter in cmd/server — nil when the
// bridge is down, which unwires the tool.
type VariantMaker interface {
	GenerateVariants(ctx context.Context, prompt string, count int) ([]string, error)
}

// GenerateVariantsTool renders 2–4 takes of one visual idea and lands each
// as a work-package figure (方案 A/B/C…), so the user can pick a direction
// instead of accepting the first draw.
type GenerateVariantsTool struct {
	Variants VariantMaker
	Session  *Session
	Emitter  event.Emitter
}

func (*GenerateVariantsTool) Name() string { return "generate_variants" }

func (*GenerateVariantsTool) Description() string {
	return "Generate 2-4 visual VARIANTS of one idea (same subject, different takes) so the user " +
		"can pick a direction. Use when the brief is open-ended or the user asks for options. " +
		"Each variant lands as a separate work-package figure labeled 方案 A/B/C."
}

func (*GenerateVariantsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["prompt"],
		"properties": {
			"prompt": {"type": "string", "description": "The shared visual idea, in English, concrete and specific."},
			"count":  {"type": "integer", "minimum": 2, "maximum": 4, "description": "How many takes (default 3)."},
			"caption": {"type": "string", "description": "Chinese base caption; variants get 方案 A/B/C suffixes."}
		}
	}`)
}

func (t *GenerateVariantsTool) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Prompt  string `json:"prompt"`
		Count   int    `json:"count"`
		Caption string `json:"caption"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return schema.ToolResult{Error: "prompt is required"}, nil
	}
	count := p.Count
	if count < 2 || count > 4 {
		count = 3
	}
	urls, err := t.Variants.GenerateVariants(ctx, p.Prompt, count)
	if err != nil {
		return schema.ToolResult{Error: "generate variants: " + err.Error()}, nil
	}
	if len(urls) == 0 {
		return schema.ToolResult{Error: "no variants returned"}, nil
	}
	base := strings.TrimSpace(p.Caption)
	if base == "" {
		base = "视觉方案"
	}
	labels := []string{"A", "B", "C", "D"}
	for i, u := range urls {
		caption := fmt.Sprintf("%s · 方案 %s", base, labels[i%len(labels)])
		version := t.Session.AddFigure(u, caption)
		if t.Emitter != nil {
			t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("figure", version, len(u)))
		}
	}
	return schema.ToolResult{Output: fmt.Sprintf(
		"Generated %d variants (%s · 方案 %s–%s), all landed as work-package figures.",
		len(urls), base, labels[0], labels[(len(urls)-1)%len(labels)])}, nil
}
