// Package themetokens is the single Go-side source of truth for each
// theme's core design tokens (background / foreground / accent / fonts).
//
// Sprint SV-1: the SVG generation path needs these values to (a) inject
// a <style>:root{...}</style> block so LLM-authored SVG can reference
// var(--bg) etc., and (b) feed the LLM concrete hex/font hints in its
// prompt so it picks colors that match the theme.
//
// These mirror the canonical CSS in packages/slide-templates/_themes/*.css.
// Kept deliberately as a flat literal table — 12 themes, rarely changes;
// a generator over the CSS would be over-engineering. If a theme's CSS
// tokens change, update the matching row here.
//
// Font fields carry the FULL CSS font-family stack (web fonts + CJK
// fallbacks) so the browser-rendered SVG looks right. The SV-3 PPTX
// converter reads PPTXFont* for the system-font name PowerPoint should
// use (first family, unquoted).
package themetokens

import "strings"

// Tokens is one theme's resolved design tokens.
type Tokens struct {
	Key       string // schema.Theme key, e.g. "editorial"
	BG        string // #RRGGBB — slide background
	FG        string // #RRGGBB — primary text/ink
	FGMuted   string // #RRGGBB — secondary text
	Accent    string // #RRGGBB — brand accent (brand override default)
	FontBody  string // full CSS font-family stack
	FontDisp  string // full CSS font-family stack (display/headlines)
	FontMono  string // full CSS font-family stack (kickers/labels)
	// Dark reports whether the background is dark (so the SVG prompt can
	// tell the LLM "this is a dark slide; use light text").
	Dark bool
}

// PPTXDisplayFont returns the first (primary) display font family with
// quotes stripped — the bare name PowerPoint should request. Falls back
// to "Georgia" if parsing fails.
func (t Tokens) PPTXDisplayFont() string { return firstFamily(t.FontDisp, "Georgia") }

// PPTXBodyFont returns the primary body font family for PowerPoint.
func (t Tokens) PPTXBodyFont() string { return firstFamily(t.FontBody, "Calibri") }

// PPTXMonoFont returns the primary mono font family for PowerPoint.
func (t Tokens) PPTXMonoFont() string { return firstFamily(t.FontMono, "Consolas") }

func firstFamily(stack, def string) string {
	first := stack
	if i := strings.IndexByte(stack, ','); i >= 0 {
		first = stack[:i]
	}
	first = strings.TrimSpace(first)
	first = strings.Trim(first, `'"`)
	// Strip any var(...) wrapper that snuck in.
	if strings.HasPrefix(first, "var(") {
		return def
	}
	if first == "" {
		return def
	}
	return first
}

// table is the canonical token set, mirrored from _themes/*.css. Accent
// values are the brand-override DEFAULTS (the bare hex inside
// var(--brand-primary, #XXX)).
var table = map[string]Tokens{
	"minimalist": {Key: "minimalist", BG: "#FFFFFF", FG: "#0F172A", FGMuted: "#64748B", Accent: "#2563EB",
		FontDisp: "'DM Serif Display', 'Noto Serif SC', Georgia, serif",
		FontBody: "'Inter', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'Inter', ui-monospace, monospace", Dark: false},
	"corporate": {Key: "corporate", BG: "#FAFAF9", FG: "#0F172A", FGMuted: "#475569", Accent: "#1E3A8A",
		FontDisp: "'IBM Plex Serif', 'Noto Serif SC', Georgia, serif",
		FontBody: "'Manrope', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'Manrope', ui-monospace, monospace", Dark: false},
	"pitch-deck": {Key: "pitch-deck", BG: "#050505", FG: "#FAFAFA", FGMuted: "#A1A1AA", Accent: "#FF6B35",
		FontDisp: "'Space Grotesk', 'Noto Sans SC', system-ui, sans-serif",
		FontBody: "'Inter', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'Space Grotesk', ui-monospace, monospace", Dark: true},
	"academic": {Key: "academic", BG: "#FAFAFA", FG: "#18181B", FGMuted: "#52525B", Accent: "#B91C1C",
		FontDisp: "'IBM Plex Serif', 'Noto Serif SC', Georgia, serif",
		FontBody: "'IBM Plex Sans', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'IBM Plex Mono', ui-monospace, monospace", Dark: false},
	"playful": {Key: "playful", BG: "#0A0A0F", FG: "#FAFAFA", FGMuted: "#A1A1AA", Accent: "#F472B6",
		FontDisp: "'Nunito', 'Noto Sans SC', system-ui, sans-serif",
		FontBody: "'Nunito', 'Inter', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'Inter', ui-monospace, monospace", Dark: true},
	"editorial": {Key: "editorial", BG: "#F7F4ED", FG: "#1A1614", FGMuted: "#57534E", Accent: "#B5371E",
		FontDisp: "'Fraunces', 'Noto Serif SC', Georgia, serif",
		FontBody: "'Fraunces', 'Noto Serif SC', Georgia, serif",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: false},
	"retro": {Key: "retro", BG: "#0F0524", FG: "#FFFFFF", FGMuted: "#C4B5FD", Accent: "#FF2D95",
		FontDisp: "'VT323', 'JetBrains Mono', 'Noto Sans SC', system-ui, sans-serif",
		FontBody: "'JetBrains Mono', 'Noto Sans SC', 'PingFang SC', ui-monospace, monospace",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: true},
	"tech": {Key: "tech", BG: "#0B0E13", FG: "#E5E7EB", FGMuted: "#94A3B8", Accent: "#4ADE80",
		FontDisp: "'JetBrains Mono', 'Noto Sans SC', system-ui, monospace",
		FontBody: "'JetBrains Mono', 'Noto Sans SC', 'PingFang SC', ui-monospace, monospace",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: true},
	"zen": {Key: "zen", BG: "#F5F0E8", FG: "#1A1815", FGMuted: "#6B655A", Accent: "#B5371E",
		FontDisp: "'Noto Serif JP', 'Noto Serif SC', 'Hiragino Mincho Pro', serif",
		FontBody: "'Noto Sans JP', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: false},
	"warm": {Key: "warm", BG: "#F4ECDD", FG: "#3D2E1F", FGMuted: "#6B5443", Accent: "#C84B1A",
		FontDisp: "'EB Garamond', 'Noto Serif SC', Georgia, serif",
		FontBody: "'EB Garamond', 'Noto Serif SC', Georgia, serif",
		FontMono: "'Caveat', 'Inter', system-ui, cursive", Dark: false},
	"noir": {Key: "noir", BG: "#0A0A0B", FG: "#F4F1E8", FGMuted: "#9A9590", Accent: "#E8FF3C",
		FontDisp: "'Bodoni Moda', 'Noto Serif SC', 'Didot', Georgia, serif",
		FontBody: "'Bodoni Moda', 'Noto Serif SC', 'Didot', Georgia, serif",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: true},
	"azure": {Key: "azure", BG: "#16357E", FG: "#FFFFFF", FGMuted: "#C2D2F2", Accent: "#9DBDF5",
		FontDisp: "'DM Serif Display', 'Noto Serif SC', Georgia, serif",
		FontBody: "'Manrope', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: true},
}

// Get returns the tokens for a theme key, falling back to minimalist
// for unknown keys (never panics — the caller always gets usable tokens).
func Get(theme string) Tokens {
	if t, ok := table[theme]; ok {
		return t
	}
	return table["minimalist"]
}

// StyleBlock renders a <style>:root{...}</style> the SVG render shell
// prepends so LLM-authored SVG referencing var(--bg) etc. resolves.
// Brand overrides (when non-empty) win over the theme defaults, mirroring
// the apply_brand CSS-var convention.
func (t Tokens) StyleBlock(brandPrimary, brandAccent, brandFont string) string {
	accent := t.Accent
	if brandPrimary != "" {
		accent = brandPrimary
	}
	accent2 := t.Accent
	if brandAccent != "" {
		accent2 = brandAccent
	}
	body := t.FontBody
	if brandFont != "" {
		body = brandFont
	}
	var b strings.Builder
	b.WriteString("<style>:root{")
	b.WriteString("--bg:" + t.BG + ";")
	b.WriteString("--fg:" + t.FG + ";")
	b.WriteString("--fg-muted:" + t.FGMuted + ";")
	b.WriteString("--accent:" + accent + ";")
	b.WriteString("--accent-2:" + accent2 + ";")
	b.WriteString("--font-display:" + t.FontDisp + ";")
	b.WriteString("--font-body:" + body + ";")
	b.WriteString("--font-mono:" + t.FontMono + ";")
	b.WriteString("}")
	// Full-bleed reset for the SVG render shell.
	b.WriteString("html,body{margin:0;padding:0;width:1920px;height:1080px;overflow:hidden;background:" + t.BG + ";}")
	b.WriteString("svg{display:block;width:1920px;height:1080px;}")
	b.WriteString("</style>")
	return b.String()
}
