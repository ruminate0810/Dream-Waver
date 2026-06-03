// cmd/ab-svg — spike: can the LLM author good, reliable SVG slides?
//
// Generates an outline (same topic/theme as the typed/html A/B), asks
// the LLM to render each slide as a full 1920×1080 SVG, then rasterizes
// each SVG to PNG via chromedp. Outputs /tmp/ab/svg-N.png for eyeball
// comparison against typed-N.png / html-N.png from cmd/ab-html.
//
// This validates the CHEAP+RISKY half of the SVG plan (LLM SVG quality +
// text-wrap behaviour) BEFORE investing in the SVG→PPTX shape converter.
//
//	go run ./cmd/ab-svg -topic "..." -theme editorial -n 5
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

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/joho/godotenv"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/config"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm/providers"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// Editorial theme palette baked in so the spike is self-contained (the
// real impl would inject the active theme's tokens). Matches editorial.css.
const themeVarsCSS = `
:root{
  --bg:#F5F1E8; --fg:#1A1614; --fg-muted:#6B5D52; --accent:#B5371E;
  --font-display:'Fraunces',Georgia,serif;
  --font-body:'Fraunces',Georgia,serif;
  --font-mono:'JetBrains Mono',monospace;
}
html,body{margin:0;padding:0;background:#fff}
svg{display:block}
`

const svgFirstSystem = `You are a world-class presentation designer who authors slides as raw SVG. Each slide is a single <svg> with viewBox="0 0 1920 1080".

You receive a deck outline (title + per-slide headline + bullet points). For EACH slide, author ONE <svg> element that visually presents that slide's content with bespoke, magazine-quality art direction — every slide visually DISTINCT.

Output STRICT JSON (no markdown fences):
{ "slides": [ { "svg": "<svg viewBox='0 0 1920 1080' xmlns='http://www.w3.org/2000/svg'>...</svg>" }, ... ] }

HARD RULES for each svg:
- Root: <svg viewBox="0 0 1920 1080" xmlns="http://www.w3.org/2000/svg" width="1920" height="1080">.
- FIRST child MUST be a full-bleed background: <rect x="0" y="0" width="1920" height="1080" fill="..."/>. Pick the bg explicitly (theme bg is light cream #F5F1E8; you MAY invert to a dark slide but then ALL text must be light).
- COLORS: use the theme palette — bg #F5F1E8, ink #1A1614, muted #6B5D52, accent #B5371E. Every element needs an explicit fill.
- TEXT: SVG <text> does NOT wrap. You MUST manually break long lines into multiple <text> or <tspan x="..." dy="1.2em"> lines. Budget ~28 Latin chars or ~18 CJK chars per line at 48px. NEVER let text exceed x=1820 (right margin) — break earlier.
- FONTS: font-family="Fraunces, Georgia, serif" for display; "JetBrains Mono, monospace" for kickers/labels. Display headline 90-150px, body 30-40px, kicker 18px letter-spacing.
- Use absolute x,y for everything. Left margin x=160. Top safe area y=120. Keep all content within x:[160,1760] y:[100,980].
- Vary composition per slide: full-bleed number, asymmetric split, giant pull-quote, edge kicker. Use <line>/<rect>/<circle>/<path> for rules, accent bars, geometric decoration.
- NO <image>, NO <foreignObject>, NO <script>. Pure SVG vector primitives + text only.
- Keep each svg under ~1500 chars.`

func main() {
	_ = godotenv.Load(".env", "../../.env", "../../../.env")
	topic := flagStr("-topic", "DeepSeek V4 如何把大模型推理成本砍到行业的十分之一")
	theme := flagStr("-theme", "editorial")
	n := flagInt("-n", 5)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	primary := providers.NewDeepSeek(cfg.DeepSeekAPIKey, cfg.DeepSeekBaseURL, cfg.ModelWorker)
	router := llm.NewMultiRouter(primary)
	router.Bind("planner", primary, cfg.ModelPlanner)
	router.Bind("worker", primary, cfg.ModelWorker)
	router.Bind("critic", primary, cfg.ModelCritic)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	outDir := "/tmp/ab"
	_ = os.MkdirAll(outDir, 0o755)

	fmt.Printf("SVG spike  topic=%q theme=%q n=%d\n\n", topic, theme, n)
	fmt.Println("→ planning outline …")
	outline, _, err := stages.Outline(ctx, router, stages.OutlineParams{Topic: topic, SlideCount: n, Style: theme})
	if err != nil {
		slog.Error("outline", "err", err)
		os.Exit(1)
	}
	fmt.Printf("  %q (%d slides)\n\n→ authoring SVG …\n", outline.Title, len(outline.Slides))

	svgs, err := authorSVG(ctx, router, outline, theme)
	if err != nil {
		slog.Error("author svg", "err", err)
		os.Exit(1)
	}
	fmt.Printf("  got %d svg slides\n\n→ rasterizing via chromedp …\n", len(svgs))

	if err := rasterize(ctx, svgs, outDir); err != nil {
		slog.Error("rasterize", "err", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== done === /tmp/ab/svg-1.png … svg-%d.png\n", len(svgs))
}

func authorSVG(ctx context.Context, router llm.Router, outline *stages.OutlineResult, theme string) ([]string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Deck title: %s\nTheme: %s\n\nSlides:\n", outline.Title, theme)
	for _, s := range outline.Slides {
		fmt.Fprintf(&b, "\n%d. [%s] %s\n", s.Index, s.Type, s.Headline)
		if len(s.KeyPoints) > 0 && string(s.KeyPoints) != "null" {
			fmt.Fprintf(&b, "   points: %s\n", string(s.KeyPoints))
		}
	}
	client := router.For("planner")
	resp, err := client.AskTool(ctx, llm.AskToolRequest{
		Model:        router.ModelFor("planner"),
		SystemPrompt: svgFirstSystem,
		Messages:     []schema.Message{schema.NewUser(b.String())},
		MaxTokens:    14000,
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Slides []struct {
			SVG string `json:"svg"`
		} `json:"slides"`
	}
	clean := stripFences(resp.Content)
	if err := json.Unmarshal(clean, &parsed); err != nil {
		return nil, fmt.Errorf("parse svg json: %w; raw=%.300s", err, resp.Content)
	}
	out := make([]string, 0, len(parsed.Slides))
	for _, s := range parsed.Slides {
		out = append(out, s.SVG)
	}
	return out, nil
}

func rasterize(ctx context.Context, svgs []string, outDir string) error {
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("force-device-scale-factor", "1"),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	tmpDir, _ := os.MkdirTemp("", "ab-svg")
	defer os.RemoveAll(tmpDir)

	for i, svg := range svgs {
		html := fmt.Sprintf("<!doctype html><html><head><meta charset='utf-8'><style>%s</style></head><body>%s</body></html>", themeVarsCSS, svg)
		htmlPath := filepath.Join(tmpDir, fmt.Sprintf("s%d.html", i))
		if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
			return err
		}
		var png []byte
		if err := chromedp.Run(browserCtx,
			chromedp.EmulateViewport(1920, 1080),
			chromedp.Navigate("file://"+htmlPath),
			chromedp.WaitReady("body", chromedp.ByQuery),
			chromedp.Sleep(250*time.Millisecond),
			chromedp.ActionFunc(func(c context.Context) error {
				var err error
				png, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).WithClip(&page.Viewport{X: 0, Y: 0, Width: 1920, Height: 1080, Scale: 1}).Do(c)
				return err
			}),
		); err != nil {
			return fmt.Errorf("slide %d: %w", i+1, err)
		}
		path := filepath.Join(outDir, fmt.Sprintf("svg-%d.png", i+1))
		if err := os.WriteFile(path, png, 0o644); err != nil {
			return err
		}
		fmt.Printf("  wrote %s (%d KB)\n", path, len(png)/1024)
	}
	return nil
}

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
