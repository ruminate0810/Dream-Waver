package claw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// GenerateImage is the designer worker's tool. It calls the shared
// image.Searcher (NanoBanana → Unsplash → Noop) and stores the result as a
// work-package figure, emitting claw.artifact.updated(kind="figure") so the
// frontend gallery picks it up. A no-result search degrades gracefully
// (returns a "skip" observation, not an error) — same posture as the rest of
// Claw's optional capabilities.
type GenerateImage struct {
	Images  image.Searcher
	Session *Session
	Emitter event.Emitter
}

func (*GenerateImage) Name() string { return "generate_image" }

func (*GenerateImage) Description() string {
	return "Generate one figure for the report from a text prompt. Pass a clear English " +
		"visual description; the image is added to the work package. Call once or twice for the " +
		"key visuals, then terminate."
}

func (*GenerateImage) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["prompt"],
		"properties": {
			"prompt":  {"type": "string", "description": "English visual description of the image to generate."},
			"caption": {"type": "string", "description": "Short Chinese caption shown under the figure."}
		}
	}`)
}

func (t *GenerateImage) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Prompt  string `json:"prompt"`
		Caption string `json:"caption"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	prompt := strings.TrimSpace(p.Prompt)
	if prompt == "" {
		return schema.ToolResult{Error: "prompt is required"}, nil
	}
	if t.Images == nil {
		return schema.ToolResult{Output: "图片生成未启用,跳过配图。"}, nil
	}

	res, err := t.Images.Search(ctx, prompt)
	if err != nil {
		return schema.ToolResult{Error: "image generation failed: " + err.Error()}, nil
	}
	if res == nil || strings.TrimSpace(res.URL) == "" {
		return schema.ToolResult{Output: "没有找到/生成到合适的配图,跳过。"}, nil
	}

	caption := strings.TrimSpace(p.Caption)
	version := t.Session.AddFigure(res.URL, caption)
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("figure", version, len(res.URL)))
	}
	return schema.ToolResult{Output: fmt.Sprintf(
		"已生成配图 #%d:%s\nURL: %s\n在报告里用 Markdown 图片语法引用它。", version, caption, res.URL)}, nil
}
