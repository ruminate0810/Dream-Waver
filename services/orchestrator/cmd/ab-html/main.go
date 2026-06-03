// cmd/ab-html — one-off A/B harness comparing the typed-layout pipeline
// against an HTML-first freeform pipeline on the SAME outline.
//
// Both sides share one outline (so the INFORMATION is identical); only
// the visual treatment differs:
//   A-side: stages.Content → typed 25-layout templates → render
//   B-side: ask LLM to author full bespoke HTML per slide → render
//
// Outputs PNGs to /tmp/ab/{typed,html}-N.png for eyeball comparison.
//
//	go run ./cmd/ab-html -topic "..." -theme editorial -n 5
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/config"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm/providers"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

const htmlFirstSystem = `You are a world-class presentation designer who writes raw HTML/CSS for individual slides. Each slide is a 1920×1080 canvas.

You will receive a deck outline (title + per-slide headline + bullet points). For EACH slide, author a single self-contained block of HTML that visually presents that slide's content with bespoke, magazine-quality art direction — NOT a generic template.

Output STRICT JSON (no markdown fences):
{
  "slides": [
    { "html": "<div style='...'>...</div>" },
    ...
  ]
}

HARD RULES for each slide's html:
- The root is ONE element that fills the 1920×1080 canvas. Use position:absolute / fl/grid freely.
- COLORS + FONTS must come from theme CSS variables so the deck stays cohesive:
  var(--bg) var(--fg) var(--fg-muted) var(--accent) var(--font-display) var(--font-body) var(--font-mono)
  Do NOT hardcode hex colors or font families — always reference the vars.
- You may use inline <style> blocks and any layout CSS (flex, grid, absolute, transforms, gradients, pseudo-via-style).
- FORBIDDEN: <script>, <iframe>, <form>, <img> with external URLs (no network). Use CSS shapes / gradients / type for visuals.
- Each slide must be VISUALLY DISTINCT from the others — vary composition: full-bleed numbers, asymmetric splits, big pull quotes, edge-anchored kickers, overlapping type. This is the whole point — escape the "every slide looks the same" template trap.
- Fill the canvas. Big confident type (display headings 80-160px). Generous but intentional whitespace. No tiny centered text floating in a void.
- Keep total under ~1200 chars of HTML per slide.

The deck's theme is provided; honor its mood.`

func main() {
	_ = godotenv.Load(".env", "../../.env", "../../../.env")

	topic := flagStr("-topic", "DeepSeek V4 如何把推理成本砍到行业 1/10")
	theme := flagStr("-theme", "editorial")
	n := flagInt("-n", 5)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	setupLogger("warn") // quiet — we only want our own prints

	primary := providers.NewDeepSeek(cfg.DeepSeekAPIKey, cfg.DeepSeekBaseURL, cfg.ModelWorker)
	router := llm.NewMultiRouter(primary)
	router.Bind("planner", primary, cfg.ModelPlanner)
	router.Bind("worker", primary, cfg.ModelWorker)
	router.Bind("critic", primary, cfg.ModelCritic)

	renderer := &tool.SlideRender{
		TemplateDir: cfg.TemplateDir,
		OutDir:      cfg.OutDir,
		Emitter:     event.NoopEmitter{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	outDir := "/tmp/ab"
	_ = os.MkdirAll(outDir, 0o755)

	fmt.Printf("A/B  topic=%q theme=%q n=%d\n\n", topic, theme, n)

	// ── Shared outline ────────────────────────────────────────────────
	fmt.Println("→ planning shared outline …")
	outline, _, err := stages.Outline(ctx, router, stages.OutlineParams{
		Topic:      topic,
		SlideCount: n,
		Style:      theme,
	})
	if err != nil {
		slog.Error("outline", "err", err)
		os.Exit(1)
	}
	outline.Theme = schema.Theme(theme)
	fmt.Printf("  outline: %q (%d slides)\n\n", outline.Title, len(outline.Slides))

	// ── A-side: typed layouts ─────────────────────────────────────────
	fmt.Println("→ [A] typed-layout: writing content + rendering …")
	content, _, err := stages.Content(ctx, router, outline)
	if err != nil {
		slog.Error("content", "err", err)
		os.Exit(1)
	}
	deckA := stages.Assemble(outline, content)
	deckA.Theme = schema.Theme(theme)
	if err := renderSide(ctx, renderer, deckA, outDir, "typed"); err != nil {
		slog.Error("render A", "err", err)
		os.Exit(1)
	}

	// ── B-side: HTML-first ────────────────────────────────────────────
	fmt.Println("\n→ [B] html-first: authoring bespoke HTML + rendering …")
	deckB, err := buildHTMLFirstDeck(ctx, router, outline, theme)
	if err != nil {
		slog.Error("html-first build", "err", err)
		os.Exit(1)
	}
	if err := renderSide(ctx, renderer, deckB, outDir, "html"); err != nil {
		slog.Error("render B", "err", err)
		os.Exit(1)
	}

	fmt.Printf("\n=== done ===\nPNGs in %s/\n  typed-1.png … typed-%d.png\n  html-1.png  … html-%d.png\n", outDir, len(deckA.Slides), len(deckB.Slides))
}

// buildHTMLFirstDeck asks the LLM to render the shared outline as full
// bespoke HTML per slide, then wraps each as a layout=html slide.
func buildHTMLFirstDeck(ctx context.Context, router llm.Router, outline *stages.OutlineResult, theme string) (schema.Deck, error) {
	// Compact the outline into the user message.
	var b strings.Builder
	fmt.Fprintf(&b, "Deck title: %s\nTheme: %s\n\nSlides:\n", outline.Title, theme)
	for _, s := range outline.Slides {
		fmt.Fprintf(&b, "\n%d. [%s] %s\n", s.Index, s.Type, s.Headline)
		// key_points is RawMessage; print as-is.
		if len(s.KeyPoints) > 0 && string(s.KeyPoints) != "null" {
			fmt.Fprintf(&b, "   points: %s\n", string(s.KeyPoints))
		}
	}

	client := router.For("planner")
	resp, err := client.AskTool(ctx, llm.AskToolRequest{
		Model:        router.ModelFor("planner"),
		SystemPrompt: htmlFirstSystem,
		Messages:     []schema.Message{schema.NewUser(b.String())},
		MaxTokens:    12000,
	})
	if err != nil {
		return schema.Deck{}, err
	}
	var parsed struct {
		Slides []struct {
			HTML string `json:"html"`
		} `json:"slides"`
	}
	clean := stripFences(resp.Content)
	if err := json.Unmarshal(clean, &parsed); err != nil {
		return schema.Deck{}, fmt.Errorf("parse html-first json: %w; raw=%.300s", err, resp.Content)
	}

	deck := schema.Deck{Title: outline.Title, Theme: schema.Theme(theme)}
	for _, s := range parsed.Slides {
		deck.Slides = append(deck.Slides, schema.Slide{
			Template: theme,
			Layout:   schema.LayoutHTML,
			Data:     schema.SlideData{HTML: s.HTML},
		})
	}
	if len(deck.Slides) == 0 {
		return schema.Deck{}, fmt.Errorf("html-first produced 0 slides")
	}
	return deck, nil
}

func renderSide(ctx context.Context, r *tool.SlideRender, deck schema.Deck, outDir, prefix string) error {
	assets, _, err := r.RenderDirty(ctx, deck, nil, nil)
	if err != nil {
		return err
	}
	for i, a := range assets {
		path := filepath.Join(outDir, fmt.Sprintf("%s-%d.png", prefix, i+1))
		if err := os.WriteFile(path, a.PreviewPNG, 0o644); err != nil {
			return err
		}
		fmt.Printf("  wrote %s (%d KB)\n", path, len(a.PreviewPNG)/1024)
	}
	return nil
}

// ── tiny flag + helpers (no flag pkg to keep arg parsing forgiving) ──

func flagStr(name, def string) string {
	for i, a := range os.Args {
		if a == name && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return def
}

func flagInt(name string, def int) int {
	s := flagStr(name, "")
	if s == "" {
		return def
	}
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil || v <= 0 {
		return def
	}
	return v
}

func stripFences(s string) []byte {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return []byte(strings.TrimSpace(s))
}

func setupLogger(level string) {
	lvl := slog.LevelWarn
	if strings.ToLower(level) == "info" {
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}
