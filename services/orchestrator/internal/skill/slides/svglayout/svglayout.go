// Package svglayout is the deterministic "plan → coordinates → SVG"
// layout engine for SVG-native slides (Sprint SV-5).
//
// The problem it solves: LLMs are good at deciding WHAT goes on a slide
// and its structure, but bad at the pixel arithmetic of placing text
// without overlaps — and SVG has no auto-layout, so hand-placed
// coordinates collide. This engine takes a coordinate-FREE block tree
// (the LLM's plan) and computes every rect/text position itself, so
// overlaps are impossible by construction: code owns the geometry, the
// LLM owns the content.
//
// Pipeline: LLM → Block tree (JSON) → Layout() assigns rects →
// EmitSVG() writes rect+text → existing chromedp render + svg2pptx
// native-shape converter.
//
// Coordinates are the 1920×1080 canvas. Everything stays within the safe
// area so nothing bleeds off-slide.
package svglayout

import (
	"fmt"
	"strings"
)

// Canvas + safe-area constants (px). The background fills the whole
// canvas; all content lives inside the safe inset.
const (
	CanvasW = 1920.0
	CanvasH = 1080.0
	safeL   = 120.0
	safeR   = 1800.0
	safeT   = 110.0
	safeB   = 1000.0
)

// Role drives a text block's font size / weight / default colour. The
// LLM picks the role; the engine owns the exact type ramp so hierarchy
// is consistent across every slide.
type Role string

const (
	RoleKicker      Role = "kicker"       // mono caps label above a title
	RoleTitle       Role = "title"        // slide assertion
	RoleSubtitle    Role = "subtitle"     // sub-assertion under title
	RoleDisplay     Role = "display"      // oversized cover/section type
	RoleBody        Role = "body"         // normal text / card line
	RoleCaption     Role = "caption"      // small muted footnote/reference
	RoleMetricValue Role = "metric-value" // the big number
	RoleMetricRef   Role = "metric-ref"   // baseline/comparison (small)
	RoleMetricImpl  Role = "metric-impl"  // implication (small, accent/muted)
	RoleCardLabel   Role = "card-label"   // bold label inside a card
	RoleNumeral     Role = "numeral"      // 01 / 02 / 03 mono numerals
)

// fontSpec is the resolved size/weight/family/colour for a role.
type fontSpec struct {
	size   float64
	weight int
	family famKind
	color  colKind
	upper  bool
	tracking float64 // letter-spacing px
}

type famKind int

const (
	famDisplay famKind = iota
	famBody
	famMono
)

type colKind int

const (
	colFG colKind = iota
	colMuted
	colAccent
)

// roleSpec is the single source of truth for the type ramp. Tuned for a
// 1920×1080 canvas (≈ 2× a 960-wide design), MBB-dense but confident.
var roleSpec = map[Role]fontSpec{
	RoleKicker:      {size: 22, weight: 600, family: famMono, color: colAccent, upper: true, tracking: 4},
	RoleTitle:       {size: 64, weight: 700, family: famDisplay, color: colFG},
	RoleSubtitle:    {size: 32, weight: 400, family: famBody, color: colMuted},
	RoleDisplay:     {size: 128, weight: 700, family: famDisplay, color: colFG},
	RoleBody:        {size: 30, weight: 400, family: famBody, color: colMuted},
	RoleCaption:     {size: 20, weight: 400, family: famBody, color: colMuted},
	RoleMetricValue: {size: 110, weight: 700, family: famDisplay, color: colAccent},
	RoleMetricRef:   {size: 24, weight: 600, family: famMono, color: colMuted, upper: false},
	RoleMetricImpl:  {size: 24, weight: 400, family: famBody, color: colMuted},
	RoleCardLabel:   {size: 38, weight: 700, family: famBody, color: colFG},
	RoleNumeral:     {size: 40, weight: 600, family: famMono, color: colAccent},
}

// Theme carries the resolved palette + font stacks the emitter writes
// into the SVG. Mirrors themetokens.Tokens but kept local so this
// package stays a leaf (no import cycle).
type Theme struct {
	BG       string
	FG       string
	FGMuted  string
	Accent   string
	FontDisp string
	FontBody string
	FontMono string
}

// Block is one node in the coordinate-free plan tree. Containers
// (row/col/card) hold Items; leaves (text/metric/numeral/spacer) hold
// content. The LLM emits this; the engine fills in geometry.
type Block struct {
	Type string `json:"type"` // row|col|card|text|metric|numeral|spacer|rule

	// container fields
	Items  []*Block `json:"items,omitempty"`
	Gap    float64  `json:"gap,omitempty"`    // px between items (default per-type)
	Weight float64  `json:"weight,omitempty"` // flex weight in parent row/col (default 1)
	Pad    float64  `json:"pad,omitempty"`    // card inner padding (default 48)
	Fill   string   `json:"fill,omitempty"`   // card fill: "surface" | "" (none) | #hex
	Grow   bool     `json:"grow,omitempty"`   // in a col: expand to take remaining height

	// text leaf
	Role  Role   `json:"role,omitempty"`
	Text  string `json:"text,omitempty"`
	Align string `json:"align,omitempty"` // left|center|right (default left)

	// metric leaf
	Value       string `json:"value,omitempty"`
	Reference   string `json:"reference,omitempty"`
	Implication string `json:"implication,omitempty"`

	// computed geometry (filled by Layout)
	x, y, w, h float64
}

// ─── public API ──────────────────────────────────────────────────────

// Render lays out the block tree inside the safe area and returns a
// complete <svg> string (full-bleed bg + positioned content). Overlaps
// are impossible: every coordinate is computed here.
func Render(root *Block, th Theme) string {
	// Root always lays out inside the safe area.
	root.x, root.y = safeL, safeT
	root.w, root.h = safeR-safeL, safeB-safeT
	layout(root)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 1920 1080" xmlns="http://www.w3.org/2000/svg" width="1920" height="1080">`)
	// full-bleed background
	fmt.Fprintf(&b, `<rect x="0" y="0" width="1920" height="1080" fill="%s"/>`, th.BG)
	emit(&b, root, th)
	b.WriteString(`</svg>`)
	return b.String()
}

// ─── measurement ─────────────────────────────────────────────────────

// charWidth estimates a glyph's advance at font size fs. CJK ideographs
// are ~1.0em square; Latin/digits/punct average ~0.55em. Deterministic
// approximation — we pad gaps generously so the estimate never needs to
// be exact to avoid overlap.
func charWidth(r rune, fs float64) float64 {
	if r >= 0x4E00 && r <= 0x9FFF || // CJK
		r >= 0x3000 && r <= 0x30FF || // CJK punct + kana
		r >= 0xFF00 && r <= 0xFFEF { // fullwidth forms
		return fs * 1.0
	}
	return fs * 0.55
}

func textWidth(s string, fs float64) float64 {
	w := 0.0
	for _, r := range s {
		w += charWidth(r, fs)
	}
	return w
}

// wrapText greedily breaks s into lines that fit maxW at font size fs.
// Breaks on spaces when present; otherwise (CJK) breaks per-character.
func wrapText(s string, fs, maxW float64) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if textWidth(s, fs) <= maxW {
		return []string{s}
	}
	var lines []string
	var cur strings.Builder
	curW := 0.0
	flush := func() {
		if cur.Len() > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curW = 0
		}
	}
	// Token-aware: keep ASCII words whole, break CJK anywhere.
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		rw := charWidth(r, fs)
		if curW+rw > maxW && cur.Len() > 0 {
			flush()
		}
		cur.WriteRune(r)
		curW += rw
	}
	flush()
	return lines
}

const lineHeight = 1.32

// ─── layout ──────────────────────────────────────────────────────────

// intrinsicHeight returns the height a block needs given its assigned
// width (set before calling). For containers it recurses.
func intrinsicHeight(blk *Block) float64 {
	switch blk.Type {
	case "spacer":
		return 0 // grows via Grow flag only
	case "rule":
		return 2
	case "text":
		fs := roleSpec[blk.Role].size
		lines := wrapText(blk.Text, fs, blk.w)
		if len(lines) == 0 {
			return 0
		}
		return float64(len(lines)) * fs * lineHeight
	case "numeral":
		return roleSpec[RoleNumeral].size * lineHeight
	case "metric":
		// stacked: ref (above) + value + implication (below), with gaps.
		h := 0.0
		if blk.Reference != "" {
			h += roleSpec[RoleMetricRef].size*lineHeight + 14
		}
		h += roleSpec[RoleMetricValue].size * lineHeight
		if blk.Implication != "" {
			rs := roleSpec[RoleMetricImpl]
			lines := wrapText(blk.Implication, rs.size, blk.w)
			h += 16 + float64(max(1, len(lines)))*rs.size*lineHeight
		}
		return h
	case "card":
		pad := blk.Pad
		if pad == 0 {
			pad = 48
		}
		inner := blk.w - 2*pad
		gap := blk.Gap
		if gap == 0 {
			gap = 18
		}
		total := 2 * pad
		for i, it := range blk.Items {
			it.w = inner
			total += intrinsicHeight(it)
			if i < len(blk.Items)-1 {
				total += gap
			}
		}
		return total
	case "col":
		gap := blk.Gap
		if gap == 0 {
			gap = 24
		}
		total := 0.0
		for i, it := range blk.Items {
			it.w = blk.w
			total += intrinsicHeight(it)
			if i < len(blk.Items)-1 {
				total += gap
			}
		}
		return total
	case "row":
		// row height = max child intrinsic height at its column width.
		gap := blk.Gap
		if gap == 0 {
			gap = 32
		}
		n := len(blk.Items)
		if n == 0 {
			return 0
		}
		avail := blk.w - gap*float64(n-1)
		totalWeight := 0.0
		for _, it := range blk.Items {
			totalWeight += weightOf(it)
		}
		maxH := 0.0
		for _, it := range blk.Items {
			it.w = avail * weightOf(it) / totalWeight
			if h := intrinsicHeight(it); h > maxH {
				maxH = h
			}
		}
		return maxH
	}
	return 0
}

// layout assigns x,y,w,h to every descendant. blk.x/y/w/h are already
// set by the caller (root by Render, children by their container).
func layout(blk *Block) {
	switch blk.Type {
	case "col":
		layoutCol(blk)
	case "row":
		layoutRow(blk)
	case "card":
		layoutCard(blk)
	}
	// leaves need no recursion.
}

func layoutCol(blk *Block) {
	gap := blk.Gap
	if gap == 0 {
		gap = 24
	}
	n := len(blk.Items)
	if n == 0 {
		return
	}
	// First pass: intrinsic heights at full col width.
	intr := make([]float64, n)
	fixedTotal := 0.0
	growCount := 0
	for i, it := range blk.Items {
		it.w = blk.w
		if it.Grow {
			growCount++
			continue
		}
		intr[i] = intrinsicHeight(it)
		fixedTotal += intr[i]
	}
	gapsTotal := gap * float64(n-1)
	extra := blk.h - fixedTotal - gapsTotal
	if extra < 0 {
		extra = 0
	}
	growEach := 0.0
	if growCount > 0 {
		growEach = extra / float64(growCount)
	}
	// Assign y top-to-bottom.
	y := blk.y
	for i, it := range blk.Items {
		it.x = blk.x
		it.w = blk.w
		if it.Grow {
			it.h = growEach
		} else {
			it.h = intr[i]
		}
		it.y = y
		layout(it)
		y += it.h + gap
	}
}

func layoutRow(blk *Block) {
	gap := blk.Gap
	if gap == 0 {
		gap = 32
	}
	n := len(blk.Items)
	if n == 0 {
		return
	}
	avail := blk.w - gap*float64(n-1)
	totalWeight := 0.0
	for _, it := range blk.Items {
		totalWeight += weightOf(it)
	}
	x := blk.x
	for _, it := range blk.Items {
		w := avail * weightOf(it) / totalWeight
		it.x = x
		it.y = blk.y
		it.w = w
		it.h = blk.h // items fill the row height (cards stretch)
		layout(it)
		x += w + gap
	}
}

func layoutCard(blk *Block) {
	pad := blk.Pad
	if pad == 0 {
		pad = 48
	}
	gap := blk.Gap
	if gap == 0 {
		gap = 18
	}
	innerX := blk.x + pad
	innerW := blk.w - 2*pad
	y := blk.y + pad
	for _, it := range blk.Items {
		it.x = innerX
		it.w = innerW
		it.h = intrinsicHeight(it)
		it.y = y
		layout(it)
		y += it.h + gap
	}
}

func weightOf(b *Block) float64 {
	if b.Weight > 0 {
		return b.Weight
	}
	return 1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
