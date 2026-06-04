// Package svgicons is a tiny locked vector-icon library for the SVG
// author path (Sprint PM-1). ppt-master gets a "designed" feel partly
// from a CONSISTENT icon set (one stylistic family, not ad-hoc clip art).
// The LLM references icons as <use data-icon="dw/<name>" x= y= width=
// height= fill="#HEX"/>; Inline() resolves each into a real <g> of vector
// shapes before the SVG is rendered, so the icon shows in both the
// chromedp preview and the PNG-background export.
//
// The set is intentionally small + uniform: clean line glyphs on a 24×24
// grid, single-colour, stroke-based (apply the icon's colour via the
// fill="" attr, which we map onto stroke). Keeping ONE family locked is
// the whole point — the deck never mixes icon styles.
package svgicons

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// icons maps a name to its 24×24 inner SVG. {C} is substituted with the
// requested colour. Glyphs are line-style (stroke {C}, fill none) unless a
// solid mark reads better. Authored from basic paths so they render
// identically everywhere.
var icons = map[string]string{
	"check":       `<path d="M5 12.5l4.5 4.5L19 7" fill="none" stroke="{C}" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/>`,
	"x":           `<path d="M6 6l12 12M18 6L6 18" fill="none" stroke="{C}" stroke-width="2.4" stroke-linecap="round"/>`,
	"arrow-right": `<path d="M4 12h15M13 6l6 6-6 6" fill="none" stroke="{C}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/>`,
	"arrow-up":    `<path d="M12 20V5M6 11l6-6 6 6" fill="none" stroke="{C}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/>`,
	"trending-up": `<path d="M3 17l6-6 4 4 8-8M15 7h6v6" fill="none" stroke="{C}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/>`,
	"bolt":        `<path d="M13 2L4 14h6l-1 8 9-12h-6l1-8z" fill="{C}"/>`,
	"shield":      `<path d="M12 2l8 3v6c0 5-3.4 8.4-8 11-4.6-2.6-8-6-8-11V5z" fill="none" stroke="{C}" stroke-width="2" stroke-linejoin="round"/>`,
	"target":      `<circle cx="12" cy="12" r="9" fill="none" stroke="{C}" stroke-width="2"/><circle cx="12" cy="12" r="4.6" fill="none" stroke="{C}" stroke-width="2"/><circle cx="12" cy="12" r="1.3" fill="{C}"/>`,
	"chart":       `<path d="M4 20V4M4 20h16M8 20v-7M13 20V9M18 20v-5" fill="none" stroke="{C}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/>`,
	"layers":      `<path d="M12 3l9 5-9 5-9-5 9-5zM3 13l9 5 9-5M3 17l9 5 9-5" fill="none" stroke="{C}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>`,
	"cpu":         `<rect x="6" y="6" width="12" height="12" rx="2" fill="none" stroke="{C}" stroke-width="2"/><rect x="10" y="10" width="4" height="4" fill="{C}"/><path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3" stroke="{C}" stroke-width="2" stroke-linecap="round"/>`,
	"bulb":        `<path d="M9 18h6M10 21h4M12 3a6 6 0 0 1 4 10.5c-.7.7-1 1.3-1 2.5H9c0-1.2-.3-1.8-1-2.5A6 6 0 0 1 12 3z" fill="none" stroke="{C}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>`,
	"lock":        `<rect x="5" y="11" width="14" height="9" rx="2" fill="none" stroke="{C}" stroke-width="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3" fill="none" stroke="{C}" stroke-width="2"/>`,
	"globe":       `<circle cx="12" cy="12" r="9" fill="none" stroke="{C}" stroke-width="2"/><path d="M3 12h18M12 3c2.5 2.5 2.5 15 0 18M12 3c-2.5 2.5-2.5 15 0 18" fill="none" stroke="{C}" stroke-width="1.8"/>`,
	"users":       `<circle cx="9" cy="8" r="3.2" fill="none" stroke="{C}" stroke-width="2"/><path d="M3.5 20c0-3.3 2.5-5.5 5.5-5.5s5.5 2.2 5.5 5.5" fill="none" stroke="{C}" stroke-width="2" stroke-linecap="round"/><path d="M16 5.2A3.2 3.2 0 0 1 16 14M17 14.6c2.6.4 4.5 2.5 4.5 5.4" fill="none" stroke="{C}" stroke-width="2" stroke-linecap="round"/>`,
	"rocket":      `<path d="M12 3c3.5 2 5 5 5 9l-2.5 3h-5L7 12c0-4 1.5-7 5-9zM9.5 18L8 21M14.5 18L16 21M5 14l-2 1 1.5 3 2.5-1" fill="none" stroke="{C}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><circle cx="12" cy="10" r="1.6" fill="{C}"/>`,
	"coin":        `<circle cx="12" cy="12" r="9" fill="none" stroke="{C}" stroke-width="2"/><path d="M12 7v10M9.5 9.2c0-1.2 1.1-2 2.5-2s2.5.8 2.5 2-1.1 1.6-2.5 1.8-2.5.6-2.5 1.9 1.1 2.1 2.5 2.1 2.5-.8 2.5-2" fill="none" stroke="{C}" stroke-width="1.8" stroke-linecap="round"/>`,
	"clock":       `<circle cx="12" cy="12" r="9" fill="none" stroke="{C}" stroke-width="2"/><path d="M12 7v5l3.5 2" fill="none" stroke="{C}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>`,
	"database":    `<ellipse cx="12" cy="6" rx="7" ry="3" fill="none" stroke="{C}" stroke-width="2"/><path d="M5 6v12c0 1.7 3.1 3 7 3s7-1.3 7-3V6M5 12c0 1.7 3.1 3 7 3s7-1.3 7-3" fill="none" stroke="{C}" stroke-width="2"/>`,
	"code":        `<path d="M8 8l-4 4 4 4M16 8l4 4-4 4M13 5l-2 14" fill="none" stroke="{C}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/>`,
	"star":        `<path d="M12 3l2.6 5.5 6 .9-4.3 4.3 1 6-5.3-2.9-5.3 2.9 1-6L3.4 9.4l6-.9z" fill="{C}"/>`,
	"flag":        `<path d="M6 21V4M6 4h11l-2 4 2 4H6" fill="none" stroke="{C}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>`,
}

// Match <use …data-icon=…/> with EITHER single or double quotes (LLMs
// emit both). attrRe likewise captures either quote style.
var useRe = regexp.MustCompile(`<use\b[^>]*\bdata-icon=['"][^'"]*['"][^>]*/?>`)
var attrRe = regexp.MustCompile(`([a-zA-Z-]+)=['"]([^'"]*)['"]`)

// Inline replaces every <use data-icon="lib/name" …/> with the resolved
// icon glyph as a positioned, scaled <g>. Unknown icons are dropped
// (removed) so they never render as a broken reference. fallbackColor is
// used when the <use> has no fill.
func Inline(svg, fallbackColor string) string {
	if !strings.Contains(svg, "data-icon") {
		return svg
	}
	return useRe.ReplaceAllStringFunc(svg, func(tag string) string {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			attrs[m[1]] = m[2]
		}
		name := attrs["data-icon"]
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:] // strip "lib/" prefix
		}
		glyph, ok := icons[name]
		if !ok {
			return "" // unknown icon → render nothing, not a broken ref
		}
		x := num(attrs["x"], 0)
		y := num(attrs["y"], 0)
		w := num(attrs["width"], 48)
		h := num(attrs["height"], 48)
		color := attrs["fill"]
		if color == "" || color == "none" {
			color = fallbackColor
		}
		sx := w / 24.0
		sy := h / 24.0
		body := strings.ReplaceAll(glyph, "{C}", color)
		return fmt.Sprintf(`<g transform="translate(%g,%g) scale(%g,%g)">%s</g>`, x, y, sx, sy, body)
	})
}

// Names returns the available icon names (for the prompt inventory).
func Names() []string {
	out := make([]string, 0, len(icons))
	for k := range icons {
		out = append(out, k)
	}
	return out
}

func num(s string, def float64) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(s, "px"))
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}
