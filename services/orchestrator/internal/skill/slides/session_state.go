package slides

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
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

	// Sprint X2b-2 — workspace this session belongs to. Captured at
	// job creation from the request ctx (via tool.WorkspaceID(ctx)
	// inside agent_runner.go). uuid.Nil for anonymous sessions, in
	// which case the persister below is a no-op and the session
	// lives only in process memory.
	WorkspaceID uuid.UUID

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

	// Sprint L1 — Pending carries a paused gate's payload while the
	// initial-generation goroutine has exited and the user is being
	// asked to do something (answer a clarification question, approve
	// the outline). nil = no pause active. The API resume handlers
	// read this to figure out which phase to drive next.
	Pending *PendingUserAction

	// Sprint X2b-2 — write-through persister. nil = in-memory only
	// (legacy / anonymous path). Set by SessionStore.Put when both
	// the store is configured AND state.WorkspaceID is non-Nil. Every
	// SetPending / SetMemory / SetDeck call below fans out to it
	// fire-and-forget; errors are logged but never failed up.
	persister *sessionPersister

	mu sync.Mutex
}

// PendingUserAction is a typed envelope for whatever the initial-gen
// phase is paused on. The Kind tells the resume handler which phase
// to drive next; the body fields are populated only for the matching
// kind (the rest stay zero).
type PendingUserAction struct {
	Kind PendingKind `json:"kind"`

	// For Kind == PendingClarification (legacy L1): the 1-2 questions
	// the user needs to answer before outline planning can run.
	Questions []string `json:"questions,omitempty"`

	// For Kind == PendingOutlineReview: the outline JSON the user is
	// being asked to approve. Stored as raw so the frontend's edit UX
	// can round-trip the exact same shape back when submitting.
	OutlineJSON string `json:"outline_json,omitempty"`

	// For Kind == PendingWizard: the current step's view envelope
	// (step #, total, kind, options, etc.) that the frontend
	// WizardCard renders.
	//
	// Sprint Q reshaped wizard state from "fixed 3-step script with
	// scenario + audience + extra answers" to "dynamic LLM-generated
	// script with N questions and N answers":
	//   - WizardScript carries the planner-LLM-generated question
	//     list for THIS turn (length 1-3, decided per-topic)
	//   - WizardAnswers is index-aligned with the script — answers[i]
	//     is the user's answer to script[i]; "" = skipped or pending
	//   - WizardScenario is gone (Sprint Q removed the hardcoded
	//     scenario enum — there's no fixed "first step")
	Wizard        *WizardStepView              `json:"wizard,omitempty"`
	WizardScript  []stages.ClarificationQuestion `json:"wizard_script,omitempty"`
	WizardAnswers []string                       `json:"wizard_answers,omitempty"`
}

// PendingKind enumerates the L1+N1 pause points. Closed set; adding a
// new kind also requires a Resume entry point in agent_runner.go.
type PendingKind string

const (
	// Legacy L1 — kept for backwards-compat with any in-flight job at
	// orchestrator restart. New jobs use PendingWizard instead.
	PendingClarification PendingKind = "clarification"

	PendingOutlineReview PendingKind = "outline_review"

	// Sprint N1 — multi-step pre-generation wizard. Replaces the
	// always-on L1 clarification gate. Step state lives in
	// PendingUserAction.Wizard + .WizardScenario + .WizardAnswers.
	PendingWizard PendingKind = "wizard"
)

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

// ReorderSlide moves slide [from] (0-based) so it occupies position
// [to] (0-based, in the resulting slice). Both indices must be in
// [0, len(Deck.Slides)). The same swap is applied to Content.Slides
// and Outline.Slides so all three stay length-aligned and order-aligned.
func (s *SessionState) ReorderSlide(from, to int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Deck == nil {
		return fmt.Errorf("no deck loaded")
	}
	n := len(s.Deck.Slides)
	if from < 0 || from >= n || to < 0 || to >= n {
		return fmt.Errorf("from=%d / to=%d out of range (have %d slides)", from, to, n)
	}
	if from == to {
		return nil
	}
	s.Deck.Slides = reorderSlice(s.Deck.Slides, from, to)
	if s.Content != nil {
		s.Content.Slides = reorderSlice(s.Content.Slides, from, to)
	}
	if s.Outline != nil {
		s.Outline.Slides = reorderSlice(s.Outline.Slides, from, to)
	}
	return nil
}

// DuplicateSlide inserts a deep copy of slide [index] (0-based) at
// position index+1. The new slide carries the same Template / Layout /
// Data / SpeakerNotes; the caller can immediately mutate it via
// UpdateSlide(index+1, …) if it wants the copy to diverge.
func (s *SessionState) DuplicateSlide(index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Deck == nil {
		return fmt.Errorf("no deck loaded")
	}
	if index < 0 || index >= len(s.Deck.Slides) {
		return fmt.Errorf("slide index %d out of range (have %d slides)", index, len(s.Deck.Slides))
	}
	// Deep copy the slide. SlideData is a value (no pointers except the
	// Brand/Image fields which are intentionally shared by reference —
	// the copy and original share the same hero photo URL).
	srcSlide := s.Deck.Slides[index]
	copySlide := schema.Slide{
		Template:     srcSlide.Template,
		Layout:       srcSlide.Layout,
		Data:         srcSlide.Data,
		SpeakerNotes: srcSlide.SpeakerNotes,
	}
	// Bullets is a slice — copy it so mutating the dup's bullets doesn't
	// reach back into the original.
	if len(srcSlide.Data.Bullets) > 0 {
		dupBullets := make([]string, len(srcSlide.Data.Bullets))
		copy(dupBullets, srcSlide.Data.Bullets)
		copySlide.Data.Bullets = dupBullets
	}
	insertAt := index + 1
	s.Deck.Slides = append(s.Deck.Slides[:insertAt], append([]schema.Slide{copySlide}, s.Deck.Slides[insertAt:]...)...)
	if s.Content != nil && index < len(s.Content.Slides) {
		srcC := s.Content.Slides[index]
		dupC := stages.ContentSlide{
			Index:        insertAt,
			Template:     srcC.Template,
			Layout:       srcC.Layout,
			Data:         copySlide.Data, // re-use the just-copied data
			SpeakerNotes: srcC.SpeakerNotes,
		}
		s.Content.Slides = append(s.Content.Slides[:insertAt], append([]stages.ContentSlide{dupC}, s.Content.Slides[insertAt:]...)...)
	}
	if s.Outline != nil && index < len(s.Outline.Slides) {
		srcO := s.Outline.Slides[index]
		dupO := stages.OutlineSlide{
			Index:        insertAt,
			Type:         srcO.Type,
			Headline:     srcO.Headline,
			KeyPoints:    append(json.RawMessage(nil), srcO.KeyPoints...),
			SpeakerNotes: srcO.SpeakerNotes,
		}
		s.Outline.Slides = append(s.Outline.Slides[:insertAt], append([]stages.OutlineSlide{dupO}, s.Outline.Slides[insertAt:]...)...)
	}
	s.SlideCount = len(s.Deck.Slides)
	return nil
}

// reorderSlice is a generic move-an-element-from-from-to-to operation
// that preserves the relative order of every other element. Centralised
// so Deck / Content / Outline reorder identically.
func reorderSlice[T any](xs []T, from, to int) []T {
	if from == to {
		return xs
	}
	moved := xs[from]
	// Remove from old position.
	xs = append(xs[:from], xs[from+1:]...)
	// Insert at new position. After the remove, indices >= from shifted
	// down by 1 — but `to` is meant in the RESULTING slice's coords, so
	// we don't adjust here. Bounded by len(xs) because xs is shorter now.
	if to > len(xs) {
		to = len(xs)
	}
	xs = append(xs[:to], append([]T{moved}, xs[to:]...)...)
	return xs
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
	s.Deck = d
	if d != nil {
		s.SlideCount = len(d.Slides)
	}
	p := s.persister
	memory := s.Memory
	pending := s.Pending
	s.mu.Unlock()
	if p != nil {
		// X2b-2 — write-through. Edit tools call SetDeck after every
		// successful mutation; piggy-backing the snapshot on the
		// same code path is cheaper than a separate "now save" call.
		p.saveCheckpoint(memory, d, pending)
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
//
// X2b-2 — also writes through to the store when persister is set, so a
// process bounce mid-conversation doesn't lose the agent's context.
// Best-effort: errors are logged, not propagated.
func (s *SessionState) SetMemory(msgs []schema.Message) {
	s.mu.Lock()
	s.Memory = msgs
	p := s.persister
	deck := s.Deck
	pending := s.Pending
	s.mu.Unlock()
	if p != nil {
		p.saveCheckpoint(msgs, deck, pending)
	}
}

// ─── Sprint L1: HILT pending-pause accessors ──────────────────────

// SetPending stores a paused gate's payload. Callers should also flip
// the corresponding job.Status to "awaiting_*" so the API can route
// later resume calls correctly. nil clears.
//
// X2b-2 — writes through to the store (with status "awaiting_*"
// derived from the pending kind) so a process bounce mid-wizard
// doesn't lose the user's questionnaire progress. Anonymous / no-
// workspace sessions skip persistence silently.
func (s *SessionState) SetPending(p *PendingUserAction) {
	s.mu.Lock()
	s.Pending = p
	pr := s.persister
	s.mu.Unlock()
	if pr != nil {
		pr.savePending(p, statusForPending(p))
	}
}

// GetPending returns the current pause envelope (nil if no pause active).
func (s *SessionState) GetPending() *PendingUserAction {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Pending
}

// ClearPending is convenience for `SetPending(nil)` called after a
// resume handler kicks off the next phase. Also nils the DB column
// so a later restart doesn't think the job is still paused.
func (s *SessionState) ClearPending() {
	s.SetPending(nil)
}

// statusForPending maps a pending kind to the "awaiting_*" sentinel
// the API exposes. Centralised so the API handler and the persister
// agree on the wire string.
func statusForPending(p *PendingUserAction) string {
	if p == nil {
		// nil pending → the resume handler will set the right status
		// on its own (running / finished / error). Pass "running" so
		// the DB column doesn't lie about an in-progress run.
		return "running"
	}
	switch p.Kind {
	case PendingClarification:
		return "awaiting_clarification"
	case PendingOutlineReview:
		return "awaiting_outline_approval"
	case PendingWizard:
		return "awaiting_wizard"
	}
	return "running"
}

// MergeOutlineEdits applies a user's outline-review edits onto the
// stored Outline in place. Three operations supported (matches the
// MVP scope locked at planning):
//   - rename a slide (by index) → updates outline.Slides[i].Headline
//   - delete a slide (by index) → splices it out
//   - change deck-wide theme → updates outline.Theme
//
// Edits is a small typed shape (defined in api/routes_slides.go) to
// keep the wire contract close to the HTTP boundary. The state side
// only sees the already-validated values.
func (s *SessionState) MergeOutlineEdits(edits OutlineEdits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Outline == nil {
		return
	}
	// Theme change is a deck-level field — apply first.
	if edits.Theme != "" {
		s.Outline.Theme = schema.Theme(edits.Theme)
	}
	// Apply rename / delete to slides. Deletes have to walk in reverse
	// order so we don't shift indices the user referenced.
	for _, r := range edits.Renames {
		if r.Index >= 0 && r.Index < len(s.Outline.Slides) {
			s.Outline.Slides[r.Index].Headline = r.Title
		}
	}
	if len(edits.DeleteIndices) > 0 {
		// Sort descending so each splice keeps lower indices valid.
		sorted := append([]int(nil), edits.DeleteIndices...)
		for i := 0; i < len(sorted); i++ {
			for j := i + 1; j < len(sorted); j++ {
				if sorted[j] > sorted[i] {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
		for _, idx := range sorted {
			if idx >= 0 && idx < len(s.Outline.Slides) {
				s.Outline.Slides = append(s.Outline.Slides[:idx], s.Outline.Slides[idx+1:]...)
			}
		}
	}
}

// OutlineEdits is the wire shape user submits when approving an
// outline review. Lives here (not in api/) because session_state.go
// owns the mutation logic; the api package just decodes the JSON and
// calls MergeOutlineEdits.
type OutlineEdits struct {
	Theme         string         `json:"theme,omitempty"`
	Renames       []SlideRename  `json:"renames,omitempty"`
	DeleteIndices []int          `json:"delete_indices,omitempty"`
}

// SlideRename targets one outline slide for a title rewrite.
type SlideRename struct {
	Index int    `json:"index"`
	Title string `json:"title"`
}

// SessionStore is the per-process registry keyed by job ID. Concurrent
// reads are fine; mutations on a given session are serialised through
// the session's own mutex (above), not the store-level lock.
//
// X2b-2 — when DB is configured (via NewSessionStoreWithDB), a
// SessionState's write-through hooks fire after each SetPending /
// SetMemory / SetDeck call. The in-memory map remains the hot-path
// cache; the DB is the recovery point across restarts. Anonymous
// sessions (WorkspaceID == uuid.Nil) skip persistence entirely.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*SessionState
	// db is the optional write-through + hydration backend. nil =
	// pure in-memory (legacy / dev-without-DB path).
	db store.SlideJobs
}

// NewSessionStore returns an in-memory-only store. Kept for tests +
// for the legacy code paths that haven't been updated to thread the
// store handle through.
func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*SessionState)}
}

// NewSessionStoreWithDB returns a store that ALSO persists to DB. nil
// db is allowed (degrades cleanly to in-memory) so main.go can call
// this unconditionally.
func NewSessionStoreWithDB(db store.SlideJobs) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*SessionState),
		db:       db,
	}
}

// Put registers the session for in-memory lookup. When the store is
// DB-backed AND the session has a workspace, also attaches the
// write-through persister so subsequent SetPending / SetMemory /
// SetDeck calls fan out.
func (s *SessionStore) Put(state *SessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil && state.WorkspaceID != uuid.Nil && state.persister == nil {
		if jobUUID, err := uuid.Parse(state.JobID); err == nil {
			state.persister = newSessionPersister(s.db, state.WorkspaceID, jobUUID)
			// Bootstrap the row so subsequent partial updates have a
			// target. Fire-and-forget; errors logged inside.
			state.persister.bootstrap(state)
		}
	}
	s.sessions[state.JobID] = state
}

// Get is the in-memory-only lookup (kept for backward compatibility).
// Callers that need restart-survival hydration use GetOrLoad below.
func (s *SessionStore) Get(jobID string) (*SessionState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.sessions[jobID]
	return st, ok
}

// GetOrLoad is the X2b-2 hydration entry point — on in-memory miss,
// queries the DB and rebuilds the SessionState from the persisted
// jsonb columns. Workspace must match (no cross-tenant rehydration).
// Returns (nil, false) when neither memory nor DB has the job.
//
// Use this from HILT resume handlers (POST /slides/{id}/messages)
// so a server bounce mid-wizard doesn't strand the user's
// questionnaire progress in the dead process.
func (s *SessionStore) GetOrLoad(ctx context.Context, workspaceID uuid.UUID, jobID string) (*SessionState, bool) {
	if st, ok := s.Get(jobID); ok {
		return st, true
	}
	if s.db == nil || workspaceID == uuid.Nil {
		return nil, false
	}
	jobUUID, err := uuid.Parse(jobID)
	if err != nil {
		return nil, false
	}
	row, err := s.db.Get(ctx, workspaceID, jobUUID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.WarnContext(ctx, "session hydrate query failed", "job_id", jobID, "err", err)
		}
		return nil, false
	}

	state := &SessionState{
		JobID:       jobID,
		WorkspaceID: workspaceID,
		SlideCount:  row.SlideCount,
		PptxPath:    row.PptxPath,
	}
	// Best-effort decode. Each blob is optional — a partial hydrate
	// (e.g. memory survived but deck didn't) is still useful.
	if len(row.Input) > 0 {
		_ = json.Unmarshal(row.Input, &state.Input)
	}
	if len(row.Pending) > 0 {
		var p PendingUserAction
		if err := json.Unmarshal(row.Pending, &p); err == nil {
			state.Pending = &p
		}
	}
	if len(row.Memory) > 0 {
		_ = json.Unmarshal(row.Memory, &state.Memory)
	}
	if len(row.Deck) > 0 {
		var d schema.Deck
		if err := json.Unmarshal(row.Deck, &d); err == nil {
			state.Deck = &d
		}
	}
	state.persister = newSessionPersister(s.db, workspaceID, jobUUID)

	// Cache the freshly-hydrated state so subsequent Get(jobID) calls
	// hit memory.
	s.mu.Lock()
	s.sessions[jobID] = state
	s.mu.Unlock()
	return state, true
}

func (s *SessionStore) Delete(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, jobID)
}

// ─── sessionPersister (X2b-2 write-through) ───────────────────────
//
// One persister per session. Holds the (store, workspace_id, job_id)
// tuple every write needs. All methods are best-effort: on error they
// log and return; the session continues happily in memory. This means
// a transient DB outage can't fail user-visible actions; the cost is
// "your wizard answer might not survive a restart that immediately
// follows a DB hiccup".

type sessionPersister struct {
	db          store.SlideJobs
	workspaceID uuid.UUID
	jobID       uuid.UUID
}

func newSessionPersister(db store.SlideJobs, workspaceID, jobID uuid.UUID) *sessionPersister {
	return &sessionPersister{db: db, workspaceID: workspaceID, jobID: jobID}
}

// bootstrap creates the slide_jobs row if it doesn't exist yet. Called
// from SessionStore.Put on first registration. Idempotent (uses Put's
// ON CONFLICT DO UPDATE semantics).
func (p *sessionPersister) bootstrap(state *SessionState) {
	if p == nil || p.db == nil {
		return
	}
	ctx, cancel := persistenceCtx()
	defer cancel()
	job := &store.SlideJob{
		ID:          p.jobID,
		WorkspaceID: p.workspaceID,
		SessionID:   p.jobID, // legacy: routes_slides.go derives session_id separately; this is fine until X2b-3 unifies
		Status:      "running",
		StartedAt:   time.Now().UTC(),
	}
	if inputJSON, err := json.Marshal(state.Input); err == nil {
		job.Input = inputJSON
	}
	if err := p.db.Put(ctx, job); err != nil {
		slog.WarnContext(ctx, "session bootstrap failed (continuing in-memory)",
			"job_id", p.jobID, "err", err)
	}
}

func (p *sessionPersister) saveCheckpoint(memory []schema.Message, deck *schema.Deck, pending *PendingUserAction) {
	if p == nil || p.db == nil {
		return
	}
	ctx, cancel := persistenceCtx()
	defer cancel()
	var memJSON, deckJSON, pendingJSON []byte
	if len(memory) > 0 {
		memJSON, _ = json.Marshal(memory)
	}
	if deck != nil {
		deckJSON, _ = json.Marshal(deck)
	}
	if pending != nil {
		pendingJSON, _ = json.Marshal(pending)
	}
	if err := p.db.SaveCheckpoint(ctx, p.workspaceID, p.jobID, memJSON, deckJSON, pendingJSON); err != nil {
		slog.WarnContext(ctx, "session checkpoint failed (audit only — session continues in-memory)",
			"job_id", p.jobID, "err", err)
	}
}

func (p *sessionPersister) savePending(pending *PendingUserAction, status string) {
	if p == nil || p.db == nil {
		return
	}
	ctx, cancel := persistenceCtx()
	defer cancel()
	var pendingJSON []byte
	if pending != nil {
		pendingJSON, _ = json.Marshal(pending)
	}
	if err := p.db.SavePending(ctx, p.workspaceID, p.jobID, pendingJSON, status); err != nil {
		slog.WarnContext(ctx, "session pending save failed (in-memory state still authoritative)",
			"job_id", p.jobID, "status", status, "err", err)
	}
}

// persistenceCtx caps each write at 3s so a wedged DB can't stall the
// agent goroutine. The store layer's own timeouts (pgx pool acquire +
// query) are independent.
func persistenceCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 3*time.Second)
}
