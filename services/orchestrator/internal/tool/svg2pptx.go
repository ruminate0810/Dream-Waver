package tool

import (
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/Esword618/unioffice/presentation"
	"github.com/Esword618/unioffice/schema/soo/dml"
	pml "github.com/Esword618/unioffice/schema/soo/pml"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// svgEMUPerPx is EMU per canvas-px for native DrawingML geometry (Off /
// Ext / line width are raw EMU int64s). NOTE: the package's `emuPerPx`
// constant is misnamed — it's actually POINTS-per-px (~0.5), used with
// measurement.Distance for the text-box overlay. Native shapes need true
// EMU: 1 inch = 914400 EMU, canvas is slideWidthInches across
// renderViewportW px → ~6350 EMU/px.
const svgEMUPerPx = 914400.0 * slideWidthInches / float64(renderViewportW)

// Sprint SV-3b — SVG → native editable PowerPoint shapes.
//
// The SV-1 path rasterizes an LLM-authored <svg> to a PNG background and
// overlays its <text> as editable boxes (SV-3a fixed their colour). This
// file upgrades the DECORATION layer: rect / circle / ellipse / line are
// parsed straight out of the SVG and emitted as native DrawingML
// autoshapes, so they're selectable / movable / recolourable in
// PowerPoint instead of being welded into a flat image.
//
// Conservative by design — parseSVGShapes returns ok=false the moment it
// meets anything it can't represent faithfully (path, polygon, gradient,
// transform, embedded image, opacity). The caller then keeps the PNG
// background for that slide (full visual fidelity, just not vector-
// editable). Real generated decks are ~100% rect/circle/line + text, so
// the native path triggers on the overwhelming majority; the fallback is
// the safety net, never a regression.

// svgShape is one parsed primitive in canvas (1920×1080) px coordinates.
type svgShape struct {
	kind    string // "rect" | "roundrect" | "ellipse" | "line"
	x, y    float64
	w, h    float64
	rx      float64 // corner radius for roundrect (px)
	fill    string  // #RRGGBB or "" (none)
	stroke  string  // #RRGGBB or "" (none)
	strokeW float64 // px
}

// parseSVGShapes walks the SVG and returns native-convertible decoration
// shapes. Sprint SV-3b.1 — "no PNG, always SVG": this is now BEST-EFFORT
// rather than all-or-nothing. Any element it can't represent faithfully
// (path/polygon/gradient/transform/image/…) is SKIPPED, and the rest of
// the slide is still emitted as native shapes — we never fall back to a
// flat PNG background for an svg slide. The svg.md prompt forbids those
// unsupported features at the source, so in practice nothing is skipped;
// this skip path is just a safety net that keeps the slide vector-native
// instead of rasterising the whole thing.
//
// ok reports only whether we got a usable parse (≥1 shape) — the caller
// keeps the PNG ONLY for the degenerate empty-parse case (malformed XML)
// to avoid a blank slide. <text>/<tspan> are skipped here (handled by
// the editable text-box overlay).
//
// tok resolves the rare var(--x) colour (the LLM normally emits concrete
// hex, but we defend against either).
// isRichSVG reports whether the SVG uses features the native-shape
// converter cannot represent faithfully (gradients, filters, custom
// paths/polygons, patterns, embedded images, icon <use>, transforms).
// Sprint PM-5: such slides take the double-layer route instead (a
// text-hidden BgPNG of the full rich decoration + editable text-box
// overlay), so the .pptx matches the preview pixel-for-pixel. The
// simpler SV-5 deterministic SVG (pure rect/circle/line/text) returns
// false here and still converts to fully vector-editable native shapes.
func isRichSVG(svg string) bool {
	for _, marker := range []string{
		"Gradient", "url(#", "<filter", "<path", "<polygon", "<polyline",
		"<pattern", "<image", "<use", "transform=", "clip-path",
	} {
		if strings.Contains(svg, marker) {
			return true
		}
	}
	return false
}

func parseSVGShapes(svg string, tok themetokens.Tokens) (shapes []svgShape, ok bool) {
	dec := xml.NewDecoder(strings.NewReader(svg))
	for {
		t, err := dec.Token()
		if err != nil {
			break // EOF or parse end
		}
		se, isStart := t.(xml.StartElement)
		if !isStart {
			continue
		}
		name := se.Name.Local
		attr := attrMap(se)

		// A transform we don't bake would misposition the element — skip
		// just this element (still emit everything else natively).
		if tr := strings.TrimSpace(attr["transform"]); tr != "" {
			continue
		}
		// Gradient/pattern paint can't be a solid fill; resolveColor maps
		// url(...) → "" (no fill) so the shape still renders, just flat.

		switch name {
		case "svg", "g", "title", "desc", "tspan", "text":
			// containers / text — descend (text handled elsewhere). A <g>
			// with no transform is transparent to us; its children are
			// visited by the streaming decoder anyway.
			continue
		case "rect":
			s := svgShape{
				kind:    "rect",
				x:       num(attr["x"]),
				y:       num(attr["y"]),
				w:       num(attr["width"]),
				h:       num(attr["height"]),
				rx:      num(attr["rx"]),
				fill:    resolveColor(attr["fill"], tok),
				stroke:  resolveColor(attr["stroke"], tok),
				strokeW: numDefault(attr["stroke-width"], 0),
			}
			if s.rx > 0 {
				s.kind = "roundrect"
			}
			if s.w > 0 && s.h > 0 {
				shapes = append(shapes, s)
			}
		case "circle":
			cx, cy, rr := num(attr["cx"]), num(attr["cy"]), num(attr["r"])
			if rr > 0 {
				shapes = append(shapes, svgShape{
					kind: "ellipse", x: cx - rr, y: cy - rr, w: 2 * rr, h: 2 * rr,
					fill: resolveColor(attr["fill"], tok), stroke: resolveColor(attr["stroke"], tok),
					strokeW: numDefault(attr["stroke-width"], 0),
				})
			}
		case "ellipse":
			cx, cy := num(attr["cx"]), num(attr["cy"])
			rx, ry := num(attr["rx"]), num(attr["ry"])
			if rx > 0 && ry > 0 {
				shapes = append(shapes, svgShape{
					kind: "ellipse", x: cx - rx, y: cy - ry, w: 2 * rx, h: 2 * ry,
					fill: resolveColor(attr["fill"], tok), stroke: resolveColor(attr["stroke"], tok),
					strokeW: numDefault(attr["stroke-width"], 0),
				})
			}
		case "line":
			x1, y1 := num(attr["x1"]), num(attr["y1"])
			x2, y2 := num(attr["x2"]), num(attr["y2"])
			shapes = append(shapes, svgShape{
				kind: "line", x: x1, y: y1, w: x2 - x1, h: y2 - y1,
				stroke: resolveColor(attr["stroke"], tok), strokeW: numDefault(attr["stroke-width"], 1),
			})
		default:
			// path, polygon, polyline, image, use, defs, gradients,
			// filters, clipPath, … — not representable as a native
			// autoshape. Skip just this element (no whole-slide PNG
			// fallback). The prompt forbids these, so this is rare.
			continue
		}
	}
	return shapes, len(shapes) > 0
}

// emitSVGShapes appends each parsed shape to the slide's shape tree as a
// native DrawingML autoshape. Must be called BEFORE the text boxes are
// added so text renders on top (z-order = insertion order).
func emitSVGShapes(slide presentation.Slide, shapes []svgShape) {
	for _, s := range shapes {
		c := pml.NewCT_GroupShapeChoice()
		slide.X().CSld.SpTree.Choice = append(slide.X().CSld.SpTree.Choice, c)
		sp := pml.NewCT_Shape()
		c.Sp = append(c.Sp, sp)
		sp.SpPr = dml.NewCT_ShapeProperties()

		// Geometry preset.
		sp.SpPr.PrstGeom = dml.NewCT_PresetGeometry2D()
		switch s.kind {
		case "ellipse":
			sp.SpPr.PrstGeom.PrstAttr = dml.ST_ShapeTypeEllipse
		case "line":
			sp.SpPr.PrstGeom.PrstAttr = dml.ST_ShapeTypeLine
		case "roundrect":
			sp.SpPr.PrstGeom.PrstAttr = dml.ST_ShapeTypeRoundRect
		default:
			sp.SpPr.PrstGeom.PrstAttr = dml.ST_ShapeTypeRect
		}

		// Transform: offset + extent in EMU. A line stores its delta in
		// w/h (may be negative) — normalise to a positive box + flip flags.
		offX, offY := s.x, s.y
		extW, extH := s.w, s.h
		var flipH, flipV bool
		if extW < 0 {
			offX += extW
			extW = -extW
			flipH = true
		}
		if extH < 0 {
			offY += extH
			extH = -extH
			flipV = true
		}
		sp.SpPr.Xfrm = dml.NewCT_Transform2D()
		sp.SpPr.Xfrm.Off = dml.NewCT_Point2D()
		ox, oy := int64(offX*svgEMUPerPx), int64(offY*svgEMUPerPx)
		sp.SpPr.Xfrm.Off.XAttr = dml.ST_Coordinate{ST_CoordinateUnqualified: &ox}
		sp.SpPr.Xfrm.Off.YAttr = dml.ST_Coordinate{ST_CoordinateUnqualified: &oy}
		sp.SpPr.Xfrm.Ext = dml.NewCT_PositiveSize2D()
		sp.SpPr.Xfrm.Ext.CxAttr = int64(extW * svgEMUPerPx)
		sp.SpPr.Xfrm.Ext.CyAttr = int64(extH * svgEMUPerPx)
		if flipH {
			sp.SpPr.Xfrm.FlipHAttr = boolPtr(true)
		}
		if flipV {
			sp.SpPr.Xfrm.FlipVAttr = boolPtr(true)
		}

		// Fill.
		if hex := normHex(s.fill); hex != "" {
			sp.SpPr.SolidFill = solidHex(hex)
		} else {
			sp.SpPr.NoFill = dml.NewCT_NoFillProperties()
		}

		// Stroke. Lines always need one (no fill); shapes only if set.
		if hex := normHex(s.stroke); hex != "" {
			sp.SpPr.Ln = dml.NewCT_LineProperties()
			w := s.strokeW
			if w <= 0 {
				w = 1
			}
			wEMU := int32(w * svgEMUPerPx)
			sp.SpPr.Ln.WAttr = &wEMU
			sp.SpPr.Ln.SolidFill = solidHex(hex)
		} else if s.kind == "line" {
			// A line with no explicit stroke still needs to be visible.
			sp.SpPr.Ln = dml.NewCT_LineProperties()
			onePx := 1.0 * svgEMUPerPx // via var so the float const truncates at runtime
			wEMU := int32(onePx)
			sp.SpPr.Ln.WAttr = &wEMU
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────

func attrMap(se xml.StartElement) map[string]string {
	m := make(map[string]string, len(se.Attr))
	for _, a := range se.Attr {
		m[a.Name.Local] = a.Value
	}
	return m
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, "px")), 64)
	return v
}

func numDefault(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	return num(s)
}

// resolveColor maps an SVG paint value to a #RRGGBB hex, or "" for
// none/transparent. Handles concrete hex, var(--token), rgb(), and the
// "none"/"transparent" keywords.
func resolveColor(raw string, tok themetokens.Tokens) string {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "" || raw == "none" || raw == "transparent":
		return ""
	case strings.HasPrefix(raw, "#"):
		return raw
	case strings.HasPrefix(raw, "var("):
		inner := strings.TrimSuffix(strings.TrimPrefix(raw, "var("), ")")
		inner = strings.TrimSpace(strings.Split(inner, ",")[0])
		switch inner {
		case "--bg":
			return tok.BG
		case "--fg":
			return tok.FG
		case "--fg-muted":
			return tok.FGMuted
		case "--accent", "--accent-2":
			return tok.Accent
		}
		return ""
	case strings.HasPrefix(raw, "rgb"):
		return rgbToHex(raw)
	default:
		return "" // named colors etc. — skip (rare in our output)
	}
}

func rgbToHex(s string) string {
	lp := strings.IndexByte(s, '(')
	rp := strings.IndexByte(s, ')')
	if lp < 0 || rp < 0 || rp < lp {
		return ""
	}
	parts := strings.Split(s[lp+1:rp], ",")
	if len(parts) < 3 {
		return ""
	}
	r, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	g, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	b, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
	return "#" + hex2(r) + hex2(g) + hex2(b)
}

func hex2(v int) string {
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	s := strconv.FormatInt(int64(v), 16)
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// normHex strips the leading # and uppercases to the 6-char form
// DrawingML's srgbClr wants; returns "" for invalid input.
func normHex(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 { // expand shorthand #abc → aabbcc
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return ""
	}
	return strings.ToUpper(s)
}

func solidHex(hex string) *dml.CT_SolidColorFillProperties {
	f := dml.NewCT_SolidColorFillProperties()
	f.SrgbClr = dml.NewCT_SRgbColor()
	f.SrgbClr.ValAttr = hex
	return f
}

func boolPtr(b bool) *bool { return &b }
