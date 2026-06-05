package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Esword618/unioffice/color"
	"github.com/Esword618/unioffice/common"
	"github.com/Esword618/unioffice/measurement"
	"github.com/Esword618/unioffice/presentation"
	"github.com/Esword618/unioffice/schema/soo/dml"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// SlideRender is the workhorse that turns structured slide specs into a real,
// downloadable .pptx file. It performs three jobs in sequence:
//
//  1. Apply each slide's data to its HTML+Tailwind template.
//  2. Drive headless Chromium (chromedp) to render each HTML page to a
//     1920×1080 PNG at 2× DPI.
//  3. Assemble a PPTX where every slide is the PNG dropped at (0,0) covering
//     the whole canvas (Phase-1 "background-image mode"). Phase-2 will inject
//     editable text boxes at the bounding boxes scraped from the DOM.
//
// SlideRender is exposed as a Tool so the LLM can call it directly in
// advanced agent mode, but the Slides skill normally drives it programmatically.
type SlideRender struct {
	TemplateDir string        // packages/slide-templates relative to repo root
	OutDir      string        // where to write the .pptx output
	Emitter     event.Emitter // optional progress events; session id comes from ctx

	once sync.Once
	// loaded and loadErr are the cached result of loadTemplates. Both
	// are written exactly once inside `once.Do`. loadErr lives on the
	// receiver (not as a closure-local) so subsequent calls see the
	// same error instead of seeing nil — without this, a first call
	// that fails before `loaded = root` would leave loaded == nil but
	// later calls return nil error, and the downstream Lookup would
	// nil-deref.
	loaded  *template.Template
	loadErr error
}

// Phase-1 default slide canvas: 16:9 at 1920×1080.
const (
	slideWidthInches  = 13.333 // 16:9 PowerPoint default
	slideHeightInches = 7.5
	renderViewportW   = 1920
	renderViewportH   = 1080
	devicePixelRatio  = 2.0
)

func (*SlideRender) Name() string { return "slide_render" }

func (*SlideRender) Description() string {
	return `Render a structured slide deck (title + per-slide template + data) into a real .pptx file. Returns a file path that can be uploaded or downloaded.`
}

func (*SlideRender) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["title", "slides"],
		"properties": {
			"title": {"type": "string"},
			"theme": {"type": "string", "description": "Optional global theme name"},
			"slides": {
				"type": "array",
				"minItems": 1,
				"items": {
					"type": "object",
					"required": ["template", "data"],
					"properties": {
						"template": {"type": "string", "description": "Template name under packages/slide-templates"},
						"layout":   {"type": "string", "description": "Optional layout variant within the template"},
						"data":     {"type": "object", "description": "Key-value pairs filled into the template"}
					}
				}
			}
		}
	}`)
}

// Execute is the LLM-facing entry. It just decodes JSON into a typed Deck
// and delegates to RenderDeck so the tool surface and the in-process surface
// share the exact same code path.
func (r *SlideRender) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var deck schema.Deck
	if err := json.Unmarshal(args, &deck); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	path, err := r.RenderDeck(ctx, deck)
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	return schema.ToolResult{Output: path}, nil
}

// RenderDeck is the typed in-process entry. The slides skill calls this
// directly so it can pass a Deck value instead of serializing/deserializing.
func (r *SlideRender) RenderDeck(ctx context.Context, deck schema.Deck) (string, error) {
	if len(deck.Slides) == 0 {
		return "", fmt.Errorf("deck must have at least one slide")
	}

	r.emit(ctx, event.NewRenderStart(deck.Title, len(deck.Slides)))

	assets, err := r.renderAllSlides(ctx, deck)
	if err != nil {
		r.emit(ctx, event.NewError("render", err))
		return "", err
	}

	outPath, err := r.assemblePPTX(ctx, deck, assets)
	if err != nil {
		r.emit(ctx, event.NewError("assemble", err))
		return "", err
	}

	r.emit(ctx, event.NewRenderEnd(outPath, len(deck.Slides)))
	return outPath, nil
}

// SlideAsset is the per-slide intermediate the renderer produces and is
// the unit cached by SessionState so follow-up edits can re-render just
// the slides that changed.
//
//   - PreviewPNG : full chromedp screenshot WITH visible text. Used as
//                  the in-browser thumbnail.
//   - BgPNG      : screenshot AFTER text was hidden. Embedded in the PPTX
//                  as a background layer (text overlays on top from real
//                  PowerPoint text boxes).
//   - TextBoxes  : DOM text nodes extracted for the editable overlay.
type SlideAsset struct {
	PreviewPNG []byte
	BgPNG      []byte
	TextBoxes  []schema.TextBox
}

// slideAssets is the internal alias the per-slide loop still uses. We keep
// it for code locality but the public surface (and SessionState cache) is
// SlideAsset above.
type slideAssets = SlideAsset

// extractTextBoxesJS walks the DOM after the page has settled and returns one
// entry per leaf text node — bounding box + parent's computed style. Any node
// whose ancestor chain includes `data-no-extract` is skipped: that lets
// template authors mark corner badges and decorative glyphs as bg-only so
// they stay welded into the PNG instead of becoming editable text boxes.
const extractTextBoxesJS = `(() => {
    const rgbToHex = (rgb) => {
        const m = rgb && rgb.match(/\d+/g);
        if (!m || m.length < 3) return '#000000';
        return '#' + [m[0], m[1], m[2]].map(x => Number(x).toString(16).padStart(2, '0')).join('');
    };
    const inNoExtract = (el) => {
        for (let cur = el; cur; cur = cur.parentElement) {
            if (cur.hasAttribute && cur.hasAttribute('data-no-extract')) return true;
        }
        return false;
    };
    const SLIDE_W = 1920;
    const SAFE_MARGIN = 30;        // px from right edge we refuse to overflow
    const WIDTH_PAD_RATIO = 0.20;  // 20% horizontal buffer for font-metric drift
    const HEIGHT_PAD_RATIO = 0.10; // 10% vertical buffer for line-height drift
    const out = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    let node;
    while ((node = walker.nextNode())) {
        const text = node.textContent.replace(/\s+/g, ' ').trim();
        if (!text) continue;
        const el = node.parentElement;
        if (!el || inNoExtract(el)) continue;
        const range = document.createRange();
        range.selectNodeContents(node);
        const rect = range.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) continue;
        const cs = window.getComputedStyle(el);

        // Sprint SV-3 — SVG text paints via the fill property, NOT CSS
        // color; HTML elements use color. Detect SVG nodes and read the
        // right paint so colored SVG text (e.g. an accent-coloured
        // headline) extracts as its real colour instead of defaulting
        // to black. Likewise SVG alignment is text-anchor
        // (start/middle/end), not text-align.
        const isSVG = el.ownerSVGElement != null ||
                      el.namespaceURI === 'http://www.w3.org/2000/svg';
        let colorSrc = cs.color;
        let alignSrc = cs.textAlign || 'left';
        if (isSVG) {
            const f = cs.fill;
            if (f && f !== 'none' && f !== '') colorSrc = f;
            const anchor = cs.textAnchor || el.getAttribute('text-anchor') || 'start';
            alignSrc = anchor === 'middle' ? 'center' : (anchor === 'end' ? 'right' : 'left');
        }

        // Browser font metrics tend to be tighter than the PowerPoint
        // fallback Chinese/Latin pair, so the glyph bbox we just measured
        // is the MINIMUM the same characters need. Pad horizontally + cap
        // at the safe right margin so the text box never bleeds off the
        // slide. PPT's normAutofit (set on the Go side) will shrink the
        // font if even the padded box can't fit the text.
        let width = rect.width * (1 + WIDTH_PAD_RATIO);
        const maxRight = SLIDE_W - SAFE_MARGIN;
        if (rect.left + width > maxRight) {
            width = Math.max(rect.width, maxRight - rect.left);
        }
        const height = rect.height * (1 + HEIGHT_PAD_RATIO);

        out.push({
            text,
            left:        rect.left,
            top:         rect.top,
            width:       width,
            height:      height,
            // px → pt conversion is NOT the CSS standard 0.75 (which assumes
            // 96 DPI). Our PNG is captured at 1920px on a 13.333" PowerPoint
            // canvas, i.e. 144 DPI — at that density 1 pt = 2 px, so the
            // multiplier is 0.5. Using 0.75 makes overlay text render 1.5×
            // larger than the original CSS.
            font_size:   parseFloat(cs.fontSize) * 0.5,
            font_family: cs.fontFamily,
            color:       rgbToHex(colorSrc),
            font_weight: parseInt(cs.fontWeight, 10) || 400,
            italic:      cs.fontStyle === 'italic',
            text_align:  alignSrc,
        });
    }
    return out;
})()`

// hideAllTextJS hides every visible text node so the next screenshot captures
// the page's "chrome" (gradients, accent bars, footers, decorative shapes)
// without text. Elements marked data-no-extract are exempt — they stay
// visible so the PNG keeps them as baked-in decoration.
const hideAllTextJS = `(() => {
    const inNoExtract = (el) => {
        for (let cur = el; cur; cur = cur.parentElement) {
            if (cur.hasAttribute && cur.hasAttribute('data-no-extract')) return true;
        }
        return false;
    };
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    const targets = new Set();
    let node;
    while ((node = walker.nextNode())) {
        if (!node.textContent.trim()) continue;
        const el = node.parentElement;
        if (!el || inNoExtract(el)) continue;
        targets.add(el);
    }
    targets.forEach(el => { el.style.visibility = 'hidden'; });
})()`

// renderAllSlides renders every slide in the deck. Thin wrapper around
// renderSlidesIndices so the existing call site stays one-liner-readable.
func (r *SlideRender) renderAllSlides(ctx context.Context, deck schema.Deck) ([]slideAssets, error) {
	idx := make([]int, len(deck.Slides))
	for i := range deck.Slides {
		idx[i] = i
	}
	out, err := r.renderSlidesIndices(ctx, deck, idx)
	if err != nil {
		return nil, err
	}
	flat := make([]slideAssets, len(deck.Slides))
	for i, a := range out {
		flat[i] = a
	}
	_ = flat
	return materialize(out, len(deck.Slides)), nil
}

func materialize(m map[int]slideAssets, n int) []slideAssets {
	out := make([]slideAssets, n)
	for i := 0; i < n; i++ {
		out[i] = m[i]
	}
	return out
}

// RenderDirty re-renders only the slides whose 0-based index is in
// `dirtyIndices`, reuses the cached assets for the rest, then re-assembles
// the PPTX. Returns the updated asset slice (suitable for stashing back
// into SessionState) plus the output pptx path.
//
// `cached` may be nil — in that case every slide is treated as dirty
// (which is equivalent to RenderDeck but lets a caller skip a code path).
func (r *SlideRender) RenderDirty(
	ctx context.Context,
	deck schema.Deck,
	cached []SlideAsset,
	dirtyIndices []int,
) ([]SlideAsset, string, error) {
	if len(deck.Slides) == 0 {
		return nil, "", fmt.Errorf("deck must have at least one slide")
	}

	// Build the working asset slice starting from cached. If cached is too
	// short (e.g. a slide was added; not in MVP but defensive) we extend.
	assets := make([]SlideAsset, len(deck.Slides))
	for i := range deck.Slides {
		if i < len(cached) {
			assets[i] = cached[i]
		}
	}

	// Default to "render everything" when caller passed no dirty list and
	// no cached assets.
	if len(dirtyIndices) == 0 {
		if len(cached) == 0 {
			dirtyIndices = make([]int, len(deck.Slides))
			for i := range deck.Slides {
				dirtyIndices[i] = i
			}
		}
	}

	r.emit(ctx, event.NewRenderStart(deck.Title, len(deck.Slides)))

	if len(dirtyIndices) > 0 {
		fresh, err := r.renderSlidesIndices(ctx, deck, dirtyIndices)
		if err != nil {
			r.emit(ctx, event.NewError("render", err))
			return nil, "", err
		}
		// slides.updated is emitted from renderSlidesIndices itself, so
		// each touched index already pushed a frame-reload event by the
		// time we get here. We just need to splice the new assets in.
		for i, a := range fresh {
			assets[i] = a
		}
	}

	outPath, err := r.assemblePPTX(ctx, deck, assets)
	if err != nil {
		r.emit(ctx, event.NewError("assemble", err))
		return nil, "", err
	}
	r.emit(ctx, event.NewRenderEnd(outPath, len(deck.Slides)))
	return assets, outPath, nil
}

// renderSlidesIndices is the per-slide render loop, indexed. Each slide
// gets two chromedp passes (text-visible preview + text-hidden bg).
// Returns a map[origIndex]asset so callers can splice into a fuller list.
func (r *SlideRender) renderSlidesIndices(
	ctx context.Context,
	deck schema.Deck,
	indices []int,
) (map[int]slideAssets, error) {
	if err := r.loadTemplates(); err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}

	// One Chromium per call. For high-throughput we'd pool allocators.
	//
	// no-sandbox + disable-dev-shm-usage are container-runtime essentials:
	// inside Docker (and Fly's microVMs) Chromium can't open the sandbox
	// namespace as root, and /dev/shm is sized 64MB by default which is
	// not enough for our 1920×1080 canvas.
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(func(format string, args ...any) {
			slog.Debug("chromedp", "msg", fmt.Sprintf(format, args...))
		}),
		chromedp.WithErrorf(func(format string, args ...any) {
			slog.Warn("chromedp error", "msg", fmt.Sprintf(format, args...))
		}),
	)
	defer cancelBrowser()

	// Stage HTML to a tmp dir; file:// URLs are more reliable across multiple
	// navigations on the same chromedp target than data: URLs (which the
	// browser may dedupe or refuse the second time around).
	stagingDir, err := os.MkdirTemp("", "dw-slides-*")
	if err != nil {
		return nil, fmt.Errorf("staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	out := make(map[int]slideAssets, len(indices))
	for _, i := range indices {
		if i < 0 || i >= len(deck.Slides) {
			continue
		}
		s := deck.Slides[i]
		// folio is the audience-facing 1-based slide number; themes that
		// paint page-folio chrome (minimalist Roman / academic § N) read
		// it from `data-folio` / `data-folio-roman` attrs in layout.html.
		html, err := r.renderHTML(s, deck.Theme, i+1)
		if err != nil {
			return nil, fmt.Errorf("slide %d render html: %w", i, err)
		}
		htmlPath := filepath.Join(stagingDir, fmt.Sprintf("slide-%03d.html", i))
		if err := os.WriteFile(htmlPath, html, 0o644); err != nil {
			return nil, fmt.Errorf("slide %d write html: %w", i, err)
		}
		fileURL := "file://" + htmlPath

		// Pass 1: load page, wait for fonts (Inter + Noto Sans SC together pull
		// several Mb of woff2), then extract text boxes from the rendered DOM.
		// `document.fonts.ready` is the spec-mandated Promise; we await it via
		// chromedp's awaitPromise option so we don't proceed until the layout
		// has settled with real glyphs.
		awaitFontsReady := chromedp.Evaluate(
			`document.fonts.ready`, nil,
			func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
				return p.WithAwaitPromise(true)
			},
		)
		// Pass 1: load page, wait for fonts, capture WITH-TEXT preview, then
		// extract text boxes.
		var (
			boxes      []schema.TextBox
			previewPNG []byte
		)
		if err := chromedp.Run(browserCtx,
			chromedp.EmulateViewport(renderViewportW, renderViewportH),
			chromedp.Navigate(fileURL),
			chromedp.WaitReady("body", chromedp.ByQuery),
			awaitFontsReady,
			chromedp.Sleep(200*time.Millisecond), // last paint settle
			chromedp.ActionFunc(func(c context.Context) error {
				png, err := page.CaptureScreenshot().
					WithCaptureBeyondViewport(false).
					WithFormat(page.CaptureScreenshotFormatPng).
					Do(c)
				if err != nil {
					return err
				}
				previewPNG = png
				return nil
			}),
			chromedp.Evaluate(extractTextBoxesJS, &boxes),
		); err != nil {
			return nil, fmt.Errorf("slide %d extract boxes: %w", i, err)
		}

		// Pass 2: hide every text node so the next capture is the page's
		// decorative chrome only — gradients, header bars, accent strips —
		// which becomes the PPTX slide background. Then take the screenshot.
		var bgPNG []byte
		if err := chromedp.Run(browserCtx,
			chromedp.Evaluate(hideAllTextJS, nil),
			chromedp.Sleep(50*time.Millisecond),
			chromedp.ActionFunc(func(c context.Context) error {
				png, err := page.CaptureScreenshot().
					WithCaptureBeyondViewport(false).
					WithFormat(page.CaptureScreenshotFormatPng).
					Do(c)
				if err != nil {
					return err
				}
				bgPNG = png
				return nil
			}),
		); err != nil {
			return nil, fmt.Errorf("slide %d bg screenshot: %w", i, err)
		}
		if _, err := png.Decode(bytes.NewReader(bgPNG)); err != nil {
			return nil, fmt.Errorf("slide %d invalid png: %w", i, err)
		}
		slog.Info("rendered slide",
			"index", i, "html_bytes", len(html),
			"preview_bytes", len(previewPNG), "bg_bytes", len(bgPNG),
			"text_boxes", len(boxes),
		)
		out[i] = slideAssets{PreviewPNG: previewPNG, BgPNG: bgPNG, TextBoxes: boxes}
		// Two events fire per slide:
		//   - slides.content  → drives the chat-timeline press-ticker;
		//   - slides.updated  → tells the live-preview iframe stack to
		//     reload exactly this page. Same trigger, two consumers.
		r.emit(ctx, event.NewSlideRendered(i+1, len(bgPNG)))
		r.emit(ctx, event.NewSlideUpdated(i+1))
	}
	return out, nil
}

// templateView is what we pass into html/template. Keeping it a concrete
// struct (not a map) means the template engine catches typos at execute time
// and IDEs can find every reference to e.g. .Data.Bullets.
//
// Folio is the 1-based slide number (what the audience sees — "slide 3 of
// 12"). Themes that paint page-folio chrome (minimalist's Roman numerals,
// academic's § N marker) read it via attr() in CSS — see layout.html which
// emits both `data-folio` (arabic) and `data-folio-roman` (upper Roman).
type templateView struct {
	Layout schema.SlideLayout
	Theme  schema.Theme
	Data   schema.SlideData
	Folio  int
	// FolioRoman is precomputed because CSS attr() can't transform
	// integer → Roman numerals (CSS Level 5's typed attr() is still
	// behind a flag in Chromium). Set to upper Roman in renderHTML.
	FolioRoman string
}

// RenderSlideHTML is the public, chromedp-free path. The live-preview HTTP
// endpoint calls this to serve one slide's HTML straight out of the
// template engine — no Chromium, no PNG, no PPTX. Sub-millisecond cost,
// so the right-hand iframe stack can refresh on every keystroke.
//
// folio is the 1-based slide number the audience sees. Used by themes
// that paint page-folio chrome (minimalist's Roman numerals at bottom-
// right, academic's § N marker on the left edge).
func (r *SlideRender) RenderSlideHTML(s schema.Slide, theme schema.Theme, folio int) ([]byte, error) {
	if err := r.loadTemplates(); err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}
	return r.renderHTML(s, theme, folio)
}

// ─── PMQ C2: whole-deck PDF export ──────────────────────────────────

var (
	pdfIDDefRe  = regexp.MustCompile(`\bid="([^"]+)"`)
	pdfURLRefRe = regexp.MustCompile(`url\(#([^)]+)\)`)
	pdfHrefRe   = regexp.MustCompile(`(\bxlink:href|\bhref)="#([^"]+)"`)
)

// namespaceSVGIDs prefixes every id + same-document reference in one slide's
// SVG with a per-page token. SVG ids are document-GLOBAL, so combining many
// slides into a single PDF document would otherwise make every page reuse
// page 1's gradients/filters (url(#bg) resolves to the first match). This
// keeps each page's defs isolated without spinning up a browser tab per slide.
func namespaceSVGIDs(svg string, page int) string {
	p := fmt.Sprintf("p%d_", page)
	svg = pdfIDDefRe.ReplaceAllString(svg, `id="`+p+`$1"`)
	svg = pdfURLRefRe.ReplaceAllString(svg, `url(#`+p+`$1)`)
	svg = pdfHrefRe.ReplaceAllString(svg, `$1="#`+p+`$2"`)
	return svg
}

// RenderDeckPDF rasterises the whole deck to a single multi-page PDF via
// Chromium print-to-PDF — one 1920×1080 landscape page per slide. SVG-mode
// slides are inlined (id-namespaced per page so their <defs> don't collide);
// any non-SVG slide falls back to a themed blank page so pagination and the
// export never break. One Chromium navigation for the whole deck.
func (r *SlideRender) RenderDeckPDF(ctx context.Context, deck schema.Deck) ([]byte, error) {
	if len(deck.Slides) == 0 {
		return nil, fmt.Errorf("RenderDeckPDF: deck has no slides")
	}
	if err := r.loadTemplates(); err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}
	theme := string(deck.Theme)
	if theme == "" {
		theme = string(schema.ThemeMinimalist)
	}
	tok := themetokens.Get(theme)

	var body strings.Builder
	for i, s := range deck.Slides {
		content := strings.TrimSpace(s.Data.SVG)
		if s.Layout == schema.LayoutSVG && content != "" {
			content = namespaceSVGIDs(content, i+1)
		} else {
			content = fmt.Sprintf(`<svg viewBox="0 0 1920 1080" xmlns="http://www.w3.org/2000/svg" width="1920" height="1080"><rect width="1920" height="1080" fill="%s"/></svg>`, tok.BG)
		}
		fmt.Fprintf(&body, `<div class="dw-pg">%s</div>`, content)
	}
	doc := "<!doctype html><html><head><meta charset=\"utf-8\">" +
		tok.StyleBlock("", "", "") +
		// NOTE: override StyleBlock's single-slide shell, which clamps
		// html,body to height:1080px;overflow:hidden (that would clip every
		// page after the first → a 1-page PDF). Force the document to grow so
		// the .dw-pg pages stack and paginate.
		`<style>@page{size:20in 11.25in;margin:0}html,body{margin:0!important;padding:0!important;width:1920px!important;height:auto!important;min-height:0!important;overflow:visible!important;background:` + tok.BG +
		`;-webkit-print-color-adjust:exact;print-color-adjust:exact}.dw-pg{width:1920px;height:1080px;overflow:hidden;break-after:page;page-break-after:always}.dw-pg:last-child{break-after:auto;page-break-after:auto}.dw-pg>svg{display:block;width:1920px;height:1080px}</style>` +
		"</head><body>" + body.String() + "</body></html>"

	stagingDir, err := os.MkdirTemp("", "dw-pdf-*")
	if err != nil {
		return nil, fmt.Errorf("staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	htmlPath := filepath.Join(stagingDir, "deck.html")
	if err := os.WriteFile(htmlPath, []byte(doc), 0o644); err != nil {
		return nil, fmt.Errorf("write deck html: %w", err)
	}

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	awaitFontsReady := chromedp.Evaluate(`document.fonts.ready`, nil,
		func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		})

	var pdf []byte
	if err := chromedp.Run(browserCtx,
		chromedp.EmulateViewport(renderViewportW, renderViewportH),
		chromedp.Navigate("file://"+htmlPath),
		chromedp.WaitReady("body", chromedp.ByQuery),
		awaitFontsReady,
		chromedp.Sleep(250*time.Millisecond), // last paint settle
		chromedp.ActionFunc(func(c context.Context) error {
			buf, _, perr := page.PrintToPDF().
				WithPrintBackground(true).
				WithPreferCSSPageSize(false). // paper dims below are the single source of truth
				WithMarginTop(0).WithMarginBottom(0).
				WithMarginLeft(0).WithMarginRight(0).
				WithPaperWidth(20).WithPaperHeight(11.25). // 1920×1080 px @ 96dpi
				Do(c)
			if perr != nil {
				return perr
			}
			pdf = buf
			return nil
		}),
	); err != nil {
		return nil, fmt.Errorf("print-to-pdf: %w", err)
	}
	return pdf, nil
}

// renderHTML executes the HTML+Tailwind template for one slide.
func (r *SlideRender) renderHTML(s schema.Slide, theme schema.Theme, folio int) ([]byte, error) {
	tplName := s.Template
	if tplName == "" {
		tplName = string(schema.ThemeMinimalist)
	}
	// Sprint SV-1 — svg layout bypasses the templated .html-slide
	// container entirely. The slide IS a full-bleed 1920×1080 <svg>;
	// we wrap it in a minimal shell carrying the theme's CSS-var <style>
	// (so var(--bg/--fg/--accent/--font-*) inside the SVG resolves) +
	// a margin-0 body so the SVG bleeds to the canvas edge. No
	// chromedp/template surprises — what the LLM authored is what
	// renders.
	if s.Layout == schema.LayoutSVG {
		return r.renderSVGShell(s, tplName), nil
	}
	// Defensive: callers are supposed to invoke loadTemplates() first, but
	// belt-and-braces — a nil *template.Template would otherwise nil-deref
	// inside Lookup and take the whole orchestrator down.
	if r.loaded == nil {
		return nil, fmt.Errorf("slide_render: templates not loaded (TemplateDir=%q, loadErr=%v)", r.TemplateDir, r.loadErr)
	}
	tpl := r.loaded.Lookup(tplName + ".html")
	if tpl == nil {
		return nil, fmt.Errorf("template %q not found in %s", tplName, r.TemplateDir)
	}
	if folio < 1 {
		folio = 1
	}
	view := templateView{
		Layout:     s.Layout,
		Theme:      theme,
		Data:       s.Data,
		Folio:      folio,
		FolioRoman: strings.ToUpper(toRoman(folio)),
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, view); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderSVGShell wraps a layout=svg slide's raw <svg> in a full-bleed
// HTML document for chromedp rasterization (Sprint SV-1). The theme's
// CSS-var <style> is prepended so var(--bg/--fg/--accent/--font-*)
// inside the SVG resolve; body margin:0 lets the 1920×1080 svg bleed to
// the canvas edge (the .html-slide padded container is deliberately
// bypassed). No sanitization here — SV-3's converter + the LLM prompt
// constrain SVG to vector primitives; the spike confirmed chromedp
// renders it safely. (A future hardening pass can run an SVG-specific
// sanitizer; for now the markup never leaves our own render sandbox.)
func (r *SlideRender) renderSVGShell(s schema.Slide, theme string) []byte {
	tok := themetokens.Get(theme)
	style := tok.StyleBlock("", "", "")
	svg := s.Data.SVG
	if strings.TrimSpace(svg) == "" {
		// Defensive: empty svg → a blank themed canvas rather than a
		// broken chromedp navigate.
		svg = `<svg viewBox="0 0 1920 1080" xmlns="http://www.w3.org/2000/svg" width="1920" height="1080"><rect width="1920" height="1080" fill="` + tok.BG + `"/></svg>`
	}
	doc := "<!doctype html><html><head><meta charset=\"utf-8\">" + style + "</head><body>" + svg + "</body></html>"
	return []byte(doc)
}

// templateFuncs are the helpers every slide template can call. Keep this set
// small and obvious — templates are authored as data, not code, so heavy
// logic should live in Go.
var templateFuncs = template.FuncMap{
	"add1":  func(i int) int { return i + 1 },
	"alpha": func(i int) string { return string(rune('a' + i%26)) }, // 0→a, 1→b, …
	"ALPHA": func(i int) string { return string(rune('A' + i%26)) }, // 0→A, 1→B, …
	"roman": func(i int) string { return toRoman(i + 1) },           // 0→i, 1→ii, …
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	// iterate(n) returns [0, 1, …, n-1] so templates can run a fixed-
	// length loop without arithmetic helpers. Used by the star-rating
	// renderer: `{{range iterate 5}} … {{end}}`.
	"iterate": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	},
	// Sprint V — safehtml runs the LLM-authored HTML through a strict
	// bluemonday sanitizer (htmlSlidePolicy below) and casts the
	// result to template.HTML so Go's html/template doesn't re-escape
	// it. Used ONLY by the `html` layout branch. Any script / iframe
	// / form / object / link / input tag is stripped; inline style +
	// svg + structural tags survive. Cost is sub-millisecond per
	// slide so we sanitize on every render rather than caching.
	"safehtml": func(raw string) template.HTML {
		return template.HTML(htmlSlidePolicy.Sanitize(raw))
	},
	// Sprint AE.7 — emit the inline CSS variable bag for per-slide
	// typography overrides. Returns a `style="..."` attribute fragment
	// (including the leading space) when any override is set; returns
	// "" otherwise so templates don't get stray empty attributes.
	// Template usage: `<section class="slide"{{slideStyle .Data.Style}} …>`
	"slideStyle": func(s *schema.SlideStyle) template.HTMLAttr {
		if s == nil {
			return ""
		}
		parts := []string{}
		if s.TitleScale > 0 {
			parts = append(parts, fmt.Sprintf("--slide-title-scale: %g", clampScale(s.TitleScale)))
		}
		if s.BodyScale > 0 {
			parts = append(parts, fmt.Sprintf("--slide-body-scale: %g", clampScale(s.BodyScale)))
		}
		if s.BulletScale > 0 {
			parts = append(parts, fmt.Sprintf("--slide-bullet-scale: %g", clampScale(s.BulletScale)))
		}
		switch s.Density {
		case "compact":
			parts = append(parts,
				"--slide-density-pad: 0.78",
				"--slide-density-leading: 0.92",
			)
		case "spacious":
			parts = append(parts,
				"--slide-density-pad: 1.28",
				"--slide-density-leading: 1.12",
			)
		}
		if len(parts) == 0 {
			return ""
		}
		// Leading space + style="..." so the template can splice it inline
		// straight after the existing attributes on <section>.
		return template.HTMLAttr(` style="` + strings.Join(parts, "; ") + `"`)
	},
}

// clampScale enforces the documented 0.7..1.5 range on per-slide
// scale multipliers. Values outside the range are silently clamped to
// the nearest valid bound — the tool layer rejects bad inputs but
// this is defence-in-depth in case a stored deck has stale values.
func clampScale(x float64) float64 {
	if x < 0.7 {
		return 0.7
	}
	if x > 1.5 {
		return 1.5
	}
	return x
}

// htmlSlidePolicy is the bluemonday policy applied to LLM-authored
// HTML in the `html` layout. Starts from UGCPolicy (the most
// permissive default that's still XSS-safe) and tightens further:
//   - allow inline `style` attributes (LLM needs them to use CSS vars)
//   - allow inline `<style>` elements (LLM may scope styles to the slide)
//   - allow `<svg>` and its common children (charts / decorations)
//   - explicitly drop `<form>` / `<input>` / `<iframe>` (UGC allows
//     iframe for video embeds; we don't want that in a slide)
var htmlSlidePolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("style").Globally()
	p.AllowElements("style")
	// SVG primitives commonly used for inline charts / decorations.
	p.AllowElements("svg", "g", "path", "rect", "circle", "ellipse",
		"line", "polyline", "polygon", "text", "tspan", "defs",
		"linearGradient", "radialGradient", "stop", "filter",
		"feGaussianBlur", "feOffset", "feMerge", "feMergeNode",
		"clipPath", "mask", "use")
	p.AllowAttrs("viewBox", "width", "height", "fill", "stroke",
		"stroke-width", "stroke-linecap", "stroke-linejoin", "d",
		"x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry",
		"points", "transform", "opacity", "fill-opacity",
		"stroke-opacity", "stop-color", "stop-opacity", "offset",
		"gradientUnits", "gradientTransform", "id", "clip-path",
		"text-anchor", "font-size", "font-family").OnElements(
		"svg", "g", "path", "rect", "circle", "ellipse",
		"line", "polyline", "polygon", "text", "tspan",
		"linearGradient", "radialGradient", "stop", "clipPath",
		"mask", "use", "filter", "feGaussianBlur", "feOffset",
		"feMerge", "feMergeNode")
	// Hard-blocked tags (defense in depth — UGCPolicy already excludes
	// most, but be explicit).
	for _, banned := range []string{
		"script", "iframe", "form", "input", "button", "object",
		"embed", "link", "meta", "base", "applet", "frame", "frameset",
	} {
		p.AllowElements(banned)              // register so we can target
		p = p.SkipElementsContent(banned)    // then strip + drop content
	}
	return p
}()

// toRoman returns lowercase Roman numerals up to 39. Enough for any slide
// bullet list we'll generate; if we ever exceed it the fallback ("xxxix+")
// is at least obvious in review.
func toRoman(n int) string {
	if n < 1 {
		return ""
	}
	if n > 39 {
		return "xxxix+"
	}
	parts := []struct {
		v int
		s string
	}{
		{10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"},
	}
	var sb strings.Builder
	for _, p := range parts {
		for n >= p.v {
			sb.WriteString(p.s)
			n -= p.v
		}
	}
	return sb.String()
}

// loadTemplates walks TemplateDir once. Two paths coexist during the
// per-theme migration (Sprint C4):
//
//   1. NEW arch  — _shared/layout.html + _shared/base.css + _themes/<name>.css.
//      One layout, per-theme tokens. The theme's CSS files are spliced
//      into the layout source at load time and registered as
//      "<theme>.html" so the existing Lookup call sites don't change.
//
//   2. LEGACY arch — <theme>/index.html monolithic file. Used as a
//      fallback for any theme that doesn't yet have a _themes/*.css
//      counterpart. Themes migrate over one at a time; once all five
//      are on the new arch we delete the legacy files and this branch.
//
// Themes covered by the new arch take precedence; legacy files for the
// same theme are skipped to avoid template-name collisions.
func (r *SlideRender) loadTemplates() error {
	r.once.Do(func() {
		root := template.New("root").Funcs(templateFuncs)

		// ─── New arch ────────────────────────────────────────────
		// Read shared layout + base CSS once. Either being absent is
		// a hard failure ONLY if we have any _themes/*.css; otherwise
		// we fall through to legacy.
		layoutPath := filepath.Join(r.TemplateDir, "_shared", "layout.html")
		baseCSSPath := filepath.Join(r.TemplateDir, "_shared", "base.css")
		themesDir := filepath.Join(r.TemplateDir, "_themes")

		layoutSrc, layoutErr := os.ReadFile(layoutPath)
		baseCSS, baseErr := os.ReadFile(baseCSSPath)
		themeFiles, themesErr := os.ReadDir(themesDir)

		migrated := map[string]bool{}
		if layoutErr == nil && baseErr == nil && themesErr == nil {
			for _, f := range themeFiles {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".css") {
					continue
				}
				themeName := strings.TrimSuffix(f.Name(), ".css")
				themeCSS, err := os.ReadFile(filepath.Join(themesDir, f.Name()))
				if err != nil {
					r.loadErr = fmt.Errorf("read theme css %s: %w", f.Name(), err)
					return
				}
				src := buildSharedTemplateSrc(layoutSrc, baseCSS, themeCSS)
				if _, err := root.New(themeName + ".html").Parse(src); err != nil {
					r.loadErr = fmt.Errorf("parse new-arch template %s: %w", themeName, err)
					return
				}
				migrated[themeName] = true
			}
		}

		// ─── Legacy arch (fallback) ──────────────────────────────
		entries, err := os.ReadDir(r.TemplateDir)
		if err != nil {
			r.loadErr = err
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
				continue
			}
			if migrated[e.Name()] {
				continue // new arch already handled it
			}
			htmlPath := filepath.Join(r.TemplateDir, e.Name(), "index.html")
			b, err := os.ReadFile(htmlPath)
			if err != nil {
				continue
			}
			if _, err := root.New(e.Name() + ".html").Parse(string(b)); err != nil {
				r.loadErr = fmt.Errorf("parse legacy %s: %w", htmlPath, err)
				return
			}
		}

		r.loaded = root
	})
	return r.loadErr
}

// buildSharedTemplateSrc splices the base + theme CSS into the shared
// layout HTML. The layout file carries two markers (__BASE_STYLE__ and
// __THEME_STYLE__) where the renderer wraps each CSS chunk in a
// <style> block before substituting. Each marker is replaced once.
func buildSharedTemplateSrc(layout, base, theme []byte) string {
	src := string(layout)
	src = strings.Replace(src, "__BASE_STYLE__", "<style>\n"+string(base)+"\n</style>", 1)
	src = strings.Replace(src, "__THEME_STYLE__", "<style>\n"+string(theme)+"\n</style>", 1)
	return src
}

// emuPerPx maps a CSS pixel (at our 1920-wide capture viewport) to PowerPoint
// EMUs. 13.333 in × 914400 EMU/in = 12,192,000 EMU; divided by 1920 = 6350.
const emuPerPx = (slideWidthInches * float64(measurement.Inch)) / float64(renderViewportW)

// assemblePPTX writes one PPTX where every slide is the captured background
// PNG plus an overlay of native PowerPoint text boxes. Users can re-edit
// the text in PowerPoint; the PNG underneath has no text so edits don't
// produce a double-render artifact.
func (r *SlideRender) assemblePPTX(_ context.Context, deck schema.Deck, assets []slideAssets) (string, error) {
	if err := os.MkdirAll(r.OutDir, 0o755); err != nil {
		return "", err
	}
	ppt := presentation.New()

	// Pick a slide layout. Layouts live on the slide-master, not directly on
	// the Presentation. The default master shipped by unioffice has one
	// (unnamed) layout that's effectively "Blank" — perfect for our
	// full-bleed image slides.
	masters := ppt.SlideMasters()
	if len(masters) == 0 {
		return "", fmt.Errorf("presentation has no slide masters")
	}
	layouts := masters[0].SlideLayouts()
	if len(layouts) == 0 {
		return "", fmt.Errorf("slide master has no layouts")
	}
	layout := layouts[0]
	for _, l := range layouts {
		if l.Name() == "Blank" {
			layout = l
			break
		}
	}

	// unioffice keeps a *reference* to the image file path; it re-reads on
	// SaveToFile. We must therefore keep the PNGs on disk until after save
	// and clean them up at the end.
	tmpImgs := make([]string, 0, len(assets))
	defer func() {
		for _, p := range tmpImgs {
			_ = os.Remove(p)
		}
	}()

	for i, asset := range assets {
		slide, err := ppt.AddDefaultSlideWithLayout(layout)
		if err != nil {
			return "", fmt.Errorf("add slide %d: %w", i, err)
		}

		// Sprint SV-3b — for svg-layout slides, try converting the
		// decoration (rect/circle/line) to native editable shapes. When
		// the whole SVG is representable we emit shapes + skip the PNG
		// background, so the deck is fully vector-editable in PowerPoint.
		// Anything we can't represent (path/gradient/transform) makes
		// parseSVGShapes return ok=false → we keep the PNG background
		// (full visual fidelity, just not vector). Text is overlaid
		// either way via addTextBox below.
		native := false
		if i < len(deck.Slides) && deck.Slides[i].Layout == schema.LayoutSVG {
			// Sprint PM-5 — rich ppt-master SVG uses gradients / filters /
			// paths / patterns the native-shape converter can't represent;
			// converting it loses the depth and looks flatter than the
			// preview. For rich SVG, take the faithful double-layer route
			// instead: the text-hidden BgPNG (full rich decoration) becomes
			// the slide background and editable TextBoxes overlay on top, so
			// the .pptx is pixel-identical to the preview AND text-editable.
			// Only the simpler SV-5 deterministic SVG goes native (fully
			// vector-editable shapes).
			if !isRichSVG(deck.Slides[i].Data.SVG) {
				tok := themetokens.Get(string(deck.Slides[i].Template))
				if shapes, ok := parseSVGShapes(deck.Slides[i].Data.SVG, tok); ok && len(shapes) > 0 {
					emitSVGShapes(slide, shapes)
					native = true
				}
			}
		}

		if !native {
			tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("dw-slide-%s-%d.png", uuid.NewString()[:8], i))
			if err := os.WriteFile(tmpFile, asset.BgPNG, 0o644); err != nil {
				return "", fmt.Errorf("write tmp png: %w", err)
			}
			tmpImgs = append(tmpImgs, tmpFile)

			img, err := common.ImageFromFile(tmpFile)
			if err != nil {
				return "", fmt.Errorf("load image into pptx: %w", err)
			}
			ref, err := ppt.AddImage(img)
			if err != nil {
				return "", fmt.Errorf("add image: %w", err)
			}
			ph := slide.AddImage(ref)
			ph.Properties().SetWidth(slideWidthInches * measurement.Inch)
			ph.Properties().SetHeight(slideHeightInches * measurement.Inch)
			ph.Properties().SetPosition(0, 0)
		}

		// Overlay editable text boxes (on top of native shapes or PNG).
		for _, tb := range asset.TextBoxes {
			addTextBox(slide, tb)
		}
	}

	outPath := filepath.Join(r.OutDir, sanitize(deck.Title)+"-"+uuid.NewString()[:8]+".pptx")
	if err := ppt.SaveToFile(outPath); err != nil {
		return "", fmt.Errorf("save pptx: %w", err)
	}

	// Persist WITH-TEXT preview PNGs next to the PPTX so the frontend can
	// render thumbnails before the user downloads anything. Naming:
	//   <base>-page-1.png, <base>-page-2.png, …
	// The API derives these paths from job.PptxPath; no extra DB row needed.
	base := strings.TrimSuffix(outPath, ".pptx")
	for i, asset := range assets {
		if len(asset.PreviewPNG) == 0 {
			continue
		}
		pngPath := fmt.Sprintf("%s-page-%d.png", base, i+1)
		if err := os.WriteFile(pngPath, asset.PreviewPNG, 0o644); err != nil {
			slog.Warn("save preview png", "path", pngPath, "err", err)
		}
	}

	return outPath, nil
}

// addTextBox creates one editable PowerPoint text box that mirrors a DOM
// text node's position, size, and typography. Italic isn't wired up because
// the unioffice fork's RunProperties doesn't expose SetItalic — the templates
// don't use italic today so this is fine. Add later via the raw OOXML tree
// when needed.
func addTextBox(slide presentation.Slide, tb schema.TextBox) {
	box := slide.AddTextBox()
	box.Properties().SetPosition(
		measurement.Distance(tb.Left*emuPerPx),
		measurement.Distance(tb.Top*emuPerPx),
	)
	box.Properties().SetWidth(measurement.Distance(tb.Width * emuPerPx))
	box.Properties().SetHeight(measurement.Distance(tb.Height * emuPerPx))
	// No fill — keep the background PNG visible behind any padding.
	box.Properties().SetNoFill()

	// unioffice's AddTextBox defaults bodyPr to wrap="square" + spAutoFit,
	// which makes PowerPoint pad each side with ~0.1" and try to grow/shrink
	// to fit the text. Our boxes are positioned at pixel-accurate DOM bboxes,
	// so any extra padding pushes neighboring runs into overlap. We reach
	// in via slide.X() to override the last-added shape's bodyPr.
	tightenBodyProps(slide)

	para := box.AddParagraph()
	if a := pptAlignFromCSS(tb.TextAlign); a != dml.ST_TextAlignTypeUnset {
		para.Properties().SetAlign(a)
	}

	run := para.AddRun()
	run.SetText(tb.Text)
	rp := run.Properties()

	// Font size: SetSize wants EMU; measurement.Point is the EMU-per-pt
	// constant. We extract pt on the JS side, so multiply directly.
	rp.SetSize(measurement.Distance(tb.FontSize) * measurement.Point)
	if tb.FontWeight >= 600 {
		rp.SetBold(true)
	}
	if c, ok := parseHexColor(tb.Color); ok {
		rp.SetSolidFill(c)
	}

	// Latin + East-Asian fonts written separately. The CSS stack carries
	// both (e.g. "Inter, Noto Sans SC, PingFang SC") and we split it so
	// PowerPoint renders Latin runs with Inter and Chinese runs with Noto
	// Sans SC instead of one Latin-only font that turns CJK into tofu.
	//
	// RunProperties.SetFont only sets <a:latin>. East-Asian needs <a:ea>,
	// which unioffice doesn't expose directly — we reach in through
	// Run.X().R.RPr to set it on the raw OOXML struct.
	latinFont, eaFont := splitFontStack(tb.FontFamily)
	if latinFont != "" {
		rp.SetFont(latinFont)
	}
	if eaFont != "" {
		if reg := run.X().R; reg != nil {
			if reg.RPr == nil {
				reg.RPr = dml.NewCT_TextCharacterProperties()
			}
			reg.RPr.Ea = &dml.CT_TextFont{TypefaceAttr: eaFont}
		}
	}
}

// splitFontStack reads a CSS font-family declaration ("Inter, 'Noto Sans SC',
// PingFang SC, sans-serif") and returns the first Latin face and the first
// CJK face. Either string can be empty if the stack doesn't include that
// kind. The detection is intentionally conservative: well-known CJK markers
// ("SC", "TC", "CJK", "PingFang", "YaHei", "WenKai", "Hanyi", "Kaiti",
// "Songti") flag a font as East Asian; everything else is treated as Latin.
func splitFontStack(stack string) (latin, ea string) {
	for _, raw := range strings.Split(stack, ",") {
		name := strings.Trim(strings.TrimSpace(raw), `"'`)
		if name == "" || isGenericFamily(name) {
			continue
		}
		if isCJKFont(name) {
			if ea == "" {
				ea = name
			}
		} else if latin == "" {
			latin = name
		}
	}
	return
}

func isCJKFont(name string) bool {
	lower := strings.ToLower(name)
	for _, tok := range []string{
		" sc", "-sc",
		" tc", "-tc",
		"cjk",
		"pingfang", "yahei", "microsoft yahei",
		"wenkai", "lxgw",
		"hanyi", "kaiti", "songti",
		"noto sans sc", "noto serif sc",
		"source han", "siyuan",
	} {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

func isGenericFamily(name string) bool {
	switch strings.ToLower(name) {
	case "sans-serif", "serif", "monospace", "system-ui", "ui-sans-serif",
		"ui-serif", "ui-monospace", "cursive", "fantasy", "emoji", "math":
		return true
	}
	return false
}

// tightenBodyProps zeroes the inset, disables wrap + autofit on the last
// shape attached to a slide. Call this immediately after slide.AddTextBox()
// so we know which shape is "last". Without it PowerPoint pads each side
// with ~0.1" and tries to autosize, both of which cause our pixel-accurate
// text boxes to overlap neighboring runs.
func tightenBodyProps(slide presentation.Slide) {
	choices := slide.X().CSld.SpTree.Choice
	if len(choices) == 0 {
		return
	}
	last := choices[len(choices)-1]
	if len(last.Sp) == 0 {
		return
	}
	sp := last.Sp[0]
	if sp.TxBody == nil {
		return
	}
	if sp.TxBody.BodyPr == nil {
		sp.TxBody.BodyPr = dml.NewCT_TextBodyProperties()
	}
	bp := sp.TxBody.BodyPr
	// wrap="square" lets PPT re-wrap text inside the box width, matching how
	// the browser laid out the same string. Forcing wrap="none" makes long
	// Chinese titles spill off the slide on a single line. The overlap
	// problem the previous build had came from spAutoFit (box auto-grows
	// to fit text, pushing into the next slot), not from wrapping.
	bp.WrapAttr = dml.ST_TextWrappingTypeSquare
	// Disable BOTH spAutoFit (don't grow the box) and noAutofit; use
	// normAutofit so PPT shrinks the font when text really won't fit even
	// after wrapping — instead of bleeding past the box boundary.
	bp.SpAutoFit = nil
	bp.NoAutofit = nil
	bp.NormAutofit = dml.NewCT_TextNormalAutofit()
	// Zero all four insets (defaults are ~91440 EMU = 0.1").
	bp.LInsAttr = newCoord32(0)
	bp.TInsAttr = newCoord32(0)
	bp.RInsAttr = newCoord32(0)
	bp.BInsAttr = newCoord32(0)
}

func newCoord32(v int32) *dml.ST_Coordinate32 {
	return &dml.ST_Coordinate32{ST_Coordinate32Unqualified: &v}
}

// parseHexColor converts "#RRGGBB" → unioffice color. Returns ok=false on
// any malformed input; the caller falls back to the run's default.
func parseHexColor(s string) (color.Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.Color{}, false
	}
	r, err1 := strconv.ParseUint(s[0:2], 16, 8)
	g, err2 := strconv.ParseUint(s[2:4], 16, 8)
	b, err3 := strconv.ParseUint(s[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return color.Color{}, false
	}
	return color.RGB(uint8(r), uint8(g), uint8(b)), true
}

// pptAlignFromCSS maps CSS `text-align` to OOXML's ST_TextAlignType enum
// (left / center / right / justify). Returns Unset if the input is empty or
// unrecognised so the caller can leave alignment at the run default.
func pptAlignFromCSS(css string) dml.ST_TextAlignType {
	switch strings.ToLower(strings.TrimSpace(css)) {
	case "center":
		return dml.ST_TextAlignTypeCtr
	case "right":
		return dml.ST_TextAlignTypeR
	case "justify":
		return dml.ST_TextAlignTypeJust
	case "left", "start":
		return dml.ST_TextAlignTypeL
	}
	return dml.ST_TextAlignTypeUnset
}

func (r *SlideRender) emit(ctx context.Context, ev event.Event) {
	if r.Emitter == nil {
		return
	}
	r.Emitter.Emit(ctx, ev) // SessionID injected from ctx
}

func sanitize(s string) string {
	if s == "" {
		return "deck"
	}
	out := make([]byte, 0, len(s))
	for _, ch := range []byte(s) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
			out = append(out, ch)
		case ch == ' ' || ch == '-' || ch == '_':
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "deck"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return string(out)
}

// (base64 encoding now uses encoding/base64.StdEncoding)

// RenderDeckPresentHTML assembles the whole deck into ONE self-contained,
// shareable HTML "presenter" page — the web-presentation mode. Each SVG slide
// is inlined (id-namespaced per slide so their <defs> don't collide) and a small
// JS presenter adds keyboard/click navigation, fullscreen, a page counter, a
// progress bar, fade transitions, and an overview grid (press O). The SVG stays
// the content (the .pptx export is untouched) — this is purely the HTML *shell*
// for presenting on the web. No chromedp; the browser renders it directly.
func (r *SlideRender) RenderDeckPresentHTML(deck schema.Deck) (string, error) {
	if len(deck.Slides) == 0 {
		return "", fmt.Errorf("RenderDeckPresentHTML: deck has no slides")
	}
	theme := string(deck.Theme)
	if theme == "" {
		theme = string(schema.ThemeMinimalist)
	}
	tok := themetokens.Get(theme)

	var slidesHTML strings.Builder
	for i, s := range deck.Slides {
		content := strings.TrimSpace(s.Data.SVG)
		if s.Layout == schema.LayoutSVG && content != "" {
			content = namespaceSVGIDs(content, i+1)
		} else {
			content = fmt.Sprintf(`<svg viewBox="0 0 1920 1080" xmlns="http://www.w3.org/2000/svg" width="1920" height="1080"><rect width="1920" height="1080" fill="%s"/></svg>`, tok.BG)
		}
		active := ""
		if i == 0 {
			active = ` data-active="1"`
		}
		fmt.Fprintf(&slidesHTML, `<section class="dwp-slide"%s>%s</section>`, active, content)
	}

	title := template.HTMLEscapeString(deck.Title)
	if title == "" {
		title = "Presentation"
	}
	n := strconv.Itoa(len(deck.Slides))

	doc := `<!doctype html><html lang="zh"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">` +
		`<title>` + title + `</title><style>` + presentCSS + `</style></head><body>` +
		`<div id="dwp-bar"></div>` +
		`<div id="dwp-stage">` + slidesHTML.String() + `</div>` +
		`<div id="dwp-grid" aria-hidden="true"></div>` +
		`<div id="dwp-ui">` +
		`<button id="dwp-over" title="总览 (O)">▦</button>` +
		`<button id="dwp-prev" title="上一页 (←)">‹</button>` +
		`<span id="dwp-count">1 / ` + n + `</span>` +
		`<button id="dwp-next" title="下一页 (→)">›</button>` +
		`<button id="dwp-full" title="全屏 (F)">⛶</button>` +
		`</div><script>` + presentJS + `</script></body></html>`
	return doc, nil
}

// presentCSS / presentJS — the web-presentation shell. Plain const strings (not
// fmt-formatted), so literal % in CSS/JS is fine. The 1920×1080 stage is scaled
// to the viewport via --dwp-s; only the active slide is shown.
const presentCSS = `*{margin:0;padding:0;box-sizing:border-box}
html,body{height:100%;background:#07090d;overflow:hidden;font-family:ui-sans-serif,system-ui,-apple-system,"PingFang SC","Microsoft YaHei",sans-serif}
#dwp-stage{position:fixed;inset:0;display:flex;align-items:center;justify-content:center}
.dwp-slide{position:absolute;width:1920px;height:1080px;opacity:0;visibility:hidden;transition:opacity .35s ease;transform:scale(var(--dwp-s,1));transform-origin:center center;box-shadow:0 30px 90px rgba(0,0,0,.55)}
.dwp-slide[data-active="1"]{opacity:1;visibility:visible}
.dwp-slide>svg{display:block;width:1920px;height:1080px}
#dwp-bar{position:fixed;left:0;top:0;height:3px;background:#3b82f6;width:0;transition:width .35s ease;z-index:30}
#dwp-ui{position:fixed;bottom:16px;right:22px;display:flex;align-items:center;gap:4px;color:#9aa3b2;font-size:13px;letter-spacing:.05em;z-index:30;opacity:.3;transition:opacity .2s}
body:hover #dwp-ui{opacity:1}
#dwp-ui button{background:none;border:none;color:#9aa3b2;cursor:pointer;font-size:17px;line-height:1;padding:6px 9px;border-radius:7px;transition:.15s}
#dwp-ui button:hover{color:#fff;background:rgba(255,255,255,.1)}
#dwp-count{font-variant-numeric:tabular-nums;padding:0 6px;min-width:64px;text-align:center}
#dwp-grid{position:fixed;inset:0;background:#07090d;display:none;grid-template-columns:repeat(4,1fr);gap:18px;padding:34px;overflow:auto;z-index:40}
#dwp-grid.open{display:grid}
.dwp-thumb{position:relative;aspect-ratio:16/9;border:2px solid #1b2128;border-radius:9px;overflow:hidden;cursor:pointer;background:#0d1017;transition:.15s}
.dwp-thumb:hover{border-color:#3b82f6aa}
.dwp-thumb.cur{border-color:#3b82f6}
.dwp-thumb svg{position:absolute;inset:0;width:100%;height:100%}
.dwp-thumb .n{position:absolute;left:9px;bottom:7px;color:#cbd2dd;font-size:12px;font-variant-numeric:tabular-nums;text-shadow:0 1px 3px #000;z-index:1}`

const presentJS = `(function(){
var stage=document.getElementById('dwp-stage');
var slides=[].slice.call(document.querySelectorAll('.dwp-slide'));
var n=slides.length,cur=0;
var count=document.getElementById('dwp-count'),bar=document.getElementById('dwp-bar'),grid=document.getElementById('dwp-grid');
function fit(){var s=Math.min(window.innerWidth/1920,window.innerHeight/1080);document.documentElement.style.setProperty('--dwp-s',s);}
function show(i){cur=Math.max(0,Math.min(n-1,i));for(var k=0;k<slides.length;k++){slides[k].setAttribute('data-active',k===cur?'1':'0');}count.textContent=(cur+1)+' / '+n;bar.style.width=((cur+1)/n*100)+'%';var th=grid.children;for(var j=0;j<th.length;j++){th[j].className='dwp-thumb'+(j===cur?' cur':'');}}
function next(){show(cur+1);}function prev(){show(cur-1);}
function full(){if(document.fullscreenElement){document.exitFullscreen();}else if(document.documentElement.requestFullscreen){document.documentElement.requestFullscreen();}}
function over(){grid.classList.toggle('open');}
window.addEventListener('keydown',function(e){var k=e.key;if(grid.classList.contains('open')&&(k==='Escape'||k==='o'||k==='O')){grid.classList.remove('open');return;}if(k==='ArrowRight'||k==='ArrowDown'||k===' '||k==='PageDown'){e.preventDefault();next();}else if(k==='ArrowLeft'||k==='ArrowUp'||k==='PageUp'){e.preventDefault();prev();}else if(k==='Home'){show(0);}else if(k==='End'){show(n-1);}else if(k==='f'||k==='F'){full();}else if(k==='o'||k==='O'){over();}});
stage.addEventListener('click',function(e){if(e.clientX<window.innerWidth*0.22){prev();}else{next();}});
for(var i=0;i<slides.length;i++){(function(idx){var t=document.createElement('div');t.className='dwp-thumb';t.innerHTML=slides[idx].innerHTML+'<span class="n">'+(idx+1)+'</span>';t.addEventListener('click',function(){show(idx);grid.classList.remove('open');});grid.appendChild(t);})(i);}
document.getElementById('dwp-prev').onclick=function(e){e.stopPropagation();prev();};
document.getElementById('dwp-next').onclick=function(e){e.stopPropagation();next();};
document.getElementById('dwp-full').onclick=function(e){e.stopPropagation();full();};
document.getElementById('dwp-over').onclick=function(e){e.stopPropagation();over();};
window.addEventListener('resize',fit);fit();show(0);
})();`
