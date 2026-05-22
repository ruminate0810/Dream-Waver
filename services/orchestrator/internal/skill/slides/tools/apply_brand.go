package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// ApplyBrand persists deck-wide brand overrides (primary colour, accent
// colour, font family) on SessionState. The renderer injects them as
// `:root { --brand-primary; --brand-accent; --brand-font }` at the top
// of every served slide HTML.
//
// **Sprint B1 scope (state + plumbing only):** the 5 templates today
// hardcode their colours; Sprint B2 upgrades them one-by-one to consume
// `var(--brand-primary, <fallback>)` etc. Until B2 ships, this tool
// successfully persists state and triggers a re-render, but the
// rendered pixels won't yet reflect the brand. The reply text says so.
type ApplyBrand struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*ApplyBrand) Name() string { return "apply_brand" }

func (*ApplyBrand) Description() string {
	return "Apply deck-wide brand overrides (primary colour, accent colour, font family). Use when the user specifies a colour or font (\"主色调换成 #0066FF\", \"用思源黑体\"). Note: visual application requires templates to opt into CSS variables; the override always persists and is exposed via the live preview's <style> block."
}

func (*ApplyBrand) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["primary_color"],
		"properties": {
			"primary_color": {
				"type": "string",
				"pattern": "^#[0-9A-Fa-f]{6}$",
				"description": "Primary brand colour as #RRGGBB."
			},
			"accent_color": {
				"type": "string",
				"pattern": "^#[0-9A-Fa-f]{6}$",
				"description": "Optional accent colour as #RRGGBB; defaults to primary_color."
			},
			"font_family": {
				"type": "string",
				"description": "Optional CSS font stack, e.g. \"Source Han Sans SC, Noto Sans SC, sans-serif\"."
			}
		}
	}`)
}

type applyBrandArgs struct {
	PrimaryColor string `json:"primary_color"`
	AccentColor  string `json:"accent_color"`
	FontFamily   string `json:"font_family"`
}

var hexColorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func (t *ApplyBrand) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a applyBrandArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	primary := strings.TrimSpace(a.PrimaryColor)
	if !hexColorRe.MatchString(primary) {
		return schema.ToolResult{Error: fmt.Sprintf("primary_color must be #RRGGBB (got %q)", a.PrimaryColor)}, nil
	}
	accent := strings.TrimSpace(a.AccentColor)
	if accent != "" && !hexColorRe.MatchString(accent) {
		return schema.ToolResult{Error: fmt.Sprintf("accent_color must be #RRGGBB (got %q)", a.AccentColor)}, nil
	}
	if accent == "" {
		accent = primary
	}

	_, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck to brand — generate the deck first"}, nil
	}

	brand := &schema.Brand{
		PrimaryColor: primary,
		AccentColor:  accent,
		FontFamily:   strings.TrimSpace(a.FontFamily),
	}
	t.State.SetBrand(brand)

	updatedDeck, newCount := t.State.Snapshot()
	dirty := make([]int, newCount)
	for i := 0; i < newCount; i++ {
		dirty[i] = i
	}
	t.State.MarkDirty(dirty...)

	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, dirty)
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	msg := fmt.Sprintf(
		"应用品牌色：accent 改为 %s。所有 %d 张 slide 的强调色（bullets / accent bar / metric 数字 / 边线 / footer dot）都已切换。注意：apply_brand 只改强调色，不改背景 / 不改字号 / 不改版式 — 如果用户想要「整体更花 / 排版更丰富」，应该用 change_theme 切到 playful / retro / editorial 等视觉更丰富的主题，或对单页用 regenerate_slide 重写为 data / quote / two-column 等不同 layout 增加视觉变化。",
		primary, newCount,
	)
	out, _ := json.Marshal(map[string]any{
		"brand":       brand,
		"slide_count": newCount,
		"pptx_path":   pptxPath,
		"message":     msg,
	})
	return schema.ToolResult{Output: string(out)}, nil
}
