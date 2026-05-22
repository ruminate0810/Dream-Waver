# PPT Generation Pipeline

```
User input → Outline → Content → Design → Render → .pptx
              (LLM)     (LLM)    (rules)  (chromedp + unioffice)
```

## 1. Outline

[`internal/skill/slides/pipeline.go`](services/orchestrator/internal/skill/slides/pipeline.go) →
`Pipeline.outline()` calls the planner model (Sonnet 4.6 by default) with
[`prompts/outline.md`](services/orchestrator/internal/skill/slides/prompts/outline.md).

**Output (strict JSON):**
```json
{
  "title": "...",
  "subtitle": "...",
  "theme": "corporate | pitch-deck | academic | playful | minimalist",
  "slides": [
    { "index": 1, "type": "title|section|content|data|quote|closing",
      "headline": "...", "key_points": ["..."], "speaker_notes": "..." }
  ]
}
```

The pipeline emits a `slides.outline` event so the UI can render the outline
immediately, before any content fill-in. This matches the Manus "watch it
plan" UX.

## 2. Content

The worker model (Haiku 4.5 by default) consumes the outline JSON and produces
final-quality slide content per [`prompts/content.md`](services/orchestrator/internal/skill/slides/prompts/content.md).

**Output:**
```json
{
  "slides": [
    { "index": 1, "template": "minimalist", "layout": "title",
      "data": { "title": "...", "subtitle": "...", "bullets": ["..."] },
      "speaker_notes": "..." }
  ]
}
```

Layouts come from the chosen template's `manifest.json` — see
[`packages/slide-templates/manifest.json`](packages/slide-templates/manifest.json).

## 3. Design (rule-based for MVP)

`assembleDeck()` in `pipeline.go` reconciles the outline's `theme` with each
slide's `template` choice. If a slide didn't pick a template, the deck theme
wins. Once Phase 2 lands the `DesignAgent` will replace this with an LLM call
that also picks per-slide variants and accent colors.

## 4. Render

`internal/tool/slide_render.go` executes three steps per deck:

### 4a. HTML rendering

For each slide, the matching template (e.g.
[`packages/slide-templates/minimalist/index.html`](packages/slide-templates/minimalist/index.html))
is filled with the slide's `data` via Go's `html/template`.

Templates self-bootstrap Tailwind via CDN:
```html
<script src="https://cdn.tailwindcss.com"></script>
```

We never need a Node build step — Chromium pulls Tailwind at load time, which
keeps the sandbox tight.

### 4b. Chromium screenshot

```go
chromedp.Run(ctx,
    chromedp.EmulateViewport(1920, 1080),
    chromedp.Navigate(dataURL),                  // HTML embedded as data: URL
    chromedp.WaitReady("body"),
    chromedp.Sleep(300 * time.Millisecond),      // let fonts settle
    page.CaptureScreenshot().WithFormat("png"),
)
```

Headless Chromium is launched once per call. For high-throughput production
we'll pool browser contexts.

### 4c. PPTX assembly

`unidoc/unioffice` builds a real `.pptx`:

```go
ppt   := presentation.New()
img   := common.ImageFromFile(pngPath)
ref,_ := ppt.AddImage(img)
slide,_ := ppt.AddDefaultSlideWithLayout()
ph := slide.AddImage(ref)
ph.Properties().SetWidth(13.333 * Inch)
ph.Properties().SetHeight(7.5 * Inch)
ph.Properties().SetPosition(0, 0)
```

**Phase 1 (current):** each slide is a single full-bleed image. The PPTX is
visually identical to the HTML but text is not editable in PowerPoint.

**Phase 2 (planned):** the renderer also reads DOM bounding boxes from
chromedp via `page.Evaluate` and reconstructs each text node as a transparent
editable text box layered over the PNG. Users get a `.pptx` that looks like
the design *and* edits like a normal PowerPoint file.

## Cost model

Per deck, with Claude 4 pricing:

| Stage   | Model     | Tokens (in/out) | Cost      |
| ------- | --------- | --------------- | --------- |
| Outline | Sonnet    | 4k / 2k         | ~$0.04    |
| Content | Haiku     | 8k / 6k         | ~$0.03    |
| —       | —         | —               | **~$0.07**|

Prompt caching reduces the system prompt cost to ~10% on repeat runs.
Estimates are recorded per-job via `Cost.EstimatedUSD` (see `pipeline.go`).

## Verification

End-to-end happy path:

```bash
cp .env.example .env       # add ANTHROPIC_API_KEY
make dev
# open http://localhost:3000 → /slides/new
# submit: topic="Introduce GPT-5", slides=8
# watch the agent trace stream → download .pptx
```

Acceptance criteria for MVP:

- [ ] 10-slide deck completes in ≤ 8 minutes
- [ ] The .pptx opens cleanly in PowerPoint, Keynote, and Google Slides
- [ ] Every slide renders the expected layout (no clipping, fonts loaded)
- [ ] WebSocket emits at minimum: outline, per-slide content, render.start, render.end
- [ ] Total LLM spend < $0.50 per deck
