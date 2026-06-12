package claw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// ImageEditor is the designer's post-production capability — a tiny
// interface so the claw package stays decoupled from the design bridge
// (same posture as VideoGenerator for the videographer). main.go injects an
// adapter around design.Bridge's edit endpoints. nil greys the tool out.
//
// op is one of: remove_bg | enhance | colorize | outpaint | img2img.
// prompt is only used by img2img; expand is [left,right,top,bottom] pixels
// and only used by outpaint.
type ImageEditor interface {
	EditImage(ctx context.Context, op, imageURL, prompt string, expand [4]int) (url string, err error)
}

// editOps maps the tool's op enum to a Chinese label for captions/output.
var editOps = map[string]string{
	"remove_bg": "抠图",
	"enhance":   "高清化",
	"colorize":  "上色",
	"outpaint":  "扩图",
	"img2img":   "图生图",
}

// EditImage is the designer worker's post-production tool. It applies one
// image operation to an already-generated work-package figure and stores the
// result as a NEW figure (the original is kept), emitting
// claw.artifact.updated(kind="figure") so the frontend gallery picks it up.
// Missing capability / no figures degrades gracefully (a "skip" observation,
// not an error) — same posture as the rest of Claw's optional capabilities.
type EditImage struct {
	Editor  ImageEditor
	Session *Session
	Emitter event.Emitter
}

func (*EditImage) Name() string { return "edit_image" }

func (*EditImage) Description() string {
	return "Post-process an already-generated work-package figure. op: remove_bg (cut out the " +
		"subject, transparent background), enhance (sharpen / upscale), colorize (add color to a " +
		"black-and-white image), outpaint (extend the canvas; set left/right/top/bottom pixels), " +
		"img2img (re-imagine the figure from a text prompt — prompt required). The result is added " +
		"as a new figure; the original is kept. Default source is the most recent figure."
}

func (*EditImage) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["op"],
		"properties": {
			"op":           {"type": "string", "enum": ["remove_bg", "enhance", "colorize", "outpaint", "img2img"], "description": "Which image operation to apply."},
			"figure_index": {"type": "integer", "description": "1-based index of the source figure (default: the most recent one)."},
			"prompt":       {"type": "string", "description": "English description of the transformation (required for img2img)."},
			"left":         {"type": "integer", "description": "outpaint: pixels to extend on the left."},
			"right":        {"type": "integer", "description": "outpaint: pixels to extend on the right."},
			"top":          {"type": "integer", "description": "outpaint: pixels to extend on the top."},
			"bottom":       {"type": "integer", "description": "outpaint: pixels to extend on the bottom."},
			"caption":      {"type": "string", "description": "Short Chinese caption for the new figure."}
		}
	}`)
}

func (t *EditImage) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Op          string `json:"op"`
		FigureIndex int    `json:"figure_index"`
		Prompt      string `json:"prompt"`
		Left        int    `json:"left"`
		Right       int    `json:"right"`
		Top         int    `json:"top"`
		Bottom      int    `json:"bottom"`
		Caption     string `json:"caption"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	op := strings.TrimSpace(strings.ToLower(p.Op))
	zh, ok := editOps[op]
	if !ok {
		return schema.ToolResult{Error: "unknown op: " + p.Op + " (use remove_bg/enhance/colorize/outpaint/img2img)"}, nil
	}
	if t.Editor == nil {
		return schema.ToolResult{Output: "修图能力未启用,跳过。"}, nil
	}

	figs := t.Session.Figures()
	if len(figs) == 0 {
		return schema.ToolResult{Output: "作品包里还没有配图可修——先用 generate_image 生成一张。"}, nil
	}
	idx := p.FigureIndex
	if idx <= 0 || idx > len(figs) {
		idx = len(figs) // default: the most recent figure
	}
	src := figs[idx-1]

	prompt := strings.TrimSpace(p.Prompt)
	if op == "img2img" && prompt == "" {
		return schema.ToolResult{Error: "img2img requires a prompt"}, nil
	}
	expand := [4]int{p.Left, p.Right, p.Top, p.Bottom}
	if op == "outpaint" && expand == [4]int{} {
		// sidecar rejects all-zero — default to a gentle widen
		expand = [4]int{256, 256, 0, 0}
	}

	url, err := t.Editor.EditImage(ctx, op, src.URL, prompt, expand)
	if err != nil {
		return schema.ToolResult{Error: zh + " failed: " + err.Error()}, nil
	}
	if strings.TrimSpace(url) == "" {
		return schema.ToolResult{Output: zh + " 没有返回结果,跳过。"}, nil
	}

	caption := strings.TrimSpace(p.Caption)
	if caption == "" {
		caption = zh
		if src.Caption != "" {
			caption = src.Caption + " · " + zh
		}
	}
	version := t.Session.AddFigure(url, caption)
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("figure", version, len(url)))
	}
	return schema.ToolResult{Output: fmt.Sprintf(
		"已完成%s,生成新配图 #%d(源图 #%d):%s\nURL: %s\n需要的话可在报告里引用新图。",
		zh, version, idx, caption, url)}, nil
}
