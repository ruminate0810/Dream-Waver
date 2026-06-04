package stages

import (
	"context"
	"regexp"
	"strings"
)

// Sprint PM-3 — full-bleed images in the rich SVG path. The author marks
// an atmospheric image with `<image href="dw-img://<english prompt>" …/>`
// (then layers a scrim gradient + text on top — ppt-master's two-layer
// composition). resolveSVGImageRefs swaps each placeholder href for a
// freshly generated image URL. Done in the slide's own author goroutine,
// so the slide arrives image-ready (no broken-placeholder flash in the
// live preview). A failed generation drops the <image> tag, so the slide
// falls back to whatever gradient/colour background it drew underneath —
// never a broken image icon.
//
// imgResolve(ctx, prompt) returns a usable image URL/path, or "" on
// failure. It's injected (RunSVG wraps the image.Searcher) so this stages
// package stays free of the image dependency.

var dwImgHrefRe = regexp.MustCompile(`href="dw-img://([^"]*)"`)
var dwImgTagRe = regexp.MustCompile(`<image\b[^>]*href="dw-img://[^"]*"[^>]*/?>`)

// resolveSVGImageRefs replaces every dw-img:// href in svg with a real
// generated image URL. If imgResolve is nil or returns "" for a prompt,
// that whole <image> element is removed.
func resolveSVGImageRefs(ctx context.Context, svg string, imgResolve func(context.Context, string) string) string {
	if imgResolve == nil || !strings.Contains(svg, "dw-img://") {
		// No resolver available — strip the placeholders so nothing renders
		// a broken reference.
		if strings.Contains(svg, "dw-img://") {
			return dwImgTagRe.ReplaceAllString(svg, "")
		}
		return svg
	}
	// Resolve each distinct prompt once.
	cache := map[string]string{}
	for _, m := range dwImgHrefRe.FindAllStringSubmatch(svg, -1) {
		prompt := decodePrompt(m[1])
		if _, done := cache[prompt]; done {
			continue
		}
		cache[prompt] = imgResolve(ctx, prompt)
	}
	// Replace resolved hrefs; drop whole <image> tags whose prompt failed.
	out := dwImgTagRe.ReplaceAllStringFunc(svg, func(tag string) string {
		mm := dwImgHrefRe.FindStringSubmatch(tag)
		if mm == nil {
			return tag
		}
		url := cache[decodePrompt(mm[1])]
		if url == "" {
			return "" // generation failed → drop the image, keep the bg
		}
		return dwImgHrefRe.ReplaceAllString(tag, `href="`+url+`"`)
	})
	return out
}

// decodePrompt turns the href payload back into a human prompt. The author
// writes a plain english phrase; we just trim and turn +/%20 into spaces
// defensively in case it URL-encoded.
func decodePrompt(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	s = strings.ReplaceAll(s, "%20", " ")
	return strings.TrimSpace(s)
}
