package svglayout

import (
	"fmt"
	"strconv"
	"strings"
)

// emit walks the laid-out tree and writes SVG for each block. All
// coordinates are already computed by layout(); emit just renders.
func emit(b *strings.Builder, blk *Block, th Theme) {
	switch blk.Type {
	case "card":
		fill := resolveSurface(blk.Fill, th)
		// Rounded card panel. rx scales gently with size.
		if fill != "" {
			fmt.Fprintf(b, `<rect x="%.0f" y="%.0f" width="%.0f" height="%.0f" rx="16" fill="%s"/>`,
				blk.x, blk.y, blk.w, blk.h, fill)
		}
		for _, it := range blk.Items {
			emit(b, it, th)
		}
	case "row", "col":
		for _, it := range blk.Items {
			emit(b, it, th)
		}
	case "rule":
		// thin accent rule spanning the block width
		fmt.Fprintf(b, `<rect x="%.0f" y="%.0f" width="%.0f" height="3" fill="%s"/>`,
			blk.x, blk.y, ruleWidth(blk.w), th.Accent)
	case "text":
		emitText(b, blk.Text, blk.Role, blk.x, blk.y, blk.w, blk.Align, th)
	case "numeral":
		emitText(b, blk.Text, RoleNumeral, blk.x, blk.y, blk.w, blk.Align, th)
	case "metric":
		emitMetric(b, blk, th)
	}
}

// emitText renders a (wrapped) text block. y is the block TOP; each
// line's baseline sits at lineTop + fs*0.82 (cap-height approximation).
func emitText(b *strings.Builder, text string, role Role, x, y, w float64, align string, th Theme) {
	rs := roleSpec[role]
	if text == "" {
		return
	}
	disp := text
	if rs.upper {
		disp = strings.ToUpper(disp)
	}
	lines := wrapText(disp, rs.size, w)
	anchor, ax := anchorFor(align, x, w)
	col := colorFor(rs.color, th)
	fam := familyFor(rs.family, th)
	ly := y
	for _, ln := range lines {
		baseline := ly + rs.size*0.82
		writeText(b, ax, baseline, ln, fam, rs.size, rs.weight, col, anchor, rs.tracking)
		ly += rs.size * lineHeight
	}
}

// emitMetric renders the value+reference+implication stack with the same
// geometry intrinsicHeight reserved, guaranteeing the parts never collide.
func emitMetric(b *strings.Builder, blk *Block, th Theme) {
	anchor, ax := anchorFor(blk.Align, blk.x, blk.w)
	y := blk.y
	if blk.Reference != "" {
		rs := roleSpec[RoleMetricRef]
		writeText(b, ax, y+rs.size*0.82, ref(blk.Reference, rs), familyFor(rs.family, th), rs.size, rs.weight, colorFor(rs.color, th), anchor, rs.tracking)
		y += rs.size*lineHeight + 14
	}
	{
		rs := roleSpec[RoleMetricValue]
		// Shrink-to-fit: a metric value is rendered on ONE line at display
		// size (numbers shouldn't wrap). But the LLM sometimes puts a long
		// CJK phrase here ("消费级显卡"), which would overflow the column and
		// collide with the neighbour. Scale the font down so it always fits
		// the block width — deterministic, so overlap is impossible.
		fs := fitSize(blk.Value, rs.size, blk.w, 40)
		writeText(b, ax, y+fs*0.82, blk.Value, familyFor(rs.family, th), fs, rs.weight, colorFor(rs.color, th), anchor, rs.tracking)
		y += rs.size * lineHeight // reserve the full slot height (fs ≤ rs.size)
	}
	if blk.Implication != "" {
		y += 16
		rs := roleSpec[RoleMetricImpl]
		for _, ln := range wrapText(blk.Implication, rs.size, blk.w) {
			writeText(b, ax, y+rs.size*0.82, ln, familyFor(rs.family, th), rs.size, rs.weight, colorFor(rs.color, th), anchor, rs.tracking)
			y += rs.size * lineHeight
		}
	}
}

// fitSize returns the largest font size ≤ base at which text fits within
// maxW on a single line, never going below floor. Guarantees no
// horizontal overflow for single-line display text (metric values).
func fitSize(text string, base, maxW, floor float64) float64 {
	if text == "" || maxW <= 0 {
		return base
	}
	w := textWidth(text, base)
	if w <= maxW {
		return base
	}
	fs := base * maxW / w
	if fs < floor {
		fs = floor
	}
	return fs
}

func ref(s string, rs fontSpec) string {
	if rs.upper {
		return strings.ToUpper(s)
	}
	return s
}

func writeText(b *strings.Builder, x, baseline float64, text, family string, size float64, weight int, color, anchor string, tracking float64) {
	fmt.Fprintf(b, `<text x="%.0f" y="%.0f" font-family="%s" font-size="%.0f" font-weight="%d" fill="%s" text-anchor="%s"`,
		x, baseline, family, size, weight, color, anchor)
	if tracking > 0 {
		fmt.Fprintf(b, ` letter-spacing="%.0f"`, tracking)
	}
	b.WriteString(">")
	b.WriteString(xmlEscape(text))
	b.WriteString("</text>")
}

func anchorFor(align string, x, w float64) (anchor string, ax float64) {
	switch align {
	case "center":
		return "middle", x + w/2
	case "right":
		return "end", x + w
	default:
		return "start", x
	}
}

func colorFor(c colKind, th Theme) string {
	switch c {
	case colMuted:
		return th.FGMuted
	case colAccent:
		return th.Accent
	default:
		return th.FG
	}
}

func familyFor(f famKind, th Theme) string {
	switch f {
	case famDisplay:
		return th.FontDisp
	case famMono:
		return th.FontMono
	default:
		return th.FontBody
	}
}

// ruleWidth caps a decorative rule so it reads as an accent, not a full
// divider: ~40% of its block width, min 120, max 360.
func ruleWidth(w float64) float64 {
	rw := w * 0.4
	if rw < 120 {
		rw = 120
	}
	if rw > 360 {
		rw = 360
	}
	if rw > w {
		rw = w
	}
	return rw
}

// resolveSurface maps a card Fill spec to a hex. "surface" → a subtle
// panel blended 8% toward FG from BG (a faint lifted tint, no alpha
// needed so it converts to a native solid-fill shape). A literal #hex
// passes through. "" / "none" → no panel.
func resolveSurface(fill string, th Theme) string {
	switch {
	case fill == "" || fill == "none":
		return ""
	case fill == "surface":
		return blendHex(th.BG, th.FG, 0.08)
	case strings.HasPrefix(fill, "#"):
		return fill
	default:
		return ""
	}
}

// blendHex linearly mixes a toward b by t∈[0,1]. Both #RRGGBB.
func blendHex(a, bb string, t float64) string {
	ar, ag, ab, ok1 := hexRGB(a)
	br, bg, bbl, ok2 := hexRGB(bb)
	if !ok1 || !ok2 {
		return a
	}
	mix := func(x, y int) int { return int(float64(x) + (float64(y)-float64(x))*t) }
	return fmt.Sprintf("#%02X%02X%02X", mix(ar, br), mix(ag, bg), mix(ab, bbl))
}

func hexRGB(s string) (r, g, b int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	ri, e1 := strconv.ParseInt(s[0:2], 16, 0)
	gi, e2 := strconv.ParseInt(s[2:4], 16, 0)
	bi, e3 := strconv.ParseInt(s[4:6], 16, 0)
	if e1 != nil || e2 != nil || e3 != nil {
		return 0, 0, 0, false
	}
	return int(ri), int(gi), int(bi), true
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
