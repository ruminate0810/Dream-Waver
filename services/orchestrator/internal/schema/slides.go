package schema

// SlideLayout is the closed set of templated slide arrangements the renderer
// knows how to lay out. Both the LLM and the HTML templates must agree on
// these names; keep prompts/content.md and packages/slide-templates in sync.
type SlideLayout string

const (
	LayoutTitle    SlideLayout = "title"
	LayoutSection  SlideLayout = "section"
	LayoutBullets  SlideLayout = "bullets"
	LayoutContent  SlideLayout = "content"
	LayoutQuote    SlideLayout = "quote"
	LayoutTwoCol   SlideLayout = "two-column"
	LayoutData     SlideLayout = "data"
	LayoutClosing  SlideLayout = "closing"
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
