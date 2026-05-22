package slides

import (
	"fmt"
	"sync"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

// SessionState is the per-deck working memory that lives between an initial
// generation and any number of follow-up edits. It carries everything an
// edit operation might need to read or mutate: the typed Deck the renderer
// last wrote, the structured outline + content the LLM produced so we can
// rewrite individual slides without re-planning the whole deck, and the
// conversation memory the agent will reload on the next turn.
//
// In-memory for now (keyed by job ID in SessionStore). When auth + Supabase
// land, the same shape serialises cleanly into a Postgres row — the agent
// memory is already a slice of schema.Message and the deck/content are
// already JSON-tagged.
type SessionState struct {
	JobID   string
	Input   Input

	// Last successfully rendered representations. Outline + Content are
	// the LLM-produced JSON; Deck is the assembled, image-resolved view
	// that the renderer actually consumed. Edit tools may mutate any of
	// these — they're the single source of truth for the next render.
	Outline *stages.OutlineResult
	Content *stages.ContentResult
	Deck    *schema.Deck

	// PptxPath / SlideCount mirror what the API exposes as job.pptx_path.
	// After an incremental edit, PptxPath is overwritten and stays valid
	// (we re-assemble the .pptx in place).
	PptxPath   string
	SlideCount int

	// Memory is the full ToolCallAgent message history across every turn
	// (initial run plus all follow-ups). Each new agent invocation
	// reloads this so it has full context — "用户上一句说的是…" works.
	Memory []schema.Message

	// Assets caches per-slide render output (preview PNG, bg PNG, text
	// boxes). Follow-up edits pass these to the renderer so we only
	// re-screenshot the slides that actually changed instead of the
	// whole deck.
	Assets []tool.SlideAsset

	mu sync.Mutex
}

// Lock / Unlock expose the embedded mutex so edit tools can hold the
// state during a multi-step mutation. All public methods below already
// lock internally.
func (s *SessionState) Lock()   { s.mu.Lock() }
func (s *SessionState) Unlock() { s.mu.Unlock() }

// Snapshot returns a deep-copy-shallow of the current Deck plus the
// slide count. Caller may freely read but should NOT mutate the returned
// pointer's slices — use UpdateSlide / DeleteSlide for that.
func (s *SessionState) Snapshot() (*schema.Deck, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Deck == nil {
		return &schema.Deck{}, 0
	}
	return s.Deck, len(s.Deck.Slides)
}

// Title returns the deck's title (mostly for tool log lines).
func (s *SessionState) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Deck == nil {
		return ""
	}
	return s.Deck.Title
}

// UpdateSlide applies the given mutator to slide [index] under the
// state's lock. Returns an error if the index is out of range. Edit
// tools call this to keep mutations atomic relative to other tools that
// might be reading the same state.
func (s *SessionState) UpdateSlide(index int, fn func(*schema.Slide)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Deck == nil || index < 0 || index >= len(s.Deck.Slides) {
		return fmt.Errorf("slide index %d out of range", index)
	}
	fn(&s.Deck.Slides[index])
	// Keep Content in sync so a future agent turn that re-reads Content
	// sees the latest values. The structure has the same per-slide
	// shape — just the wrapping containers differ.
	if s.Content != nil && index < len(s.Content.Slides) {
		s.Content.Slides[index].Data = s.Deck.Slides[index].Data
		s.Content.Slides[index].Layout = s.Deck.Slides[index].Layout
		s.Content.Slides[index].Template = s.Deck.Slides[index].Template
	}
	return nil
}

// DeleteSlide removes slide [index] from both the Deck and the parallel
// Content/Outline records, keeping all three in sync for the next agent
// turn. After this returns the Deck.Slides slice is one shorter.
func (s *SessionState) DeleteSlide(index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Deck == nil || index < 0 || index >= len(s.Deck.Slides) {
		return fmt.Errorf("slide index %d out of range", index)
	}
	s.Deck.Slides = append(s.Deck.Slides[:index], s.Deck.Slides[index+1:]...)
	if s.Content != nil && index < len(s.Content.Slides) {
		s.Content.Slides = append(s.Content.Slides[:index], s.Content.Slides[index+1:]...)
	}
	if s.Outline != nil && index < len(s.Outline.Slides) {
		s.Outline.Slides = append(s.Outline.Slides[:index], s.Outline.Slides[index+1:]...)
	}
	s.SlideCount = len(s.Deck.Slides)
	return nil
}

// InsertSlide is the mirror of DeleteSlide for the add_slide tool. It
// splices `s` into Deck.Slides at `index`, and inserts zero-filled
// peer entries into Content.Slides and Outline.Slides so all three
// stay length-aligned for the next agent turn.
//
// `index` ranges 0 .. len(Deck.Slides) inclusive — i.e. equal to the
// length means "append".
func (s *SessionState) InsertSlide(index int, slide schema.Slide) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Deck == nil {
		return fmt.Errorf("no deck loaded — generate the deck before inserting slides")
	}
	if index < 0 || index > len(s.Deck.Slides) {
		return fmt.Errorf("slide index %d out of range (have %d slides)", index, len(s.Deck.Slides))
	}
	// Splice into Deck.Slides.
	s.Deck.Slides = append(s.Deck.Slides[:index], append([]schema.Slide{slide}, s.Deck.Slides[index:]...)...)
	// Keep Content.Slides length-aligned with zero-filled entry. The
	// add_slide tool's caller fills SlideData from a worker LLM; this
	// entry just keeps the indexes in sync.
	if s.Content != nil {
		filler := stages.ContentSlide{
			Index:    index,
			Template: slide.Template,
			Layout:   slide.Layout,
			Data:     slide.Data,
		}
		if index >= len(s.Content.Slides) {
			s.Content.Slides = append(s.Content.Slides, filler)
		} else {
			s.Content.Slides = append(s.Content.Slides[:index], append([]stages.ContentSlide{filler}, s.Content.Slides[index:]...)...)
		}
	}
	// Outline gets a thin placeholder — title becomes the headline so a
	// future Continue() turn that re-reads outline still finds something
	// meaningful at this index.
	if s.Outline != nil {
		filler := stages.OutlineSlide{
			Index:    index,
			Type:     string(slide.Layout),
			Headline: slide.Data.Title,
		}
		if index >= len(s.Outline.Slides) {
			s.Outline.Slides = append(s.Outline.Slides, filler)
		} else {
			s.Outline.Slides = append(s.Outline.Slides[:index], append([]stages.OutlineSlide{filler}, s.Outline.Slides[index:]...)...)
		}
	}
	s.SlideCount = len(s.Deck.Slides)
	return nil
}

// SetTheme rewrites Deck.Theme and overrides every slide.Template with
// the new theme name so the next render picks up the new template family.
// Per-slide Layout is preserved (a "bullets" layout stays "bullets" —
// the new theme just provides a different visual treatment of that layout).
func (s *SessionState) SetTheme(theme schema.Theme) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Deck == nil {
		return
	}
	s.Deck.Theme = theme
	for i := range s.Deck.Slides {
		s.Deck.Slides[i].Template = string(theme)
	}
}

// SetBrand replaces Deck.Brand with the given pointer (nil clears any
// previously applied brand). The renderer injects this into every served
// slide HTML as `:root { --brand-primary: …; --brand-accent: …; … }`.
// Templates that opt into `var(--brand-*, default)` pick them up; until
// Sprint B2 upgrades the 5 templates, the variable is set but not yet
// consumed by template CSS.
func (s *SessionState) SetBrand(b *schema.Brand) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Deck == nil {
		return
	}
	s.Deck.Brand = b
}

// MarkDirty is a no-op today — the incremental renderer derives dirty
// indices from its argument list directly. We keep the method so the
// SessionAccessor interface stays expressive: in a future revision we
// might track dirty state across multiple tool calls within one turn
// and only render at the end.
func (s *SessionState) MarkDirty(_ ...int) {}

// SetOutline / SetContent are called by the initial-generation tools
// (plan_outline, write_content) so the planning JSON is available to
// follow-up edit tools without re-asking the planner LLM.
//
// We take `any` here because tools/SessionAccessor lives in the tools
// package and can't import stages — concrete *stages.OutlineResult /
// *stages.ContentResult fit through fine and we type-assert in the
// helpers below.
func (s *SessionState) SetOutline(o any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := o.(*stages.OutlineResult); ok {
		s.Outline = v
	}
}
func (s *SessionState) SetContent(c any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := c.(*stages.ContentResult); ok {
		s.Content = v
	}
}
func (s *SessionState) SetDeck(d *schema.Deck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Deck = d
	if d != nil {
		s.SlideCount = len(d.Slides)
	}
}
func (s *SessionState) SetPptxPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PptxPath = path
}

// Asset cache used by the renderer adapter — incremental renders pass
// these back in so we only re-screenshot dirty slides.
func (s *SessionState) GetAssets() []tool.SlideAsset {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Assets == nil {
		return nil
	}
	out := make([]tool.SlideAsset, len(s.Assets))
	copy(out, s.Assets)
	return out
}
func (s *SessionState) SetAssets(a []tool.SlideAsset) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Assets = a
}

// SetMemory replaces the persisted agent memory. Called by AgentRunner
// after each turn so the next Continue() can reload the full conversation.
func (s *SessionState) SetMemory(msgs []schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Memory = msgs
}

// SessionStore is the in-memory registry keyed by job ID. Concurrent reads
// are fine; mutations on a given session are serialised through the
// session's own mutex (above), not the store-level lock.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*SessionState
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*SessionState)}
}

func (s *SessionStore) Put(state *SessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[state.JobID] = state
}

func (s *SessionStore) Get(jobID string) (*SessionState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.sessions[jobID]
	return st, ok
}

func (s *SessionStore) Delete(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, jobID)
}
