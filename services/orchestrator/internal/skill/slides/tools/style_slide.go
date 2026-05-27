package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// StyleSlide applies per-slide typography overrides — font-size scales
// and density preset — without changing content or theme. Solves the
// long-standing "字号太小 / 这页文字大一点" complaint that previously
// could only be answered with "目前不支持 — 换主题试试".
//
// No LLM call. The renderer emits inline CSS variables (--slide-title-
// scale, --slide-body-scale, --slide-bullet-scale, --slide-density-*)
// on the slide root <section> via the slideStyle template helper. The
// base.css typography selectors multiply by these vars with calc(),
// defaulting to 1 (i.e. theme value) when unset.
//
// Triggers: "把第 3 页标题字号调大 / make slide 3's title bigger /
//  this page is too cramped (use density=spacious)".
type StyleSlide struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*StyleSlide) Name() string { return "style_slide" }

func (*StyleSlide) Description() string {
	return "Apply per-slide typography overrides (font scales + density preset) without rewriting content or changing theme. Use for \"字号太小\" / \"标题大一点\" / \"这页排太挤\" — directly solves the user-pain that previously required a full theme change. NO LLM call; just adds inline CSS vars on the slide root. Either or both: slide_index = a specific page; clear=true wipes existing override. Scales are multipliers (1.0 = theme default; 1.2 = 20% bigger; 0.85 = slightly tighter). Density preset adjusts padding + line-height in one shot. Reset by calling with clear=true."
}

func (*StyleSlide) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index"],
		"properties": {
			"slide_index":   {"type": "integer", "minimum": 1, "description": "1-based slide index to restyle."},
			"title_scale":   {"type": "number", "minimum": 0.7, "maximum": 1.5, "description": "Title font-size multiplier. 1.0 = theme default; 1.2 = 20% bigger; 0.85 = ~15% smaller."},
			"body_scale":    {"type": "number", "minimum": 0.7, "maximum": 1.5, "description": "Body / paragraph font-size multiplier."},
			"bullet_scale": {"type": "number", "minimum": 0.7, "maximum": 1.5, "description": "Bullet list font-size multiplier."},
			"density":       {"type": "string", "enum": ["compact", "normal", "spacious"], "description": "Spacing preset. 'compact' = tighter padding + line-height (~80%); 'normal' = theme default; 'spacious' = looser (~125%). Use spacious for sparse pages where text feels lost; compact for over-dense pages."},
			"clear":         {"type": "boolean", "description": "When true, removes any existing style override on this slide (returns to theme default). All other fields ignored."}
		}
	}`)
}

type styleSlideArgs struct {
	SlideIndex  int     `json:"slide_index"`
	TitleScale  float64 `json:"title_scale,omitempty"`
	BodyScale   float64 `json:"body_scale,omitempty"`
	BulletScale float64 `json:"bullet_scale,omitempty"`
	Density     string  `json:"density,omitempty"`
	Clear       bool    `json:"clear,omitempty"`
}

func (t *StyleSlide) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a styleSlideArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	_, count := t.State.Snapshot()
	if count == 0 {
		return schema.ToolResult{Error: "no deck to style"}, nil
	}
	if a.SlideIndex < 1 || a.SlideIndex > count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index out of range (have %d slides)", count)}, nil
	}
	idx0 := a.SlideIndex - 1

	var newStyle *schema.SlideStyle
	if !a.Clear {
		// Validate inputs. The schema 0.7..1.5 clamp is enforced both
		// here (with a clear error) and downstream in the renderer
		// (silent clamp) for defence-in-depth.
		if a.TitleScale != 0 && (a.TitleScale < 0.7 || a.TitleScale > 1.5) {
			return schema.ToolResult{Error: fmt.Sprintf("title_scale %.2f out of range 0.7..1.5", a.TitleScale)}, nil
		}
		if a.BodyScale != 0 && (a.BodyScale < 0.7 || a.BodyScale > 1.5) {
			return schema.ToolResult{Error: fmt.Sprintf("body_scale %.2f out of range 0.7..1.5", a.BodyScale)}, nil
		}
		if a.BulletScale != 0 && (a.BulletScale < 0.7 || a.BulletScale > 1.5) {
			return schema.ToolResult{Error: fmt.Sprintf("bullet_scale %.2f out of range 0.7..1.5", a.BulletScale)}, nil
		}
		density := strings.ToLower(strings.TrimSpace(a.Density))
		if density == "normal" {
			density = "" // treat as "no override"
		}
		if density != "" && density != "compact" && density != "spacious" {
			return schema.ToolResult{Error: fmt.Sprintf(`density %q is invalid; pick one of "compact", "normal", "spacious"`, a.Density)}, nil
		}
		// If nothing was actually changed, fail clearly so the agent
		// doesn't waste a critic round on a no-op.
		if a.TitleScale == 0 && a.BodyScale == 0 && a.BulletScale == 0 && density == "" {
			return schema.ToolResult{Error: "no override fields provided; pass at least one of title_scale / body_scale / bullet_scale / density, or set clear=true to remove the existing override"}, nil
		}
		newStyle = &schema.SlideStyle{
			TitleScale:  a.TitleScale,
			BodyScale:   a.BodyScale,
			BulletScale: a.BulletScale,
			Density:     density,
		}
	}

	if err := t.State.SetSlideStyle(idx0, newStyle); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	t.State.MarkDirty(idx0)

	updatedDeck, _ := t.State.Snapshot()
	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, []int{idx0})
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	msg := ""
	if a.Clear {
		msg = fmt.Sprintf("Slide %d: style override removed; back to theme defaults.", a.SlideIndex)
	} else {
		parts := []string{}
		if newStyle.TitleScale != 0 {
			parts = append(parts, fmt.Sprintf("title %.0f%%", newStyle.TitleScale*100))
		}
		if newStyle.BodyScale != 0 {
			parts = append(parts, fmt.Sprintf("body %.0f%%", newStyle.BodyScale*100))
		}
		if newStyle.BulletScale != 0 {
			parts = append(parts, fmt.Sprintf("bullets %.0f%%", newStyle.BulletScale*100))
		}
		if newStyle.Density != "" {
			parts = append(parts, "density "+newStyle.Density)
		}
		msg = fmt.Sprintf("Slide %d: applied %s.", a.SlideIndex, strings.Join(parts, ", "))
	}

	out, _ := json.Marshal(map[string]any{
		"slide_index": a.SlideIndex,
		"cleared":     a.Clear,
		"style":       newStyle,
		"pptx_path":   pptxPath,
		"message":     msg,
	})
	return schema.ToolResult{Output: string(out)}, nil
}
