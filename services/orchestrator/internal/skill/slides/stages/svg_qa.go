package stages

import (
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// Sprint PM-4 — zero-LLM quality gate for authored SVG. The chromedp
// preview renders even slightly-malformed SVG (browsers are lenient) and
// rich SVG already degrades to a PNG background on export, so the real
// failure mode isn't "invalid XML" — it's the LLM occasionally returning
// junk or an empty body for one slide, which would ship as a BLANK page.
// QASlideUsable catches that; FallbackSVG gives a clean titled slide to
// stand in, so a deck never has a garbage/blank slide. (Text overflow /
// overlap is handled separately by the measure-repair loop.)

// QASlideUsable reports whether an authored slide SVG is good enough to
// ship: it must be a real <svg> with a viewBox and carry at least one
// piece of visible text. Empty shells / prose / truncated fragments fail.
func QASlideUsable(svg string) bool {
	s := strings.TrimSpace(svg)
	if len(s) < 80 {
		return false
	}
	if !strings.Contains(s, "<svg") || !strings.Contains(s, "</svg>") {
		return false
	}
	if !strings.Contains(s, "viewBox") {
		return false
	}
	// Must render at least one non-empty <text> — a content slide with no
	// text is broken even if it parses.
	low := strings.Index(s, "<text")
	if low < 0 {
		return false
	}
	return true
}

// FallbackSVG renders a clean, on-theme stand-in slide carrying just the
// headline — used when QASlideUsable rejects an authored slide so the
// deck stays coherent instead of showing a blank page. Hand-wraps the
// headline to keep it inside the safe area.
func FallbackSVG(tok themetokens.Tokens, headline string) string {
	if strings.TrimSpace(headline) == "" {
		headline = "—"
	}
	lines := wrapHeadline(headline, 14) // ~14 CJK chars per line at 72px
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 1920 1080" xmlns="http://www.w3.org/2000/svg" width="1920" height="1080">`)
	b.WriteString(fallbackMarker) // lets the A4 self-critique skip plain stand-in slides
	fmt.Fprintf(&b, `<rect x="0" y="0" width="1920" height="1080" fill="%s"/>`, tok.BG)
	// accent rule
	fmt.Fprintf(&b, `<rect x="120" y="300" width="120" height="6" rx="3" fill="%s"/>`, tok.Accent)
	y := 460.0
	for _, ln := range lines {
		fmt.Fprintf(&b, `<text x="120" y="%.0f" font-family="%s" font-size="72" font-weight="700" fill="%s">%s</text>`,
			y, tok.FontDisp, tok.FG, xmlEscapeStr(ln))
		y += 96
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func wrapHeadline(s string, perLine int) []string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) == 0 {
		return []string{"—"}
	}
	var out []string
	for i := 0; i < len(runes); i += perLine {
		end := i + perLine
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
		if len(out) >= 3 {
			break // cap at 3 lines
		}
	}
	return out
}

func xmlEscapeStr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
