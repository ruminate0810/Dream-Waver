package stages

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// PMR — deterministic SVG chrome.
//
// The model authors each slide's CONTENT. The engine owns the two things that
// must be byte-identical on every page for a deck to read as coherent ("协和"):
//   - the BACKGROUND — a unified gradient + faint texture (Tokens.BackgroundSVG)
//   - the FOOTER — deck title + page number, same font/size/position everywhere
//
// Doing these in the render layer kills the page-to-page drift you get when
// each per-slide LLM call paints its own background and footer (flat monotone
// fills, wandering footer sizes). The author prompt tells the model not to draw
// them; this strips any it drew anyway (belt-and-braces) and injects ours.

var (
	svgOpenRe     = regexp.MustCompile(`(?s)<svg\b[^>]*>`)
	anyRectRe     = regexp.MustCompile(`(?s)<rect\b[^>]*?/?>`)
	footerGroupRe = regexp.MustCompile(`(?s)<g\b[^>]*\bid=["']footer["'][^>]*>.*?</g>`)
	rectW1920Re   = regexp.MustCompile(`\bwidth=["']1920(?:px)?["']`)
	rectH1080Re   = regexp.MustCompile(`\bheight=["']1080(?:px)?["']`)
	rectSolidRe   = regexp.MustCompile(`\bfill=["'](?:#[0-9A-Fa-f]{3,8}|url\(#[^)]+\))["']`)
)

// finalizeSVGChrome strips the model's flat full-bleed background and any
// author footer, then injects the deck-wide background (behind everything) and
// a uniform footer (in front). Applied to the final shipped SVG — real or
// fallback — so every page shares the exact same canvas + footer.
func finalizeSVGChrome(svg string, tok themetokens.Tokens, deckTitle string, page, total int) string {
	if strings.TrimSpace(svg) == "" {
		return svg
	}
	svg = stripLeadingBGRect(svg)
	svg = footerGroupRe.ReplaceAllString(svg, "") // drop any author footer; engine owns it

	if loc := svgOpenRe.FindStringIndex(svg); loc != nil {
		svg = svg[:loc[1]] + tok.BackgroundSVG() + svg[loc[1]:]
	}
	footer := deterministicFooter(tok, deckTitle, page, total)
	if i := strings.LastIndex(svg, "</svg>"); i >= 0 {
		svg = svg[:i] + footer + svg[i:]
	}
	return svg
}

// stripLeadingBGRect removes the FIRST full-bleed (1920×1080) solid-hex <rect>
// — the model's flat background — so the injected gradient isn't painted over.
// Tinted/gradient overlays (fill-opacity, url(#…)) are left untouched. Only the
// first few rects are considered (the bg is always near the top).
func stripLeadingBGRect(svg string) string {
	for _, loc := range anyRectRe.FindAllStringIndex(svg, 6) {
		tag := svg[loc[0]:loc[1]]
		if rectW1920Re.MatchString(tag) && rectH1080Re.MatchString(tag) && rectSolidRe.MatchString(tag) {
			return svg[:loc[0]] + svg[loc[1]:]
		}
	}
	return svg
}

// deterministicFooter builds the uniform footer: a hairline rule + deck label
// (left) + "NN / NN" page counter (right), in the theme's mono face at a fixed
// size and baseline — identical on every page.
func deterministicFooter(tok themetokens.Tokens, deckTitle string, page, total int) string {
	if total < 1 {
		total = 1
	}
	title := footerShortTitle(deckTitle)
	return fmt.Sprintf(
		`<g id="dwfooter"><line x1="120" y1="982" x2="1800" y2="982" stroke="%s" stroke-width="1"/>`+
			`<text x="120" y="1018" font-family="%s" font-size="20" letter-spacing="1" fill="%s">%s</text>`+
			`<text x="1800" y="1018" font-family="%s" font-size="20" letter-spacing="1" fill="%s" text-anchor="end">%02d / %02d</text></g>`,
		tok.Border, tok.FontMono, tok.FGMuted, xmlEscapeStr(title),
		tok.FontMono, tok.FGMuted, page, total)
}

// footerShortTitle trims the deck title to a tidy footer label — cut at the
// first separator so a long subtitle doesn't collide with the page counter.
func footerShortTitle(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{"：", ":", " — ", "—", " - ", " · ", "｜", "|"} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
			break
		}
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) > 22 {
		r = r[:22]
	}
	return string(r)
}
