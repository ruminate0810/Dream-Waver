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
		FontMono: "'Inter', ui-monospace, monospace", Dark: false,
		Accent2: "#1E40AF", Surface: "#F1F5F9", Border: "#E2E8F0", Pattern: "none",
		ImagePalette: "bright white studio, soft daylight, clean composition, single-subject minimal product photography, cool neutral tones",
		Mood:         "clean blue-on-white consulting deck — confident #2563EB blue accents on white, structured data cards / metric grids / comparison tables, hairline rules. FILL the canvas with well-organized substance: every content slide carries an assertion + 2–4 dense cards or a real chart. Whitespace is deliberate framing, NEVER empty padding, a top-heavy half-empty page, or hollow oversized cards. Substantial, gridded, board-ready — not sparse."},
	"corporate": {Key: "corporate", BG: "#FAFAF9", FG: "#0F172A", FGMuted: "#475569", Accent: "#1E3A8A",
		FontDisp: "'IBM Plex Serif', 'Noto Serif SC', Georgia, serif",
		FontBody: "'Manrope', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'Manrope', ui-monospace, monospace", Dark: false,
		Accent2: "#B45309", Surface: "#F3F1ED", Border: "#E4E1DB", Pattern: "grid",
		ImagePalette: "modern glass office towers and boardrooms, navy-and-bronze palette, confident corporate photography, clean daylight",
		Mood:         "blue-chip consultancy — navy authority on warm paper, a left accent bar, structured grids, quiet confidence"},
	"pitch-deck": {Key: "pitch-deck", BG: "#050505", FG: "#FAFAFA", FGMuted: "#A1A1AA", Accent: "#FF6B35",
		FontDisp: "'Space Grotesk', 'Noto Sans SC', system-ui, sans-serif",
		FontBody: "'Inter', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'Space Grotesk', ui-monospace, monospace", Dark: true,
		Accent2: "#FF9E66", Surface: "#141414", Border: "#2A2A2A", Pattern: "diagonal",
		ImagePalette: "startup energy, dark studio, vivid orange spotlights, product hero shots, high contrast, cinematic",
		Mood:         "founder pitch energy — Space Grotesk, electric orange on black, oversized metrics, bold momentum, VC-ready"},
	"academic": {Key: "academic", BG: "#FAFAFA", FG: "#18181B", FGMuted: "#52525B", Accent: "#B91C1C",
		FontDisp: "'IBM Plex Serif', 'Noto Serif SC', Georgia, serif",
		FontBody: "'IBM Plex Sans', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'IBM Plex Mono', ui-monospace, monospace", Dark: false,
		Accent2: "#3F3F46", Surface: "#F3F3F4", Border: "#E4E4E7", Pattern: "none",
		ImagePalette: "library stacks and lab benches, muted scholarly neutrals, IBM Plex aesthetic, even diffuse light",
		Mood:         "research-paper rigor — IBM Plex Serif, section marks, crimson citation accents, footnote-fine rules, evidence first"},
	"playful": {Key: "playful", BG: "#0A0A0F", FG: "#FAFAFA", FGMuted: "#A1A1AA", Accent: "#F472B6",
		FontDisp: "'Nunito', 'Noto Sans SC', system-ui, sans-serif",
		FontBody: "'Nunito', 'Inter', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'Inter', ui-monospace, monospace", Dark: true,
		Accent2: "#818CF8", Surface: "#16161E", Border: "#2E2E3A", Pattern: "dot",
		ImagePalette: "vibrant candy-color gradients, pink and indigo, friendly rounded shapes, energetic playful photography, soft glow",
		Mood:         "friendly and vivid — Nunito rounded, pink + indigo duo, glossy gradient cards, big rounded shapes, approachable energy"},
	"editorial": {Key: "editorial", BG: "#F7F4ED", FG: "#1A1614", FGMuted: "#57534E", Accent: "#B5371E",
		FontDisp: "'Fraunces', 'Noto Serif SC', Georgia, serif",
		FontBody: "'Fraunces', 'Noto Serif SC', Georgia, serif",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: false,
		Accent2: "#6F4A3A", Surface: "#EFEADF", Border: "#DED7C9", Pattern: "none",
		ImagePalette: "warm cream paper and letterpress texture, vermillion ink, vintage magazine photography, soft directional light",
		Mood:         "New Yorker editorial — Fraunces serif, vermillion drop-accents, oversized quotation marks, generous margins, long-read elegance"},
	"retro": {Key: "retro", BG: "#0F0524", FG: "#FFFFFF", FGMuted: "#C4B5FD", Accent: "#FF2D95",
		FontDisp: "'VT323', 'JetBrains Mono', 'Noto Sans SC', system-ui, sans-serif",
		FontBody: "'JetBrains Mono', 'Noto Sans SC', 'PingFang SC', ui-monospace, monospace",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: true,
		Accent2: "#00E5FF", Surface: "#1C0A3E", Border: "#3A1E6E", Pattern: "grid",
		ImagePalette: "80s synthwave, neon magenta and cyan, retro sunset grid horizon, chrome, VHS glow",
		Mood:         "80s synthwave — VT323 pixel display, neon magenta + cyan glow, retro grid horizon, CRT scanline energy"},
	"tech": {Key: "tech", BG: "#0B0E13", FG: "#E5E7EB", FGMuted: "#94A3B8", Accent: "#4ADE80",
		FontDisp: "'JetBrains Mono', 'Noto Sans SC', system-ui, monospace",
		FontBody: "'JetBrains Mono', 'Noto Sans SC', 'PingFang SC', ui-monospace, monospace",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: true,
		Accent2: "#38BDF8", Surface: "#12161D", Border: "#232A35", Pattern: "grid",
		ImagePalette: "terminal IDE, dark code editor, green-on-black monospace, circuit traces, blue accent glow, developer workspace",
		Mood:         "terminal IDE — JetBrains Mono, green prompt + sky accent, shell prefixes, code-grid background, engineer precision"},
	"zen": {Key: "zen", BG: "#F5F0E8", FG: "#1A1815", FGMuted: "#6B655A", Accent: "#B5371E",
		FontDisp: "'Noto Serif JP', 'Noto Serif SC', 'Hiragino Mincho Pro', serif",
		FontBody: "'Noto Sans JP', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: false,
		Accent2: "#6B655A", Surface: "#EDE7DB", Border: "#DCD3C3", Pattern: "none",
		ImagePalette: "Japanese washi paper texture, sumi ink wash, vermillion seal, ikebana, soft natural light, negative space",
		Mood:         "Japanese ma — Noto Serif JP, sumi ink on washi, a single vermillion seal, vast asymmetric emptiness, quiet"},
	"warm": {Key: "warm", BG: "#F4ECDD", FG: "#3D2E1F", FGMuted: "#6B5443", Accent: "#C84B1A",
		FontDisp: "'EB Garamond', 'Noto Serif SC', Georgia, serif",
		FontBody: "'EB Garamond', 'Noto Serif SC', Georgia, serif",
		FontMono: "'Caveat', 'Inter', system-ui, cursive", Dark: false,
		Accent2: "#7C9A6B", Surface: "#ECE1CD", Border: "#DDCDB2", Pattern: "none",
		ImagePalette: "vintage kraft paper, handwritten notes, burnt-orange and sage, warm analog photography, golden-hour light",
		Mood:         "vintage paper warmth — EB Garamond + Caveat hand, burnt orange on kraft, hand-drawn arrows, analog craft, cozy"},
	"noir": {Key: "noir", BG: "#0A0A0B", FG: "#F4F1E8", FGMuted: "#9A9590", Accent: "#E8FF3C",
		FontDisp: "'Bodoni Moda', 'Noto Serif SC', 'Didot', Georgia, serif",
		FontBody: "'Bodoni Moda', 'Noto Serif SC', 'Didot', Georgia, serif",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: true,
		Accent2: "#C2CE3A", Surface: "#161618", Border: "#2A2A2D", Pattern: "none",
		ImagePalette: "high-contrast film noir, chiaroscuro lighting, deep shadow, acid-yellow neon glints, 35mm grain",
		Mood:         "cinema noir — Bodoni Didone drama, acid-yellow on near-black, aperture marks and Roman numerals, high-contrast restraint"},
	"azure": {Key: "azure", BG: "#16357E", FG: "#FFFFFF", FGMuted: "#AFC2EA", Accent: "#FF7A3D",
		FontDisp: "'DM Serif Display', 'Noto Serif SC', Georgia, serif",
		FontBody: "'Manrope', 'Noto Sans SC', 'PingFang SC', system-ui, sans-serif",
		FontMono: "'JetBrains Mono', ui-monospace, monospace", Dark: true,
		Accent2: "#FFA866", Surface: "#1E3F8F", Border: "#33549E", Pattern: "dot",
		IconLib:      "tabler-outline",
		ImagePalette: "deep navy field with warm orange glints, cinematic low-key light, glass-and-steel, corporate-report gravitas",
		Mood:         "deep-navy + warm-orange — clean, confident, board-ready (简约大气). One vivid orange accent (#FF7A3D) carries the eye to the single most important number / label on each slide; everything else is white and soft-blue on the navy field. Fill the canvas with well-structured content — cards, metric rows, comparison tables, a chart — never leave a half-empty page."},
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

// BackgroundSVG returns a deterministic, deck-wide background block injected
// behind every authored slide: a subtle gradient (a soft top-light + vignette
// on dark themes; a gentle top→bottom wash on light ones) plus a whisper-faint
// texture when the theme has a motif. Painting this in the render layer — ONCE,
// identically, for every page — is what makes a deck feel coherent ("协和"):
// no flat monotone background, and zero page-to-page drift from each per-slide
// LLM call guessing its own gradient. ids are dw-prefixed to avoid colliding
// with author-drawn <defs>.
func (t Tokens) BackgroundSVG() string {
	var b strings.Builder
	b.WriteString(`<defs>`)
	if t.Dark {
		c0 := blendHex(t.BG, "#FFFFFF", 0.05) // soft light near the top
		c1 := blendHex(t.BG, "#000000", 0.24) // vignette toward the edges
		b.WriteString(`<radialGradient id="dwbg" cx="50%" cy="30%" r="95%"><stop offset="0" stop-color="` + c0 + `"/><stop offset="1" stop-color="` + c1 + `"/></radialGradient>`)
	} else {
		c0 := blendHex(t.BG, "#FFFFFF", 0.30) // cleaner up top
		c1 := blendHex(t.BG, t.FGMuted, 0.06) // barely deeper at the foot
		b.WriteString(`<linearGradient id="dwbg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="` + c0 + `"/><stop offset="1" stop-color="` + c1 + `"/></linearGradient>`)
	}
	patCol := blendHex(t.BG, t.FG, 0.10)
	patRect := ""
	switch t.Pattern {
	case "dot":
		b.WriteString(`<pattern id="dwpat" width="46" height="46" patternUnits="userSpaceOnUse"><circle cx="2" cy="2" r="1.5" fill="` + patCol + `"/></pattern>`)
		patRect = `<rect width="1920" height="1080" fill="url(#dwpat)" fill-opacity="0.55"/>`
	case "grid":
		b.WriteString(`<pattern id="dwpat" width="64" height="64" patternUnits="userSpaceOnUse"><path d="M64 0H0V64" fill="none" stroke="` + patCol + `" stroke-width="1"/></pattern>`)
		patRect = `<rect width="1920" height="1080" fill="url(#dwpat)" fill-opacity="0.5"/>`
	case "diagonal":
		b.WriteString(`<pattern id="dwpat" width="42" height="42" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><line x1="0" y1="0" x2="0" y2="42" stroke="` + patCol + `" stroke-width="1"/></pattern>`)
		patRect = `<rect width="1920" height="1080" fill="url(#dwpat)" fill-opacity="0.5"/>`
	}
	b.WriteString(`</defs>`)
	b.WriteString(`<rect width="1920" height="1080" fill="url(#dwbg)"/>`)
	b.WriteString(patRect)
	return b.String()
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
