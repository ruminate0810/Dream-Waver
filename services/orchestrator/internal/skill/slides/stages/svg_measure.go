package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// Sprint SV-2 — measure-repair loop. SVG has no auto-wrap, so the LLM
// occasionally lets a <text> run past the safe margin or collide with a
// neighbour (the spike's "−90% badge over $0.001" bug). Unlike a static
// format, SVG renders in a browser — so we can MEASURE every text box's
// real bounding rect and feed precise violations back to the LLM to fix.
// This file owns the measurement; svg.go drives the repair loop.

// Safe layout bounds (px, in the 1920×1080 canvas). A text whose
// rendered rect crosses these is flagged. Slightly looser than the
// prompt's stated margins so we only flag genuine overruns, not 1px
// kisses.
const (
	svgSafeLeft   = 40.0
	svgSafeTop    = 24.0
	svgSafeRight  = 1880.0
	svgSafeBottom = 1056.0
	// overlapMinArea — two text rects must overlap by at least this many
	// px² to count as a collision (filters out incidental 1px touches +
	// intentional tight kerning).
	overlapMinArea = 1600.0
)

// Violation is one layout problem on a slide, phrased for the LLM.
type Violation struct {
	Kind   string // "overflow-right" | "overflow-bottom" | "overflow-left" | "overflow-top" | "overlap"
	Detail string // human/LLM-readable, e.g. "text \"−90%\" (x 905→1075) overlaps \"$0.001\""
}

// textRect mirrors the JS measurement payload for one <text>/<tspan>.
type textRect struct {
	Text   string  `json:"text"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
}

// cardRect mirrors a measured panel/card <rect> (no text payload). Used to
// flag text that renders OUTSIDE its own card even when it's nowhere near the
// global safe margin.
type cardRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
}

// measurePayload is the JS measurement result: every text box + every card rect.
type measurePayload struct {
	Texts []textRect `json:"texts"`
	Cards []cardRect `json:"cards"`
}

// measureJS walks every rendered <text> and returns its viewport bbox.
// We measure <text> (not <tspan>) as the atomic unit — a multi-line
// <text> with <tspan> children reports the union bbox, which is what we
// want for overflow. getBoundingClientRect is in CSS px at our 1920-wide
// viewport, i.e. canvas coordinates.
const measureJS = `(() => {
  const texts = [];
  document.querySelectorAll('text').forEach(t => {
    const r = t.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) return;
    texts.push({
      text: (t.textContent || '').trim().slice(0, 40),
      x: r.left, y: r.top, right: r.right, bottom: r.bottom, w: r.width, h: r.height
    });
  });
  // Card / panel rects, so we can flag text that spills OUTSIDE its own card
  // even when it stays far inside the global safe margin. Skip the full-bleed
  // background and thin decorative bars / chips / chart ticks.
  const cards = [];
  document.querySelectorAll('rect').forEach(c => {
    const r = c.getBoundingClientRect();
    if (r.width >= 1860 && r.height >= 1040) return; // full-canvas background
    if (r.width < 140 || r.height < 56) return;      // dividers / pills / ticks
    cards.push({ x: r.left, y: r.top, right: r.right, bottom: r.bottom, w: r.width, h: r.height });
  });
  return JSON.stringify({ texts, cards });
})()`

// MeasureSVGs rasterizes each svg in a headless browser and returns the
// per-slide layout violations. svgs[i] corresponds to result[i]. A nil
// inner slice means that slide is clean.
//
// One browser context is reused across all slides in the call (cheaper
// than per-slide). The theme is used only to wrap each svg in the same
// full-bleed shell the real renderer uses, so font metrics (and thus
// text widths — critical for CJK) match what ships.
func MeasureSVGs(ctx context.Context, svgs []string, theme string) ([][]Violation, error) {
	tok := themetokens.Get(theme)
	style := tok.StyleBlock("", "", "")

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

	tmpDir, err := os.MkdirTemp("", "svg-measure")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	out := make([][]Violation, len(svgs))
	for i, svg := range svgs {
		doc := "<!doctype html><html><head><meta charset=\"utf-8\">" + style + "</head><body>" + svg + "</body></html>"
		htmlPath := filepath.Join(tmpDir, fmt.Sprintf("m%d.html", i))
		if err := os.WriteFile(htmlPath, []byte(doc), 0o644); err != nil {
			return nil, err
		}
		var raw string
		runCtx, cancel := context.WithTimeout(browserCtx, 20*time.Second)
		err := chromedp.Run(runCtx,
			chromedp.EmulateViewport(1920, 1080),
			chromedp.Navigate("file://"+htmlPath),
			chromedp.WaitReady("body", chromedp.ByQuery),
			chromedp.Sleep(250*time.Millisecond), // font + layout settle
			chromedp.Evaluate(measureJS, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
				return p.WithReturnByValue(true)
			}),
		)
		cancel()
		if err != nil {
			// Measurement failure on one slide shouldn't abort the deck —
			// treat as "no violations found" (we just skip repair for it).
			out[i] = nil
			continue
		}
		var payload measurePayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			out[i] = nil
			continue
		}
		out[i] = detectViolations(payload.Texts, payload.Cards)
	}
	return out, nil
}

// detectViolations turns measured rects into flagged problems: out-of-
// safe-bounds overruns + pairwise text overlaps + text that escapes its card.
func detectViolations(rects []textRect, cards []cardRect) []Violation {
	var vs []Violation
	for _, r := range rects {
		label := truncate(r.Text, 24)
		switch {
		case r.Right > svgSafeRight:
			vs = append(vs, Violation{Kind: "overflow-right",
				Detail: fmt.Sprintf("text %q extends to x=%.0f, past the right safe margin %.0f — shorten it or break it onto another line", label, r.Right, svgSafeRight)})
		case r.X < svgSafeLeft:
			vs = append(vs, Violation{Kind: "overflow-left",
				Detail: fmt.Sprintf("text %q starts at x=%.0f, left of the safe margin %.0f", label, r.X, svgSafeLeft)})
		}
		switch {
		case r.Bottom > svgSafeBottom:
			vs = append(vs, Violation{Kind: "overflow-bottom",
				Detail: fmt.Sprintf("text %q reaches y=%.0f, below the bottom safe margin %.0f — move it up or reduce lines", label, r.Bottom, svgSafeBottom)})
		case r.Y < svgSafeTop:
			vs = append(vs, Violation{Kind: "overflow-top",
				Detail: fmt.Sprintf("text %q starts at y=%.0f, above the top safe margin %.0f", label, r.Y, svgSafeTop)})
		}
	}
	// Pairwise overlap (O(n²); n is small — a slide has <40 text nodes).
	for a := 0; a < len(rects); a++ {
		for b := a + 1; b < len(rects); b++ {
			if area := intersectArea(rects[a], rects[b]); area >= overlapMinArea {
				vs = append(vs, Violation{Kind: "overlap",
					Detail: fmt.Sprintf("text %q overlaps text %q (collision ~%.0f px²) — reposition one so they don't sit on top of each other",
						truncate(rects[a].Text, 20), truncate(rects[b].Text, 20), area)})
			}
		}
	}
	// Per-card overflow: text that renders PAST the right/bottom border of the
	// card it lives in. SVG has no auto-wrap, so CJK body text routinely runs
	// outside its panel while staying far inside the global safe margin (the
	// "字到框框外面" bug) — invisible to the bounds checks above, obvious to a
	// viewer. We flag only text that crosses the border (cardEps past it), so a
	// centred hero number that fills its card is never a false positive.
	const (
		cardEps      = 1.0  // crossing a border by >1px is always overflow
		bodyRightPad = 14.0 // small body/caption/label text must keep this much padding inside the card
		bigTextH     = 46.0 // a text taller than this is a hero number / card title, judged leniently
	)
	cardOverflows := 0
	for _, t := range rects {
		c, ok := smallestContainingCard(t, cards)
		if !ok {
			continue
		}
		// Body text must keep real right padding inside the card; a big centred
		// number that fills its card is only flagged if it actually crosses out.
		rightLimit := c.Right + cardEps
		if t.H < bigTextH {
			rightLimit = c.Right - bodyRightPad
		}
		if t.Right > rightLimit {
			vs = append(vs, Violation{Kind: "overflow-card",
				Detail: fmt.Sprintf("text %q reaches x=%.0f with no padding before the right edge (x=%.0f) of the card it sits inside — shorten the line, wrap it onto a second line, or widen the card so the text stays inside with margin",
					truncate(t.Text, 24), t.Right, c.Right)})
			cardOverflows++
		} else if t.Bottom > c.Bottom+cardEps {
			vs = append(vs, Violation{Kind: "overflow-card",
				Detail: fmt.Sprintf("text %q reaches y=%.0f, below the bottom edge (y=%.0f) of the card it sits inside — remove a line, shrink the text, or make the card taller",
					truncate(t.Text, 24), t.Bottom, c.Bottom)})
			cardOverflows++
		}
		if cardOverflows >= 6 { // cap noise fed to the repair prompt
			break
		}
	}
	// Dedup identical details + keep deterministic order.
	sort.Slice(vs, func(i, j int) bool { return vs[i].Detail < vs[j].Detail })
	seen := map[string]bool{}
	dedup := vs[:0]
	for _, v := range vs {
		if seen[v.Detail] {
			continue
		}
		seen[v.Detail] = true
		dedup = append(dedup, v)
	}
	return dedup
}

func intersectArea(a, b textRect) float64 {
	w := min64(a.Right, b.Right) - max64(a.X, b.X)
	h := min64(a.Bottom, b.Bottom) - max64(a.Y, b.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// smallestContainingCard returns the tightest card the text actually lives in:
// the text must start within the card horizontally AND sit fully inside it
// vertically. The vertical test is the key disambiguator — a slide title or
// kicker floating ABOVE the cards has t.Y < card.Y and is never matched, so a
// wide headline is not mistaken for card overflow. Among the cards that qualify
// we pick the smallest by area (the innermost panel).
func smallestContainingCard(t textRect, cards []cardRect) (cardRect, bool) {
	var best cardRect
	found := false
	bestArea := 0.0
	for _, c := range cards {
		if t.X < c.X-2 || t.X > c.Right { // text starts inside the card horizontally
			continue
		}
		if t.Y < c.Y-2 || t.Bottom > c.Bottom+8 { // and sits fully inside it vertically
			continue
		}
		if area := c.W * c.H; !found || area < bestArea {
			best, bestArea, found = c, area, true
		}
	}
	return best, found
}

// formatViolations renders a slide's violations as a bullet list for the
// repair prompt.
func formatViolations(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		fmt.Fprintf(&b, "  - %s\n", v.Detail)
	}
	return b.String()
}
