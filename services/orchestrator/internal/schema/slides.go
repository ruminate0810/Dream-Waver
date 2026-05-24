package schema

// SlideLayout is the closed set of templated slide arrangements the renderer
// knows how to lay out. Both the LLM and the HTML templates must agree on
// these names; keep prompts/content.md and packages/slide-templates in sync.
type SlideLayout string

const (
	LayoutTitle       SlideLayout = "title"
	LayoutSection     SlideLayout = "section"
	LayoutBullets     SlideLayout = "bullets"
	LayoutContent     SlideLayout = "content"
	LayoutQuote       SlideLayout = "quote"
	LayoutTwoCol      SlideLayout = "two-column"
	LayoutData        SlideLayout = "data"
	LayoutClosing     SlideLayout = "closing"
	// Sprint E2 — extra layouts for richer decks.
	LayoutTimeline    SlideLayout = "timeline"     // 3-7 dated events on a horizontal axis
	LayoutComparison  SlideLayout = "comparison"   // two labelled columns of bullets side-by-side
	LayoutMultiMetric SlideLayout = "multi-metric" // 2-4 KPI cards (value + label + delta)
	// Sprint G1 — high information-density layouts (info-table / TOC / SWOT)
	LayoutComparisonTable SlideLayout = "comparison-table" // N-column × M-row data table with optional star ratings per cell
	LayoutTOC             SlideLayout = "toc"              // table-of-contents — huge title + numbered section list with decoration
	LayoutSWOT            SlideLayout = "swot"             // 2x2 strategic grid (strengths / weaknesses / opportunities / threats)
	// Sprint H8 — image-led layouts that REQUIRE an image_query to look
	// good. Designed to showcase nano-banana AI imagery as the slide's
	// primary content rather than as background.
	LayoutPhotoEssay SlideLayout = "photo-essay" // full-bleed image + 1-line editorial title bottom-left + optional caption — "image-as-statement"
	LayoutSplitImage SlideLayout = "split-image" // 50:50 image-left / text-right (or swapped) — magazine feature feel
	LayoutImageGrid  SlideLayout = "image-grid"  // 3-4 image moodboard grid with single caption — for "concepts / examples / visual references"
)

// Theme picks a base template family. The renderer falls back to LayoutTitle
// content if a slide doesn't specify a known layout.
type Theme string

const (
	ThemeMinimalist Theme = "minimalist"
	ThemeCorporate  Theme = "corporate"
	ThemePitchDeck  Theme = "pitch-deck"
	ThemeAcademic   Theme = "academic"
	ThemePlayful    Theme = "playful"
)

// SlideData carries every datum any layout might use. Layouts pick the
// subset they need (e.g. quote layouts read Quote+Attribution, ignore
// Bullets). Keep this flat — the HTML templates address fields by name.
type SlideData struct {
	Title       string   `json:"title,omitempty"`
	Subtitle    string   `json:"subtitle,omitempty"`
	Body        string   `json:"body,omitempty"`
	Bullets     []string `json:"bullets,omitempty"`
	Quote       string   `json:"quote,omitempty"`
	Attribution string   `json:"attribution,omitempty"`
	Metric      string   `json:"metric,omitempty"`
	Footer      string   `json:"footer,omitempty"`

	// ImageQuery is a 2-5 word English search hint the LLM emits when the
	// slide would benefit from a hero photo. The pipeline resolves it to
	// Image (URL) via Unsplash before rendering. Empty means "no image".
	ImageQuery string `json:"image_query,omitempty"`
	// Image is the resolved hero URL — set by the pipeline, not the LLM.
	// Templates use it as a background or side panel; absent ⇒ no image.
	Image string `json:"image,omitempty"`
	// ImageCredit is a "Photo by X on Unsplash" attribution string mandated
	// by Unsplash's API terms when displaying their photos.
	ImageCredit string `json:"image_credit,omitempty"`

	// ─── Sprint E2 layout-specific fields ────────────────────────────

	// Events populates layout=timeline. 3-7 chronological items rendered
	// on a horizontal axis with date above, label below, optional note.
	Events []TimelineEvent `json:"events,omitempty"`

	// LeftHeader / RightHeader / LeftItems / RightItems populate
	// layout=comparison — two labelled columns side-by-side (e.g.
	// "Before / After", "Plan A / Plan B", "Pros / Cons").
	LeftHeader  string   `json:"left_header,omitempty"`
	RightHeader string   `json:"right_header,omitempty"`
	LeftItems   []string `json:"left_items,omitempty"`
	RightItems  []string `json:"right_items,omitempty"`

	// Metrics populates layout=multi-metric — 2 to 4 KPI cards.
	Metrics []Metric `json:"metrics,omitempty"`

	// Sprint G1 fields:

	// TableHeaders + TableRows populate layout=comparison-table.
	// Headers is the first row's labels; Rows are the data rows
	// underneath. Each cell can be plain text OR a 0-5 star rating.
	TableHeaders []string         `json:"table_headers,omitempty"`
	TableRows    []TableRow       `json:"table_rows,omitempty"`

	// Sections populates layout=toc — the table-of-contents section
	// list. Each item is a [number, label] pair. The decorative chrome
	// (big "Table of Contents" title + background circles) is rendered
	// by base.css; this slice only carries the entries.
	Sections []TOCSection `json:"sections,omitempty"`

	// SWOT populates layout=swot — four quadrants of bullet text.
	// Slice per quadrant so each can have a different length; theme
	// CSS chooses background colour per quadrant (green / red / blue /
	// amber by default).
	Strengths     []string `json:"strengths,omitempty"`
	Weaknesses    []string `json:"weaknesses,omitempty"`
	Opportunities []string `json:"opportunities,omitempty"`
	Threats       []string `json:"threats,omitempty"`

	// Sprint H8 image-led layout fields:

	// Caption is a small italic caption rendered under the title block
	// in layout=photo-essay (and reused as the single caption on
	// layout=image-grid). ≤ 16 words; mood-setting line, not a body
	// paragraph. Renders in serif italic when the theme has one.
	Caption string `json:"caption,omitempty"`

	// ImagePosition controls layout=split-image — "left" (default) puts
	// the image on the left half, "right" puts it on the right. No
	// effect on other layouts.
	ImagePosition string `json:"image_position,omitempty"`

	// ImageQueries populates layout=image-grid. 3-4 short English
	// queries; the pipeline resolves each to its own AI image. The
	// renderer arranges them in a 2x2 (or 1x3) grid. The slide-level
	// ImageQuery field is ignored in favour of this slice for grids.
	ImageQueries []string `json:"image_queries,omitempty"`

	// Images is the resolved URL slice for layout=image-grid, parallel
	// to ImageQueries. Set by the pipeline, not the LLM.
	Images []string `json:"images,omitempty"`
}

// TableRow is one data row inside a comparison-table slide. Cells
// align positionally to the slide's TableHeaders.
type TableRow struct {
	Cells []TableCell `json:"cells"`
}

// TableCell is one datum inside a comparison-table row. Exactly one
// of Text or Rating should be populated:
//   - Text   is shown verbatim (e.g. "27%", "中价格", "✓", "—").
//   - Rating renders as 5 unicode stars (★★★★☆) where filled = Rating,
//     empty = 5 - Rating; range [0, 5]. Use for "评价" / "评分" cells.
//     If Rating > 0, Text is ignored.
type TableCell struct {
	Text   string `json:"text,omitempty"`
	Rating int    `json:"rating,omitempty"`
}

// TOCSection is one entry in a table-of-contents slide.
type TOCSection struct {
	Number string `json:"number"` // "01", "02", … (string so authors can use roman / chapter labels)
	Title  string `json:"title"`
}

// TimelineEvent is one node on a timeline slide. Date is a short label
// (year, "Q1 2026", "Day 3" etc — the renderer doesn't parse it);
// Label is the headline event; Note is optional sub-context.
type TimelineEvent struct {
	Date  string `json:"date"`
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
}

// Metric is one tile on a multi-metric slide. Value is the headline
// number ("73%", "$1.2B"); Label is the dimension ("ARR", "NPS",
// "Churn"); Delta is an optional change indicator ("+12% YoY",
// "−3 pts"). Renderer colours positive/negative deltas if the string
// starts with + or −.
type Metric struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Delta string `json:"delta,omitempty"`
}

// Slide is one rendered page in a Deck.
type Slide struct {
	Template     string      `json:"template"`         // theme name; defaults to Deck.Theme when empty
	Layout       SlideLayout `json:"layout,omitempty"` // empty = template's default
	Data         SlideData   `json:"data"`
	SpeakerNotes string      `json:"speaker_notes,omitempty"`
}

// Deck is the structured form the renderer consumes — produced by the slides
// pipeline (LLM → JSON → validation → Deck). We avoid the legacy round-trip
// through `map[string]any` so both producer and consumer share the same
// compile-time contract.
type Deck struct {
	Title  string  `json:"title"`
	Theme  Theme   `json:"theme,omitempty"`
	Slides []Slide `json:"slides"`

	// Brand carries deck-wide colour + typography overrides set via the
	// `apply_brand` agent tool. The renderer injects these as CSS
	// variables (`--brand-primary` / `--brand-accent` / `--brand-font`)
	// at the top of every served slide HTML. Templates that opt into
	// `var(--brand-*, <default>)` automatically pick them up. Nil
	// means "use the template's own defaults".
	Brand *Brand `json:"brand,omitempty"`
}

// Brand carries the deck-wide overrides applied by the `apply_brand` tool.
// All fields are optional individually; an empty Brand is equivalent to nil
// (no overrides).
type Brand struct {
	PrimaryColor string `json:"primary_color,omitempty"` // #RRGGBB
	AccentColor  string `json:"accent_color,omitempty"`  // #RRGGBB; defaults to PrimaryColor in CSS
	FontFamily   string `json:"font_family,omitempty"`   // raw CSS stack, e.g. "Source Han Sans SC, Noto Sans SC, sans-serif"
}

// TextBox is one editable text region overlaid on a slide. The HTML renderer
// extracts these from the DOM (one per leaf text node) so the final PPTX can
// be re-edited in PowerPoint without regenerating the deck. Coordinates and
// sizes are in CSS pixels at the 1920×1080 capture viewport; the assembler
// converts to EMU before writing the OOXML.
type TextBox struct {
	Text       string  `json:"text"`
	Left       float64 `json:"left"`        // px @ 1920 viewport
	Top        float64 `json:"top"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	FontSize   float64 `json:"font_size"`   // pt (CSS px ÷ 1.333)
	FontFamily string  `json:"font_family"` // comma-separated CSS stack; assembler picks the first
	Color      string  `json:"color"`       // #RRGGBB
	FontWeight int     `json:"font_weight"` // 100-900; ≥600 ⇒ bold
	Italic     bool    `json:"italic"`
	TextAlign  string  `json:"text_align"` // left | center | right | justify
}
