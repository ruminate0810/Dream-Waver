package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/svglayout"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// PlanSlideLayout is the Sprint SV-5 generation stage. Instead of asking
// the LLM to author raw SVG with hand-placed coordinates (which collide,
// since SVG has no auto-layout), it asks for a coordinate-FREE block
// tree per slide — semantic structure + content only. The deterministic
// svglayout engine then computes every position/size/colour, so overlaps
// are impossible by construction. No measure-repair needed.
//
// Returns a ContentResult whose slides carry Layout=svg + Data.SVG (the
// engine-rendered markup), exactly like AuthorSVG, so the rest of the
// pipeline (Assemble → render → native-shape PPTX) is unchanged.
func PlanSlideLayout(ctx context.Context, router llm.Router, outline *OutlineResult, theme string) (*ContentResult, llm.Usage, error) {
	if outline == nil || len(outline.Slides) == 0 {
		return nil, llm.Usage{}, fmt.Errorf("PlanSlideLayout: outline is empty")
	}
	th := svgTheme(theme)

	var user strings.Builder
	fmt.Fprintf(&user, "Deck title: %s\nTheme: %s\n\nSlides:\n", outline.Title, theme)
	for _, s := range outline.Slides {
		fmt.Fprintf(&user, "\n%d. [%s] %s\n", s.Index, s.Type, s.Headline)
		if len(s.KeyPoints) > 0 && string(s.KeyPoints) != "null" {
			fmt.Fprintf(&user, "   points: %s\n", string(s.KeyPoints))
		}
		if strings.TrimSpace(s.SpeakerNotes) != "" {
			fmt.Fprintf(&user, "   note: %s\n", s.SpeakerNotes)
		}
	}

	client := router.For("planner")
	var slides []ContentSlide
	resp, err := askWithRetry(ctx, client, "svg-plan", llm.AskToolRequest{
		Model:        router.ModelFor("planner"),
		SystemPrompt: prompts.SVGPlan,
		Messages:     []schema.Message{schema.NewUser(user.String())},
		// Block trees are compact JSON (no verbose SVG markup), so this
		// is smaller than raw-SVG authoring despite richer structure.
		MaxTokens:         10000,
		EnablePromptCache: true,
	}, func(content string) error {
		slides = nil
		var parsed struct {
			Slides []json.RawMessage `json:"slides"`
		}
		if err := json.Unmarshal(stripFences(content), &parsed); err != nil {
			return fmt.Errorf("parse svg-plan json: %w; raw=%q", err, truncate(content, 200))
		}
		if len(parsed.Slides) == 0 {
			return fmt.Errorf("svg-plan produced 0 slides")
		}
		total := len(parsed.Slides)
		for i, raw := range parsed.Slides {
			var root svglayout.Block
			if err := json.Unmarshal(raw, &root); err != nil {
				return fmt.Errorf("slide %d: parse block tree: %w", i+1, err)
			}
			// Deck chrome: code-injected folio + footer, identical on
			// every slide for coherence. The first slide is the cover —
			// it stays clean (no folio/footer).
			chrome := svglayout.Chrome{
				Folio:  i + 1,
				Total:  total,
				Footer: outline.Title,
				Cover:  i == 0,
			}
			svg := svglayout.Render(&root, th, chrome)
			slides = append(slides, ContentSlide{
				Index:    i + 1,
				Template: theme,
				Layout:   schema.LayoutSVG,
				Data:     schema.SlideData{SVG: svg},
			})
		}
		return nil
	})
	if err != nil {
		return nil, llm.Usage{}, err
	}
	return &ContentResult{Slides: slides}, resp.Usage, nil
}

// svgTheme maps the shared theme tokens into the svglayout engine's
// local Theme (kept separate so svglayout stays a leaf package).
func svgTheme(theme string) svglayout.Theme {
	t := themetokens.Get(theme)
	return svglayout.Theme{
		BG:       t.BG,
		FG:       t.FG,
		FGMuted:  t.FGMuted,
		Accent:   t.Accent,
		FontDisp: t.FontDisp,
		FontBody: t.FontBody,
		FontMono: t.FontMono,
	}
}
