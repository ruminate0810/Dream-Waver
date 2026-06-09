package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// StyleSVGElement applies a DETERMINISTIC per-element visual restyle to ONE
// <text> node of a bespoke-SVG slide (mode=svg decks): change its colour
// (fill), font-size, and/or font-weight. NO LLM call — it locates the element
// by exact text content + occurrence index and rewrites attributes in place,
// then re-renders just that slide.
//
// This is what powers the live preview's "样式" tab: the user clicks a word and
// picks a colour / size / weight. edit_svg_slide does a full LLM re-author of
// the whole slide (slow, non-deterministic); style_slide is HTML-template-only
// and a no-op on SVG. This tool is the surgical, zero-LLM path for the common
// "make this one bit red / bigger / bold" edit.
//
// Element identity: authored SVG <text> nodes carry no stable id (svg.md
// mandates plain <text>/<tspan> + inline styles), so we match on the exact
// rendered text + a 1-based occurrence index (the Kth same-text element, sent
// by the iframe bridge) to disambiguate repeats. Ambiguous matches error rather
// than restyle the wrong node.
type StyleSVGElement struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*StyleSVGElement) Name() string { return "style_svg_element" }

func (*StyleSVGElement) Description() string {
	return "Deterministically restyle ONE text element of a bespoke-SVG slide (layout=svg): set its colour (fill), font_size, and/or font_weight. NO re-authoring — locate the element by its exact text + occurrence and rewrite attributes, then re-render just that slide. Use for surgical 'make this word red / bigger / bold' edits; use edit_svg_slide for layout/content rewrites."
}

func (*StyleSVGElement) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index", "match_text"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based slide to edit."},
			"match_text":  {"type": "string", "description": "The exact text content of the element to restyle (as it reads on the slide)."},
			"occurrence":  {"type": "integer", "minimum": 1, "description": "1-based index when several elements share the same text (default 1 = first)."},
			"fill":        {"type": "string", "description": "New colour as #RRGGBB. Omit to leave the colour unchanged."},
			"font_size":   {"type": "number", "description": "New font size in SVG user units (px on the 1920x1080 canvas). Omit to leave unchanged."},
			"font_weight": {"type": "string", "description": "New weight: normal | bold | 100..900. Omit to leave unchanged."}
		}
	}`)
}

type styleSVGElementArgs struct {
	SlideIndex int     `json:"slide_index"`
	MatchText  string  `json:"match_text"`
	Occurrence int     `json:"occurrence"`
	Fill       string  `json:"fill"`
	FontSize   float64 `json:"font_size"`
	FontWeight string  `json:"font_weight"`
}

// hexColorRe is shared with apply_brand.go (same package) — reused here.
var (
	fontWeightRe  = regexp.MustCompile(`^(normal|bold|[1-9]00)$`)
	tspanOpenRe   = regexp.MustCompile(`<tspan\b[^>]*>`)
	stripTagsRe   = regexp.MustCompile(`<[^>]*>`)
	collapseWSRe  = regexp.MustCompile(`\s+`)
	openTagEndErr = "could not parse the element's opening tag"
)

func (t *StyleSVGElement) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a styleSVGElementArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if a.Occurrence <= 0 {
		a.Occurrence = 1
	}
	if strings.TrimSpace(a.MatchText) == "" {
		return schema.ToolResult{Error: "match_text is required"}, nil
	}

	// Validate the requested style changes; collect the ones actually set.
	setFill := strings.TrimSpace(a.Fill) != ""
	setSize := a.FontSize > 0
	setWeight := strings.TrimSpace(a.FontWeight) != ""
	if !setFill && !setSize && !setWeight {
		return schema.ToolResult{Error: "nothing to change — provide fill, font_size, and/or font_weight"}, nil
	}
	if setFill && !hexColorRe.MatchString(strings.TrimSpace(a.Fill)) {
		return schema.ToolResult{Error: "fill must be #RRGGBB"}, nil
	}
	if setSize && (a.FontSize < 8 || a.FontSize > 400) {
		return schema.ToolResult{Error: "font_size out of range (8–400)"}, nil
	}
	if setWeight && !fontWeightRe.MatchString(strings.TrimSpace(a.FontWeight)) {
		return schema.ToolResult{Error: "font_weight must be normal | bold | 100..900"}, nil
	}

	idx := a.SlideIndex - 1
	deck, count := t.State.Snapshot()
	if idx < 0 || idx >= count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index out of range (have %d slides)", count)}, nil
	}
	cur := deck.Slides[idx]
	if cur.Layout != schema.LayoutSVG || cur.Data.SVG == "" {
		return schema.ToolResult{Error: fmt.Sprintf("slide %d is not an SVG slide", a.SlideIndex)}, nil
	}

	newSVG, matchCount, err := restyleTextElement(cur.Data.SVG, a.MatchText, a.Occurrence, styleEdit{
		fill: a.Fill, setFill: setFill,
		fontSize: a.FontSize, setSize: setSize,
		fontWeight: a.FontWeight, setWeight: setWeight,
	})
	if err != nil {
		if matchCount == 0 {
			return schema.ToolResult{Error: fmt.Sprintf("no text element matching %q on slide %d", a.MatchText, a.SlideIndex)}, nil
		}
		return schema.ToolResult{Error: err.Error()}, nil
	}

	if err := t.State.UpdateSlide(idx, func(s *schema.Slide) {
		s.Layout = schema.LayoutSVG
		s.Data.SVG = newSVG
	}); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	t.State.MarkDirty(idx)

	updatedDeck, _ := t.State.Snapshot()
	pptxPath, rerr := t.Renderer.RenderIncremental(ctx, *updatedDeck, []int{idx})
	if rerr != nil {
		return schema.ToolResult{Error: "rerender: " + rerr.Error()}, nil
	}

	out, _ := json.Marshal(map[string]any{
		"slide_index": a.SlideIndex,
		"pptx_path":   pptxPath,
		"message":     fmt.Sprintf("Styled %q on slide %d.", a.MatchText, a.SlideIndex),
	})
	return schema.ToolResult{Output: string(out)}, nil
}

type styleEdit struct {
	fill       string
	setFill    bool
	fontSize   float64
	setSize    bool
	fontWeight string
	setWeight  bool
}

// restyleTextElement finds the `occurrence`-th <text>…</text> whose rendered
// text equals `want` and rewrites its opening-tag attributes per `e`. It also
// strips the same attributes from that element's child <tspan>s so the new
// text-level value actually wins (a tspan's own fill/size/weight would override
// the parent otherwise). Returns the new SVG, how many elements matched the
// text (for diagnostics), and an error when the occurrence isn't found.
func restyleTextElement(svg, want string, occurrence int, e styleEdit) (string, int, error) {
	wantN := normalizeText(want)
	seen := 0
	i := 0
	for {
		os := strings.Index(svg[i:], "<text")
		if os < 0 {
			break
		}
		os += i
		// Reject <textPath>/<textArea> — the char after "<text" must end the
		// tag name (whitespace, '>', or '/').
		if os+5 >= len(svg) {
			break
		}
		switch svg[os+5] {
		case ' ', '\t', '\n', '\r', '>', '/':
		default:
			i = os + 5
			continue
		}
		gt := strings.IndexByte(svg[os:], '>')
		if gt < 0 {
			return "", seen, fmt.Errorf("%s", openTagEndErr)
		}
		openEnd := os + gt // index of '>'
		// Self-closing <text/> carries no content — skip.
		if svg[openEnd-1] == '/' {
			i = openEnd + 1
			continue
		}
		cs := strings.Index(svg[openEnd:], "</text>")
		if cs < 0 {
			return "", seen, fmt.Errorf("unterminated <text> element")
		}
		closeStart := openEnd + cs
		content := svg[openEnd+1 : closeStart]
		if normalizeText(stripInnerTags(content)) == wantN {
			seen++
			if seen == occurrence {
				openTag := svg[os : openEnd+1] // includes '>'
				newOpen := openTag
				newContent := content
				if e.setFill {
					newOpen = upsertAttr(newOpen, "fill", e.fill)
					newContent = stripTspanAttr(newContent, "fill")
				}
				if e.setSize {
					newOpen = upsertAttr(newOpen, "font-size", trimNum(e.fontSize))
					newContent = stripTspanAttr(newContent, "font-size")
				}
				if e.setWeight {
					newOpen = upsertAttr(newOpen, "font-weight", e.fontWeight)
					newContent = stripTspanAttr(newContent, "font-weight")
				}
				return svg[:os] + newOpen + newContent + svg[closeStart:], seen, nil
			}
		}
		i = closeStart + len("</text>")
	}
	if seen == 0 {
		return "", 0, fmt.Errorf("no matching text element")
	}
	return "", seen, fmt.Errorf("occurrence %d out of range (only %d element(s) match %q)", occurrence, seen, want)
}

// upsertAttr replaces name="…" inside an opening tag (which includes its
// trailing '>'), or inserts it before the close when absent.
func upsertAttr(openTag, name, val string) string {
	re := regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `="[^"]*"`)
	repl := ` ` + name + `="` + val + `"`
	if re.MatchString(openTag) {
		return re.ReplaceAllString(openTag, repl)
	}
	if strings.HasSuffix(openTag, "/>") {
		return openTag[:len(openTag)-2] + repl + "/>"
	}
	return openTag[:len(openTag)-1] + repl + ">"
}

// stripTspanAttr removes name="…" from every <tspan …> opening tag inside a
// <text> body, so the parent <text>'s freshly-set value cascades to the lines.
func stripTspanAttr(content, name string) string {
	attrRe := regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `="[^"]*"`)
	return tspanOpenRe.ReplaceAllStringFunc(content, func(tag string) string {
		return attrRe.ReplaceAllString(tag, "")
	})
}

func stripInnerTags(s string) string { return stripTagsRe.ReplaceAllString(s, "") }

func normalizeText(s string) string {
	return strings.TrimSpace(collapseWSRe.ReplaceAllString(s, " "))
}

func trimNum(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
