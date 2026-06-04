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
	Key      string // schema.Theme key, e.g. "editorial"
	BG       string // #RRGGBB — slide background
	FG       string // #RRGGBB — primary text/ink
	FGMuted  string // #RRGGBB — secondary text
	Accent   string // #RRGGBB — brand accent (brand override default)
	FontBody string // full CSS font-family stack
	FontDisp string // full CSS font-family stack (display/headlines)
	FontMono string // full CSS font-family stack (kickers/labels)
	// Dark reports whether the background is dark (so the SVG prompt can
	// tell the LLM "this is a dark slide; use light text").
	Dark bool

	// Sprint PM — rich "spec_lock" tokens for ppt-master-grade SVG. These
	// are optional in the table: Get() fills sensible derived defaults so
	// existing themes keep working. Set them explicitly to art-direct a
	// theme's depth, motif, and imagery.
	Accent2      string // #RRGGBB — secondary accent (depth / 2nd emphasis); default = Accent
	Surface      string // #RRGGBB — card/panel fill (subtle lift off BG); default = blend(BG,FG,0.07)
	Border       string // #RRGGBB — hairline border on panels; default = blend(BG,FG,0.16)
	Pattern      string // background motif hint: "grid"|"dot"|"diagonal"|"none"; default "none"
	IconLib      string // locked vector icon library: "tabler-outline"|"tabler-filled"|"phosphor"; default "tabler-outline"
	ImagePalette string // AI-image palette hint (keeps deck imagery coherent); default derived from Dark
	Mood         string // one-line aesthetic the LLM should channel; default derived
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
	"azure": {Key: "azure", BG: "#16357E", FG: "#FFFFFF", FGMuted: "#AFC2EA", Accent: "#F5B841",
		FontDisp: "'DM Serif Display', 'Noto Serif SC', Georgia, serif",
		FontBody: "'Manrope', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: true,
		Accent2: "#6E92D6", Surface: "#1E3F8F", Border: "#33549E", Pattern: "dot",
		IconLib:      "tabler-outline",
		ImagePalette: "deep navy field with warm gold glints, cinematic low-key light, glass-and-steel, financial-report gravitas",
		Mood:         "deep-navy editorial with a single warm-gold accent — a premium financial annual report; gold appears on ONLY the one number that matters per slide"},
}

// Get returns the tokens for a theme key, falling back to minimalist
// for unknown keys (never panics — the caller always gets usable tokens).
// Rich spec_lock fields left blank in the table are filled with derived
// defaults here, so every theme exposes a complete token set.
func Get(theme string) Tokens {
	t, ok := table[theme]
	if !ok {
		t = table["minimalist"]
	}
	return t.withDefaults()
}

// withDefaults fills the optional Sprint-PM spec_lock fields from the
// core palette when a theme didn't set them explicitly.
func (t Tokens) withDefaults() Tokens {
	if t.Accent2 == "" {
		t.Accent2 = t.Accent
	}
	if t.Surface == "" {
		t.Surface = blendHex(t.BG, t.FG, 0.07)
	}
	if t.Border == "" {
		t.Border = blendHex(t.BG, t.FG, 0.16)
	}
	if t.Pattern == "" {
		t.Pattern = "none"
	}
	if t.IconLib == "" {
		t.IconLib = "tabler-outline"
	}
	if t.ImagePalette == "" {
		if t.Dark {
			t.ImagePalette = "moody, cinematic, low-key lighting, rich shadows, desaturated with a single accent hue"
		} else {
			t.ImagePalette = "bright, airy, clean editorial light, soft natural tones, generous negative space"
		}
	}
	if t.Mood == "" {
		t.Mood = "clean, confident, editorial — restrained palette, generous whitespace, one clear focal point per slide"
	}
	return t
}

// blendHex linearly mixes a toward b by t∈[0,1]; both #RRGGBB. Used to
// derive surface/border tints from the core palette.
func blendHex(a, b string, ratio float64) string {
	ar, ag, ab, ok1 := hexRGB(a)
	br, bg, bbl, ok2 := hexRGB(b)
	if !ok1 || !ok2 {
		return a
	}
	mix := func(x, y int) int {
		v := int(float64(x) + (float64(y)-float64(x))*ratio)
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return v
	}
	const hexd = "0123456789ABCDEF"
	enc := func(v int) string { return string([]byte{hexd[v>>4], hexd[v&0xF]}) }
	return "#" + enc(mix(ar, br)) + enc(mix(ag, bg)) + enc(mix(ab, bbl))
}

func hexRGB(s string) (r, g, b int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	val := func(c byte) (int, bool) {
		switch {
		case c >= '0' && c <= '9':
			return int(c - '0'), true
		case c >= 'a' && c <= 'f':
			return int(c-'a') + 10, true
		case c >= 'A' && c <= 'F':
			return int(c-'A') + 10, true
		}
		return 0, false
	}
	hex2 := func(hi, lo byte) (int, bool) {
		h, ok1 := val(hi)
		l, ok2 := val(lo)
		return h<<4 | l, ok1 && ok2
	}
	var o1, o2, o3 bool
	r, o1 = hex2(s[0], s[1])
	g, o2 = hex2(s[2], s[3])
	b, o3 = hex2(s[4], s[5])
	return r, g, b, o1 && o2 && o3
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
