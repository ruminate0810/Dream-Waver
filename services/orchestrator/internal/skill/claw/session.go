// Package claw is the general-AI-worker vertical (Genspark-style "watch
// the workers do their jobs"). A single ToolCallAgent loop plans a set of
// sub-tasks, runs web_search / code_execute against them, and writes a
// markdown report artifact — emitting claw.plan / claw.task.update /
// claw.artifact.updated events the frontend renders as a TaskPlanCard +
// WorkerDesk + ArtifactPanel.
//
// Unlike slides (multi-stage pipeline) or games (single-shot), claw is a
// general agent loop with a small, disciplined tool set. The discipline
// lives in prompt.go; the tools live alongside this file (plan_tasks /
// update_task / write_document); the loop is built in runner.go.
//
// Persistence mirrors the games restart-recovery posture: the in-memory
// SessionStore is the hot path, store.ClawRuns is the recovery point.
// GetOrLoad rebuilds a session from the persisted row on an in-memory
// miss; the route layer re-persists at each terminal branch. Anonymous
// sessions (workspace == uuid.Nil) never persist.
package claw

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// Task status vocabulary. The frontend checklist's checked-state is
// driven solely by these (via claw.task.update events) so the plan and
// the UI can't drift.
const (
	TaskPending = "pending"
	TaskDoing   = "doing"
	TaskDone    = "done"
	TaskSkipped = "skipped"
)

// Task is one row in the plan checklist. Title is the sub-task label the
// coordinator produced; Role is the worker the coordinator assigned it to
// (researcher/engineer/designer/writer — drives which pixel-worker owns the
// row); Status is the live state the checklist reflects.
type Task struct {
	Title  string `json:"title"`
	Role   string `json:"role,omitempty"`
	Status string `json:"status"`
}

// Figure is one generated image in the work package — produced by the
// designer worker's generate_image tool. URL is HTTP-servable; Caption is a
// short zh label the report/gallery renders.
type Figure struct {
	URL     string `json:"url"`
	Caption string `json:"caption,omitempty"`
}

// Deck is the slide-deck artifact — produced by the producer worker's
// generate_deck tool. Path is the on-disk .pptx (served via GET
// /claw/{id}/deck); Title + SlideCount are display metadata.
type Deck struct {
	Path       string `json:"path"`
	Title      string `json:"title,omitempty"`
	SlideCount int    `json:"slide_count,omitempty"`
	// PreviewID is the slides-session id the pipeline registered for this
	// deck — GET /api/v1/slides/{PreviewID}/page/{n}.html serves a live
	// HTML preview of slide n (1-based). Empty on decks made before this
	// field existed.
	PreviewID string `json:"preview_id,omitempty"`
}

// Video is one generated clip in the work package — produced by the
// videographer worker's generate_video tool (image-to-video). URL is the
// servable mp4; Poster is the source figure's URL; Caption + Resolution +
// Duration are display metadata.
type Video struct {
	URL        string `json:"url"`
	Poster     string `json:"poster,omitempty"`
	Caption    string `json:"caption,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	Duration   int    `json:"duration,omitempty"`
}

// Session is the per-run working state: the conversation history the
// agent loop restores on a follow-up, the plan checklist, and the latest
// markdown artifact + its monotonic version. All access is mutex-guarded
// because the agent goroutine and the route layer's persist/snapshot read
// it concurrently.
type Session struct {
	mu              sync.RWMutex
	Prompt          string
	Title           string
	History         []schema.Message
	plan            []Task
	artifact        string // the markdown report (work-package's main artifact)
	artifactVersion int
	figures         []Figure // generated images (work-package figures)
	videos          []Video  // generated clips (work-package videos, i2v)
	deck            *Deck    // generated slide deck (work-package, optional)
	brief           string   // clarification answers (受众/深度/篇幅/格式), fed to writer + critic
	debate          string   // kickoff-debate agreed approach (协商共识), fed to writer + critic

	// Adaptive clarification gate (pause-before-planning). When the triage
	// step decides the goal is ambiguous, the run pauses here with the
	// questions; the user's answers resume it. No mid-pipeline checkpoint —
	// the pause is at the very start, before any team work.
	pendingClarify bool
	clarifyQs      []string
}

// SetPlan replaces the plan with a fresh pending checklist. Called by the
// plan_tasks tool. Returns the stored titles (clamped/trimmed copy) so the
// caller can emit the matching claw.plan event from the same source of
// truth.
func (s *Session) SetPlan(titles []string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plan = make([]Task, 0, len(titles))
	out := make([]string, 0, len(titles))
	for _, t := range titles {
		s.plan = append(s.plan, Task{Title: t, Status: TaskPending})
		out = append(out, t)
	}
	return out
}

// SetPlanTasks replaces the plan with a coordinator-built checklist where
// each row already carries its assigned role. Status is forced to pending.
// Returns parallel title/role slices so the caller emits claw.plan from the
// same source of truth.
func (s *Session) SetPlanTasks(tasks []Task) (titles, roles []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plan = make([]Task, 0, len(tasks))
	for _, t := range tasks {
		s.plan = append(s.plan, Task{Title: t.Title, Role: t.Role, Status: TaskPending})
		titles = append(titles, t.Title)
		roles = append(roles, t.Role)
	}
	return titles, roles
}

// TaskIndicesForRole returns the 1-based indices of plan rows assigned to a
// role. The coordinator uses this to flip a role's rows doing→done around
// its sub-agent run.
func (s *Session) TaskIndicesForRole(role string) []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []int
	for i, t := range s.plan {
		if t.Role == role {
			out = append(out, i+1)
		}
	}
	return out
}

// AddFigure appends a generated figure and returns the new figure count
// (used as the figure's version in the claw.artifact.updated notification).
func (s *Session) AddFigure(url, caption string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.figures = append(s.figures, Figure{URL: url, Caption: caption})
	return len(s.figures)
}

// Figures returns a copy of the work-package figures.
func (s *Session) Figures() []Figure {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Figure, len(s.figures))
	copy(out, s.figures)
	return out
}

// AddVideo appends a generated clip and returns the new video count (used as
// the video's version in the claw.artifact.updated notification).
func (s *Session) AddVideo(v Video) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.videos = append(s.videos, v)
	return len(s.videos)
}

// Videos returns a copy of the work-package videos.
func (s *Session) Videos() []Video {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Video, len(s.videos))
	copy(out, s.videos)
	return out
}

// SetDeck records the generated slide deck artifact. previewID is the
// slides-session id registered by the pipeline (live per-page HTML preview).
func (s *Session) SetDeck(path, title string, slideCount int, previewID string) {
	s.mu.Lock()
	s.deck = &Deck{Path: path, Title: title, SlideCount: slideCount, PreviewID: previewID}
	s.mu.Unlock()
}

// DeckArtifact returns the slide deck artifact, or nil if none generated.
func (s *Session) DeckArtifact() *Deck {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.deck == nil {
		return nil
	}
	d := *s.deck
	return &d
}

// UpdateTask flips one 1-based plan entry's status. Returns false when the
// index is out of range so the tool can hand the model a corrective error
// (and the model self-fixes) rather than silently no-op.
func (s *Session) UpdateTask(index1based int, status string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index1based < 1 || index1based > len(s.plan) {
		return false
	}
	s.plan[index1based-1].Status = status
	return true
}

// SetArtifact stores a new markdown report and bumps the version. Returns
// the new version + byte length so the tool can emit
// claw.artifact.updated. Version is monotonic under the lock so concurrent
// writes can't reorder.
func (s *Session) SetArtifact(markdown string) (version, bytes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifact = markdown
	s.artifactVersion++
	return s.artifactVersion, len(markdown)
}

// SetBrief records the clarification answers (受众/深度/篇幅/格式) so the
// writer and critic can tailor the report. No-op for empty.
func (s *Session) SetBrief(brief string) {
	if strings.TrimSpace(brief) == "" {
		return
	}
	s.mu.Lock()
	s.brief = brief
	s.mu.Unlock()
}

// Brief returns the captured clarification brief (empty if none).
func (s *Session) Brief() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.brief
}

// SetDebate records the agreed approach reconciled from the kickoff debate,
// so the writer and critic build on the team's协商共识. No-op for empty.
func (s *Session) SetDebate(agreed string) {
	if strings.TrimSpace(agreed) == "" {
		return
	}
	s.mu.Lock()
	s.debate = agreed
	s.mu.Unlock()
}

// Debate returns the kickoff-debate agreed approach (empty if none).
func (s *Session) Debate() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.debate
}

// SetClarifyPending records that the run is paused awaiting answers to the
// given questions.
func (s *Session) SetClarifyPending(questions []string) {
	s.mu.Lock()
	s.pendingClarify = true
	s.clarifyQs = append([]string(nil), questions...)
	s.mu.Unlock()
}

// ClearClarifyPending lifts the clarification pause (on resume).
func (s *Session) ClearClarifyPending() {
	s.mu.Lock()
	s.pendingClarify = false
	s.clarifyQs = nil
	s.mu.Unlock()
}

// IsClarifyPending reports whether the run is paused for clarification.
func (s *Session) IsClarifyPending() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingClarify
}

// ClarifyQuestions returns a copy of the pending questions (nil if none).
func (s *Session) ClarifyQuestions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.clarifyQs...)
}

// SetTitle records the run title (first plan_tasks / write_document may
// set it). No-op for empty.
func (s *Session) SetTitle(title string) {
	if title == "" {
		return
	}
	s.mu.Lock()
	s.Title = title
	s.mu.Unlock()
}

// Artifact returns the latest markdown + version under a read lock.
func (s *Session) Artifact() (markdown string, version int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.artifact, s.artifactVersion
}

// PlanSnapshot returns a copy of the plan checklist for the view layer.
func (s *Session) PlanSnapshot() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, len(s.plan))
	copy(out, s.plan)
	return out
}

// PendingTasks returns the 1-based indices of entries still pending or
// doing — the post-run guard auto-completes these so the frontend
// checklist never strands a half-checked row after the agent terminates.
func (s *Session) PendingTasks() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []int
	for i, t := range s.plan {
		if t.Status == TaskPending || t.Status == TaskDoing {
			out = append(out, i+1)
		}
	}
	return out
}

// AppendUser appends a user turn (follow-up message) to history.
func (s *Session) AppendUser(content string) {
	s.mu.Lock()
	s.History = append(s.History, schema.NewUser(content))
	s.mu.Unlock()
}

// SnapshotHistory returns a copy of the conversation history for restoring
// into a fresh agent's Memory.
func (s *Session) SnapshotHistory() []schema.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]schema.Message(nil), s.History...)
}

// SetHistory replaces the conversation history (called with the agent's
// post-run Memory snapshot so the next follow-up sees this turn).
func (s *Session) SetHistory(h []schema.Message) {
	s.mu.Lock()
	s.History = append([]schema.Message(nil), h...)
	s.mu.Unlock()
}

// SnapshotForPersist serialises the session into the blobs the route
// layer writes to store.ClawRuns: memory (history), plan, artifact, and
// version. Best-effort marshaling — a failure yields a nil blob and the
// store's coalesce keeps the prior column value.
func (s *Session) SnapshotForPersist() (memory, plan, figures, videos, deck json.RawMessage, artifact string, version int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.History) > 0 {
		memory, _ = json.Marshal(s.History)
	}
	if len(s.plan) > 0 {
		plan, _ = json.Marshal(s.plan)
	}
	if len(s.figures) > 0 {
		figures, _ = json.Marshal(s.figures)
	}
	if len(s.videos) > 0 {
		videos, _ = json.Marshal(s.videos)
	}
	if s.deck != nil {
		deck, _ = json.Marshal(s.deck)
	}
	return memory, plan, figures, videos, deck, s.artifact, s.artifactVersion
}

// ─── SessionStore ───────────────────────────────────────────────────────

// SessionStore is the jobID → Session map. When DB-backed (via
// NewSessionStoreWithDB) the in-memory map is the hot path and
// store.ClawRuns is the recovery point across restarts; GetOrLoad
// rebuilds from the persisted row on a miss.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	db       store.ClawRuns
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session)}
}

// NewSessionStoreWithDB hydrates from DB on an in-memory miss. nil db is
// allowed (degrades cleanly to in-memory) so main.go can call this
// unconditionally.
func NewSessionStoreWithDB(db store.ClawRuns) *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session), db: db}
}

func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *SessionStore) Put(id string, sess *Session) {
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
}

// GetOrLoad returns the in-memory session, or — on miss — rebuilds it from
// the persisted ClawRun row so a previously-run worker survives a restart
// (the artifact GET serves the recovered report, a follow-up re-prompts
// with the recovered history + plan). Returns (nil, false) when the store
// isn't configured, the request has no workspace, or the row doesn't
// exist. Workspace must match (the store query is workspace-scoped).
func (s *SessionStore) GetOrLoad(ctx context.Context, workspaceID uuid.UUID, jobID string) (*Session, bool) {
	if sess, ok := s.Get(jobID); ok {
		return sess, true
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
			slog.WarnContext(ctx, "claw session hydrate query failed", "job_id", jobID, "err", err)
		}
		return nil, false
	}

	sess := &Session{
		Prompt:          row.Prompt,
		Title:           row.Title,
		artifact:        row.Artifact,
		artifactVersion: row.ArtifactVersion,
	}
	if len(row.Memory) > 0 {
		_ = json.Unmarshal(row.Memory, &sess.History)
	}
	if len(row.Plan) > 0 {
		if err := json.Unmarshal(row.Plan, &sess.plan); err != nil {
			slog.WarnContext(ctx, "claw session plan malformed", "job_id", jobID, "err", err)
		}
	}
	if len(row.Figures) > 0 {
		_ = json.Unmarshal(row.Figures, &sess.figures)
	}
	if len(row.Videos) > 0 {
		_ = json.Unmarshal(row.Videos, &sess.videos)
	}
	if len(row.Deck) > 0 {
		var d Deck
		if json.Unmarshal(row.Deck, &d) == nil && d.Path != "" {
			sess.deck = &d
		}
	}

	s.mu.Lock()
	s.sessions[jobID] = sess
	s.mu.Unlock()
	return sess, true
}
