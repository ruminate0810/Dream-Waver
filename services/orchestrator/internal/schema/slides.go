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
	// Sprint K1 — information-architecture layouts covering common
	// presentation patterns the existing 17 layouts can't compose:
	LayoutProcessFlow SlideLayout = "process-flow" // horizontal 3-5 numbered steps with arrows — workflow / how-to (no dates, unlike timeline)
	LayoutBentoGrid   SlideLayout = "bento-grid"   // Apple-style asymmetric card grid (1 large + 3-4 small) — modern "feature overview"
	LayoutPullQuote   SlideLayout = "pull-quote"   // context paragraph + huge serif quote + attribution — quote-with-context
	LayoutBeforeAfter SlideLayout = "before-after" // 50:50 image-vs-image with header labels — transformations / makeovers
	LayoutIconGrid    SlideLayout = "icon-grid"    // 3-4 column grid of icon + label + description — feature / capability listings
	LayoutTeamRoster  SlideLayout = "team-roster"  // horizontal row of avatar + name + role cards — team intros
	// Sprint P1 — close two major content-shape gaps:
	LayoutCode      SlideLayout = "code"      // title + language pill + mono code block + optional caption — for tech tutorials, API docs, snippet showcases
	LayoutChecklist SlideLayout = "checklist" // title + 3-7 task-item rows with ☐ checkboxes — for action items, "next steps", launch checklists, training takeaways
	// Sprint V — freeform HTML escape-hatch. LLM writes raw HTML in
	// SlideData.HTML when none of the 24 typed layouts fits the topic
	// (vintage UI demo / ASCII art / unusual data viz). Output goes
	// through a strict bluemonday sanitizer before render so it can't
	// inject scripts. NOT a default — outline.md tells the LLM to
	// only pick this when other layouts honestly don't work.
	LayoutHTML SlideLayout = "html"
)

// AllSlideLayouts returns every templated layout the renderer knows
// about. Used by the API handler to validate user-supplied layout
// overrides (Sprint S — outline review relayouts) and as the single
// source of truth for surfacing the layout enum to the frontend.
//
// Order is roughly "ship date" — primitives first, then specialty
// layouts. The frontend may regroup for picker UX; the wire enum is
// flat.
func AllSlideLayouts() []SlideLayout {
	return []SlideLayout{
		LayoutTitle, LayoutSection, LayoutBullets, LayoutContent,
		LayoutQuote, LayoutTwoCol, LayoutData, LayoutClosing,
		LayoutTimeline, LayoutComparison, LayoutMultiMetric,
		LayoutComparisonTable, LayoutTOC, LayoutSWOT,
		LayoutPhotoEssay, LayoutSplitImage, LayoutImageGrid,
		LayoutProcessFlow, LayoutBentoGrid, LayoutPullQuote,
		LayoutBeforeAfter, LayoutIconGrid, LayoutTeamRoster,
		LayoutCode, LayoutChecklist,
		LayoutHTML,
	}
}

// IsValidSlideLayout reports whether the given string matches one of
// the SlideLayout consts. Cheap linear scan over ~25 entries.
func IsValidSlideLayout(s string) bool {
	for _, l := range AllSlideLayouts() {
		if string(l) == s {
			return true
		}
	}
	return false
}

// Theme picks a base template family. The renderer falls back to LayoutTitle
// content if a slide doesn't specify a known layout.
type Theme string

const (
	ThemeMinimalist Theme = "minimalist"
	ThemeCorporate  Theme = "corporate"
	ThemePitchDeck  Theme = "pitch-deck"
	ThemeAcademic   Theme = "academic"
	ThemePlayful    Theme = "playful"
	// Sprint E1 — extra themes for richer visual variety
	ThemeEditorial Theme = "editorial"
	ThemeRetro     Theme = "retro"
	ThemeTech      Theme = "tech"
	ThemeZen       Theme = "zen"
	ThemeWarm      Theme = "warm"
	// Sprint H11 — high-contrast cinematic, designed for B&W AI photography
	ThemeNoir Theme = "noir"
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

	// ─── Sprint K1 fields ──────────────────────────────────────────

	// Steps populates layout=process-flow. 3-5 sequential steps
	// rendered as numbered circles with arrows between them; each
	// step has a short label + 1-line description. NO dates (use
	// timeline if there are dates).
	Steps []ProcessStep `json:"steps,omitempty"`

	// BentoCards populates layout=bento-grid. Apple-keynote-style
	// asymmetric mosaic — 1 large card + 3-4 small cards. Each card
	// is one of: title+body (text), metric (big number), or
	// image_query (AI image). Mix freely.
	BentoCards []BentoCard `json:"bento_cards,omitempty"`

	// Citation populates layout=pull-quote (optional). Source line
	// rendered as a small mono-caps footer after the attribution —
	// e.g. "DeepSeek blog · 2026-03 · §4". Empty omits the row.
	Citation string `json:"citation,omitempty"`

	// BeforeImageQuery / AfterImageQuery populate layout=before-after.
	// Two AI-image queries, resolved in parallel to BeforeImage /
	// AfterImage URLs by the pipeline. Both REQUIRED for the layout
	// to render meaningfully — fallback states use diagonal accent
	// striping when fetch fails.
	BeforeImageQuery string `json:"before_image_query,omitempty"`
	AfterImageQuery  string `json:"after_image_query,omitempty"`
	BeforeImage      string `json:"before_image,omitempty"` // resolved by pipeline
	AfterImage       string `json:"after_image,omitempty"`  // resolved by pipeline
	BeforeLabel      string `json:"before_label,omitempty"` // default "Before"
	AfterLabel       string `json:"after_label,omitempty"`  // default "After"

	// Features populates layout=icon-grid. 3 or 4 (or 6 for 2x3)
	// icon + label + 1-2-sentence description cells. Icon is a
	// single emoji or short symbol — no icon-font dependency.
	Features []Feature `json:"features,omitempty"`

	// TeamMembers populates layout=team-roster. 3-6 horizontal
	// avatar+name+role cards. AvatarQuery routes through the same
	// image fanout as ImageQueries (nano-banana generates a square
	// portrait). When the fetch fails, the card renders a monogram
	// disc from the name's first letter.
	TeamMembers []TeamMember `json:"team_members,omitempty"`

	// ─── Sprint P1 layout-specific fields ────────────────────────────

	// Code populates layout=code. Raw code as a single string with
	// real newlines. No syntax highlighter dependency — base.css
	// renders with mono font + theme-coloured comment/keyword hints
	// per language. Caller-provided indentation is preserved (CSS
	// uses white-space: pre). Cap ~25 lines / ~80 cols for readability;
	// longer snippets read better as 2-3 code slides.
	Code string `json:"code,omitempty"`
	// Language hints the renderer's theming + populates a small mono
	// pill next to the title. Common values: "go" / "ts" / "py" /
	// "sh" / "sql" / "json" / "yaml" / "rust" / "css" / "html". Free
	// text — renderer falls back to plain mono if unknown.
	Language string `json:"language,omitempty"`

	// Tasks populates layout=checklist — 3-7 action items rendered
	// as ☐ rows. Distinct from Bullets so the writer LLM can
	// produce a real "next steps / TODO" framing rather than
	// generic bullets. Each item should be an imperative verb-
	// phrase (e.g. "审核 Q1 预算", "联系合规团队", "Update README").
	Tasks []string `json:"tasks,omitempty"`

	// HTML populates layout=html — raw HTML markup the LLM writes when
	// none of the 24 typed layouts fits the topic (vintage UI mock,
	// ASCII art, unusual data viz, etc). The renderer runs this
	// through a strict bluemonday sanitizer before injecting into the
	// `.html-slide` container, so script/iframe/form/object tags are
	// stripped. Inline `<style>` IS allowed; the LLM is told to use
	// `var(--bg/--fg/--accent/--font-*)` CSS variables so theme +
	// brand overrides keep working.
	HTML string `json:"html,omitempty"`

	// Sprint AE.7 — per-slide style overrides. Nil = inherit theme
	// defaults (the normal case). When set, the renderer emits inline
	// CSS variables on the slide's root div so per-slide typography
	// can deviate from theme without forking the theme file.
	//
	// Scales are multipliers around 1.0 (= theme default). Allowed
	// range 0.7..1.5 (validated on the tool side). Density is a
	// shorthand that simultaneously bumps padding + line-height for a
	// roomier or tighter feel without per-element tuning.
	Style *SlideStyle `json:"style,omitempty"`
}

// SlideStyle is the per-slide typography override. Each field is
// optional; zero values keep the theme default. Implemented as a
// pointer on SlideData so a slide with no override doesn't carry
// any payload (and `omitempty` keeps the JSON tight).
type SlideStyle struct {
	// TitleScale multiplies the title font-size around 1.0. Range
	// 0.7..1.5. 1.2 = "make the title 20% bigger". Renderer emits
	// --slide-title-scale CSS variable; base.css title sizes use
	// `calc(... * var(--slide-title-scale, 1))`.
	TitleScale float64 `json:"title_scale,omitempty"`
	// BodyScale multiplies body/paragraph font-size. Same range.
	BodyScale float64 `json:"body_scale,omitempty"`
	// BulletScale multiplies bullet/list font-size. Same range.
	BulletScale float64 `json:"bullet_scale,omitempty"`
	// Density is a shorthand for padding + line-height. One of
	// "compact" (tighter, ~80%), "normal" (theme default), or
	// "spacious" (looser, ~125%). Empty string means "no change".
	Density string `json:"density,omitempty"`
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

// ─── Sprint K1 types ────────────────────────────────────────────

// ProcessStep is one node in a process-flow slide. Label is the
// step name (≤ 5 words), Description is a single-sentence elaboration
// (≤ 18 words). Numbering is automatic from slice order.
type ProcessStep struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// BentoCard is one tile in a bento-grid. Size is "large" (spans 2×2)
// or "small" (1×1). The tile renders one of:
//   - Title + Body   (text card; both required)
//   - Metric         (huge number, no body)
//   - ImageQuery     (AI image fills the tile; resolved to Image)
//
// Mix freely; the LLM picks per-card based on content.
type BentoCard struct {
	Size       string `json:"size"` // "large" | "small"
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	Metric     string `json:"metric,omitempty"`
	ImageQuery string `json:"image_query,omitempty"`
	Image      string `json:"image,omitempty"` // resolved by pipeline
}

// Feature is one cell in an icon-grid. Icon is a single emoji or
// short symbol string (🚀, ⚡, ✦, etc.). Label is the feature name
// (≤ 4 words). Description is 1-2 sentences (≤ 25 words total).
type Feature struct {
	Icon        string `json:"icon"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// TeamMember is one card in a team-roster. AvatarQuery routes through
// the same image fanout as ImageQuery — typically a portrait prompt
// like "professional portrait of female founder, neutral background".
// When the fetch fails, the renderer falls back to a monogram disc
// using Name's first letter.
type TeamMember struct {
	Name        string `json:"name"`
	Role        string `json:"role"`
	Bio         string `json:"bio,omitempty"`
	AvatarQuery string `json:"avatar_query,omitempty"`
	Avatar      string `json:"avatar,omitempty"` // resolved by pipeline
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
