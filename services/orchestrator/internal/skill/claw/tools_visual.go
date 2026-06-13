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

// tools_visual.go — 多样产出. The designer's image source (NanoBanana via the
// design bridge) can make far more than report figures: posters, illustrated
// storybooks, social cards. These tools are prompt-engineering wrappers over
// the same image.Searcher the designer already holds, and they store results
// as work-package figures (reusing the figure gallery / persistence) — so a
// run's output is no longer "always a report".

// ── 海报 ───────────────────────────────────────────────────────────────────

// GeneratePoster makes one text-forward promotional poster. It composes a
// NanoBanana prompt from the title/subtitle/style/orientation so the model
// renders a real poster (bold title, balanced composition) rather than a
// generic illustration. Degrades gracefully when the image source is unwired.
type GeneratePoster struct {
	Images  image.Searcher
	Session *Session
	Emitter event.Emitter
}

func (*GeneratePoster) Name() string { return "generate_poster" }

func (*GeneratePoster) Description() string {
	return "Design one promotional poster (a polished, text-forward image — NOT a report figure). " +
		"Give a big title, optional subtitle, a visual theme, a style, and orientation. Use this when " +
		"the user wants a 海报 / poster / 活动宣传图 / 社媒主图. Call once, then terminate."
}

func (*GeneratePoster) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["title", "theme"],
		"properties": {
			"title":       {"type": "string", "description": "The big headline text on the poster (the language you want rendered, e.g. Chinese)."},
			"subtitle":    {"type": "string", "description": "Optional smaller supporting line."},
			"theme":       {"type": "string", "description": "English description of the subject / scene / mood of the poster."},
			"style":       {"type": "string", "description": "Visual style, e.g. flat / retro / cyberpunk / minimalist / hand-drawn / 3D. Default: modern flat."},
			"orientation": {"type": "string", "enum": ["portrait", "landscape", "square"], "description": "Poster shape (default portrait)."},
			"caption":     {"type": "string", "description": "Short Chinese caption shown under the artifact."}
		}
	}`)
}

func (t *GeneratePoster) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Title       string `json:"title"`
		Subtitle    string `json:"subtitle"`
		Theme       string `json:"theme"`
		Style       string `json:"style"`
		Orientation string `json:"orientation"`
		Caption     string `json:"caption"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	title := strings.TrimSpace(p.Title)
	theme := strings.TrimSpace(p.Theme)
	if title == "" || theme == "" {
		return schema.ToolResult{Error: "title and theme are required"}, nil
	}
	if t.Images == nil {
		return schema.ToolResult{Output: "图片生成未启用,跳过海报。"}, nil
	}

	style := strings.TrimSpace(p.Style)
	if style == "" {
		style = "modern flat graphic design"
	}
	shape := map[string]string{"portrait": "portrait 3:4", "landscape": "landscape 16:9", "square": "square 1:1"}[p.Orientation]
	if shape == "" {
		shape = "portrait 3:4"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "A %s promotional poster, %s orientation. ", style, shape)
	fmt.Fprintf(&b, "Large bold headline text reading \"%s\" as the focal point. ", title)
	if s := strings.TrimSpace(p.Subtitle); s != "" {
		fmt.Fprintf(&b, "Smaller subtitle text \"%s\". ", s)
	}
	fmt.Fprintf(&b, "Theme: %s. ", theme)
	b.WriteString("Strong visual hierarchy, balanced composition, generous negative space, vivid but harmonious palette, crisp typography, print-ready poster art.")

	res, err := t.Images.Search(ctx, b.String())
	if err != nil {
		return schema.ToolResult{Error: "poster generation failed: " + err.Error()}, nil
	}
	if res == nil || strings.TrimSpace(res.URL) == "" {
		return schema.ToolResult{Output: "没有生成到海报,跳过。"}, nil
	}

	caption := strings.TrimSpace(p.Caption)
	if caption == "" {
		caption = "海报:" + title
	}
	version := t.Session.AddFigure(res.URL, caption)
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("figure", version, len(res.URL)))
	}
	return schema.ToolResult{Output: fmt.Sprintf("已设计海报 #%d:%s\nURL: %s", version, caption, res.URL)}, nil
}

// ── 绘本 / 多格插画 ─────────────────────────────────────────────────────────

// GenerateStorybook turns a short story into N consistent illustrated panels.
// Each panel is one image.Search call with a shared style prefix so the book
// reads as one work; every panel lands as a work-package figure.
type GenerateStorybook struct {
	Images  image.Searcher
	Session *Session
	Emitter event.Emitter
}

func (*GenerateStorybook) Name() string { return "generate_storybook" }

func (*GenerateStorybook) Description() string {
	return "Illustrate a short story as a sequence of 2–6 consistent panels (a 绘本 / picture book / " +
		"漫画分镜). Pass a scene list (one short English visual description per panel) and a shared style. " +
		"Each panel is added as a figure. Use this when the user wants an illustrated story, not a report. " +
		"Call once, then terminate."
}

func (*GenerateStorybook) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["scenes"],
		"properties": {
			"scenes":   {"type": "array", "items": {"type": "string"}, "description": "2–6 panels, each a short English visual description of that scene."},
			"style":    {"type": "string", "description": "Shared illustration style applied to every panel, e.g. warm watercolor children's book / flat vector / anime. Default: soft storybook illustration."},
			"captions": {"type": "array", "items": {"type": "string"}, "description": "Optional Chinese captions, one per panel."}
		}
	}`)
}

func (t *GenerateStorybook) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Scenes   []string `json:"scenes"`
		Style    string   `json:"style"`
		Captions []string `json:"captions"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	scenes := make([]string, 0, len(p.Scenes))
	for _, s := range p.Scenes {
		if strings.TrimSpace(s) != "" {
			scenes = append(scenes, strings.TrimSpace(s))
		}
	}
	if len(scenes) == 0 {
		return schema.ToolResult{Error: "scenes is required (2–6 panels)"}, nil
	}
	if len(scenes) > 6 {
		scenes = scenes[:6] // cap — each panel is a billed image call
	}
	if t.Images == nil {
		return schema.ToolResult{Output: "图片生成未启用,跳过绘本。"}, nil
	}

	style := strings.TrimSpace(p.Style)
	if style == "" {
		style = "soft warm storybook illustration, consistent characters and palette across panels"
	}

	var made []int
	for i, sc := range scenes {
		prompt := fmt.Sprintf("%s. Panel %d of %d of a picture book. Scene: %s. Consistent character design and color palette with the other panels, gentle lighting, clean composition.",
			style, i+1, len(scenes), sc)
		res, err := t.Images.Search(ctx, prompt)
		if err != nil || res == nil || strings.TrimSpace(res.URL) == "" {
			continue // skip a failed panel, keep the rest
		}
		caption := fmt.Sprintf("绘本 第%d页", i+1)
		if i < len(p.Captions) && strings.TrimSpace(p.Captions[i]) != "" {
			caption = fmt.Sprintf("第%d页:%s", i+1, strings.TrimSpace(p.Captions[i]))
		}
		version := t.Session.AddFigure(res.URL, caption)
		made = append(made, version)
		if t.Emitter != nil {
			t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("figure", version, len(res.URL)))
		}
	}
	if len(made) == 0 {
		return schema.ToolResult{Output: "绘本没有生成出任何一页,跳过。"}, nil
	}
	return schema.ToolResult{Output: fmt.Sprintf("已绘制绘本 %d 页(配图 #%v)。", len(made), made)}, nil
}
