// Package store is the persistence boundary. Handlers and skill code
// depend on the interfaces declared here; concrete implementations
// live alongside (pgx for production, in-memory for tests and for
// local dev when DATABASE_URL is empty).
//
// Phase 1 scope (Sprint X1): Users + Workspaces are actually wired.
// The other entity interfaces (SlideJobs, GameJobs, VideoRuns,
// DesignAssets) are declared here so the surface is stable; Phase 2
// fills in the real implementations and migrates the in-memory
// SessionStores in slides / games over to them.
//
// Multi-tenancy invariant: every method that writes a per-entity row
// REQUIRES workspaceID as a parameter. There is deliberately no
// convenience overload that omits it — tenant isolation is a
// compile-time concern, not a code-review one.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ─── Errors ─────────────────────────────────────────────────────────

var (
	// ErrNotFound surfaces when a Get returns nothing.
	ErrNotFound = errors.New("store: not found")
	// ErrForbidden surfaces when a caller asks for a row outside their
	// workspace memberships. Distinct from ErrNotFound so the API
	// layer can choose how to leak (we currently mirror ErrForbidden
	// as 404 to avoid leaking existence).
	ErrForbidden = errors.New("store: forbidden")
	// ErrConflict surfaces on uniqueness violations (e.g. inviting a
	// user who's already a member).
	ErrConflict = errors.New("store: conflict")
	// ErrInsufficient is the billing-side equivalent of "not enough
	// credit to perform this debit". Defined here (not in billing)
	// so InsertDebit can return it without an import cycle.
	ErrInsufficient = errors.New("store: insufficient credit")
)

// ─── Entity types ───────────────────────────────────────────────────

type User struct {
	ID        uuid.UUID
	Email     string
	CreatedAt time.Time
}

type Workspace struct {
	ID          uuid.UUID
	Name        string
	OwnerUserID uuid.UUID
	Kind        string // "personal" | "team"
	CreatedAt   time.Time
}

type WorkspaceMember struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Role        string // "owner" | "admin" | "member"
	CreatedAt   time.Time
}

// ─── Interfaces ─────────────────────────────────────────────────────

// Users handles identity upsert. Supabase is the source of truth; we
// mirror the essentials (id + email) so foreign keys can reach a real
// row without round-tripping to Supabase.
type Users interface {
	// UpsertFromJWT is called by the auth middleware on every request.
	// Idempotent — second call with the same id is a no-op (or an
	// email refresh if the JWT claim changed).
	UpsertFromJWT(ctx context.Context, id uuid.UUID, email string) (*User, error)
	Get(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
}

// Workspaces handles workspace CRUD + membership.
type Workspaces interface {
	// EnsurePersonal creates the user's personal workspace if it
	// doesn't exist; returns the existing or new workspace either
	// way. Idempotent — safe to call on every login. The boolean
	// `created` distinguishes "I just made this" from "already
	// existed" so callers can fire one-time hooks (X3b's trial
	// credit grant uses this).
	EnsurePersonal(ctx context.Context, userID uuid.UUID) (workspace *Workspace, created bool, err error)
	// CreateTeam creates a team workspace owned by the caller; the
	// owner is auto-inserted into workspace_members with role "owner".
	CreateTeam(ctx context.Context, ownerID uuid.UUID, name string) (*Workspace, error)
	Get(ctx context.Context, id uuid.UUID) (*Workspace, error)
	// ListForUser returns every workspace the user is a member of,
	// newest first. Drives the workspace switcher dropdown.
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*Workspace, error)
	// IsMember is the gate every job-table read/write delegates to.
	// Cached in process for the duration of one request via the auth
	// middleware (don't call it more than once per request).
	IsMember(ctx context.Context, workspaceID, userID uuid.UUID) (bool, error)
	// AddMember inserts a membership row. Caller MUST be admin/owner
	// of workspaceID — the route handler enforces this above the store.
	AddMember(ctx context.Context, workspaceID, userID uuid.UUID, role string) error
	// RemoveMember deletes a membership row. Cannot remove the owner.
	RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error
	// ListMembers returns all member rows for a workspace.
	ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]*WorkspaceMember, error)
}

// ─── Job entity types ──────────────────────────────────────────────
//
// Phase 2a (Sprint X2a) ships the storage layer for the four verticals'
// jobs. Phase 2b swaps the in-memory SessionStores / route-level maps
// over to these. Until then, the implementations exist but the
// handlers don't read/write them yet — the swap is one focused commit.
//
// Memory / Deck / Pending columns are stored as json.RawMessage so the
// store package stays oblivious to the slides/games-specific shapes
// (no import cycle). Skill packages do the marshal/unmarshal.

// SlideJob mirrors the slide_jobs migration row.
type SlideJob struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	SessionID   uuid.UUID
	CreatedBy   uuid.UUID // uuid.Nil when unknown (legacy/dev)
	Status      string
	Mode        string // "agent" | "pipeline"
	Input       json.RawMessage
	Title       string
	SlideCount  int
	PptxPath    string
	Error       string
	Pending     json.RawMessage // PendingUserAction from the slides package
	Memory      json.RawMessage // []schema.Message snapshot
	Deck        json.RawMessage // *schema.Deck snapshot
	StartedAt   time.Time
	FinishedAt  *time.Time
}

// GameJob mirrors the game_jobs migration row.
type GameJob struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	SessionID   uuid.UUID
	CreatedBy   uuid.UUID
	Status      string
	Input       json.RawMessage
	Title       string
	Bytes       int
	PlayURL     string
	Files       json.RawMessage // map[string]string of path → contents
	Revisions   json.RawMessage // []GameRevision snapshot
	Error       string
	Pending     json.RawMessage // mirror of slides PendingUserAction (Phase 5d)
	Memory      json.RawMessage
	StartedAt   time.Time
	FinishedAt  *time.Time
}

// VideoRun mirrors the video_runs migration row. The Spec is the
// story_spec.json the user submitted; status is mirrored from the
// Opendream timeline at periodic refresh points.
type VideoRun struct {
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	CreatedBy       uuid.UUID
	OpendreamRunID  string
	Title           string
	Status          string
	Spec            json.RawMessage
	StartedAt       time.Time
	FinishedAt      *time.Time
}

// DesignAsset mirrors the design_assets migration row. Insert-mostly:
// every generation/edit creates one row; the canvas's TLDraw state
// references rows by URL.
type DesignAsset struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	CreatedBy   uuid.UUID
	Kind        string // "generate" | "variants" | "edit"
	ImageURL    string
	Width       int
	Height      int
	TaskID      string
	Metadata    json.RawMessage // prompt / source_image_url / op
	CreatedAt   time.Time
}

// ─── Job interfaces ────────────────────────────────────────────────

// SlideJobs is the persistence boundary for slide deck generations.
type SlideJobs interface {
	// Put upserts the row. Phase 2b call site: routes_slides.go
	// CreateSlides, the agent runner's status transitions.
	Put(ctx context.Context, job *SlideJob) error
	// Get enforces workspace isolation — wrong workspace returns
	// ErrNotFound (we deliberately don't leak existence).
	Get(ctx context.Context, workspaceID, jobID uuid.UUID) (*SlideJob, error)
	// List returns the most recent `limit` jobs for the workspace.
	List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*SlideJob, error)
	// UpdateStatus is the common "finish this job" partial update;
	// avoids round-tripping the whole row when only status / pptx /
	// finished_at changed.
	UpdateStatus(ctx context.Context, workspaceID, jobID uuid.UUID, status, errMsg, pptxPath string, finishedAt *time.Time) error
	// SaveCheckpoint persists the agent's working state at a turn
	// boundary. Memory + Deck + Pending land together so a process
	// bounce mid-edit doesn't desync them.
	SaveCheckpoint(ctx context.Context, workspaceID, jobID uuid.UUID, memory, deck, pending json.RawMessage) error
	// SavePending persists a HILT pause envelope + status flip in one
	// atomic write. Called from every place that flips the job into
	// awaiting_* state so resume across restarts is reliable.
	SavePending(ctx context.Context, workspaceID, jobID uuid.UUID, pending json.RawMessage, status string) error
	// Delete is for the (future) "abandon this job" UX; not currently
	// wired anywhere.
	Delete(ctx context.Context, workspaceID, jobID uuid.UUID) error
}

// GameJobs mirrors SlideJobs shape for game generations.
type GameJobs interface {
	Put(ctx context.Context, job *GameJob) error
	Get(ctx context.Context, workspaceID, jobID uuid.UUID) (*GameJob, error)
	List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*GameJob, error)
	UpdateStatus(ctx context.Context, workspaceID, jobID uuid.UUID, status, errMsg, playURL string, bytes int, finishedAt *time.Time) error
	SaveCheckpoint(ctx context.Context, workspaceID, jobID uuid.UUID, memory, files, revisions, pending json.RawMessage) error
	SavePending(ctx context.Context, workspaceID, jobID uuid.UUID, pending json.RawMessage, status string) error
	Delete(ctx context.Context, workspaceID, jobID uuid.UUID) error
}

// VideoRuns is mostly insert + lightweight status refresh. The
// Opendream side is the source of truth for the timeline — we just
// remember enough to list the user's history and remap an opendream
// run id to a workspace.
type VideoRuns interface {
	Put(ctx context.Context, run *VideoRun) error
	Get(ctx context.Context, workspaceID, runID uuid.UUID) (*VideoRun, error)
	GetByOpendreamRunID(ctx context.Context, opendreamRunID string) (*VideoRun, error)
	List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*VideoRun, error)
	UpdateStatus(ctx context.Context, workspaceID, runID uuid.UUID, status string, finishedAt *time.Time) error
	Delete(ctx context.Context, workspaceID, runID uuid.UUID) error
}

// DesignAssets is insert-only on the hot path; the canvas's TLDraw
// state is the per-user view, the table is the audit/history record.
type DesignAssets interface {
	Put(ctx context.Context, asset *DesignAsset) error
	Get(ctx context.Context, workspaceID, assetID uuid.UUID) (*DesignAsset, error)
	List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*DesignAsset, error)
	Delete(ctx context.Context, workspaceID, assetID uuid.UUID) error
}

// DesignSession is one ChatGPT-style design thread (Sprint BA): a named,
// resumable conversation carrying its own chat history + a thumbnail.
// Workspace-scoped. History is the frontend's HistoryEntry[] as JSON.
type DesignSession struct {
	ID           uuid.UUID
	WorkspaceID  uuid.UUID
	CreatedBy    uuid.UUID
	Title        string
	ThumbnailURL string
	History      json.RawMessage // HistoryEntry[] — small (URLs + text)
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// DesignSessions persists design threads. List returns metadata only
// (history omitted) so the switcher payload stays light; Get returns
// the full row including history for resume. Update is a partial
// upsert of title / thumbnail / history (whichever the caller sets);
// it always bumps updated_at.
type DesignSessions interface {
	Create(ctx context.Context, s *DesignSession) error
	List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*DesignSession, error)
	Get(ctx context.Context, workspaceID, sessionID uuid.UUID) (*DesignSession, error)
	Update(ctx context.Context, s *DesignSession) error
	Delete(ctx context.Context, workspaceID, sessionID uuid.UUID) error
}

// DesignMemoryEntry is one persistent fact the assistant remembers
// across sessions (Sprint BB), workspace-scoped. Source is "auto"
// (written by the extract/consolidate LLM pass) or "manual" (pinned
// by the user; the auto pass never deletes manual rows).
type DesignMemoryEntry struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Content     string
	Source      string // "auto" | "manual"
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DesignMemory persists cross-session memory. The extract/consolidate
// pass uses List (read current set) + Add/UpdateContent/Delete to apply
// the LLM's A.U.D.N. decisions; the panel uses List + Add(manual) +
// Delete. Scoped by workspace throughout.
type DesignMemory interface {
	List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*DesignMemoryEntry, error)
	Add(ctx context.Context, e *DesignMemoryEntry) error
	UpdateContent(ctx context.Context, workspaceID, id uuid.UUID, content string) error
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
}

// ─── Billing (X3a) ─────────────────────────────────────────────────

// CreditLedgerEntry is one row in credit_ledger. Append-only —
// returned by Insert* methods so the caller (billing.Service) can
// expose the resulting balance to its own caller.
type CreditLedgerEntry struct {
	ID            uuid.UUID
	WorkspaceID   uuid.UUID
	AmountMicro   int64 // positive = grant, negative = debit
	Reason        string
	Meta          json.RawMessage
	CreatedAt     time.Time
	// BalanceAfter is the result of `SUM(amount_micro)` taken
	// inside the same SQL statement that wrote the row, so it's
	// consistent with the row's view of the world even under
	// concurrent writers.
	BalanceAfter  int64
}

// CreditLedger is the persistence boundary for the per-workspace
// credit pool. Balance is computed from the rows; no denormalised
// counter column to lose-update.
type CreditLedger interface {
	// SumBalance returns SUM(amount_micro) for the workspace. Zero
	// for new workspaces with no ledger entries.
	SumBalance(ctx context.Context, workspaceID uuid.UUID) (int64, error)

	// InsertDebit attempts to write a negative-amount row IFF the
	// current balance covers it. Returns ErrInsufficient otherwise.
	// `amount` is given as a POSITIVE number (the store negates
	// before insert) so callers don't keep getting the sign wrong.
	InsertDebit(ctx context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) (*CreditLedgerEntry, error)

	// InsertCredit writes a positive-amount row unconditionally.
	// Used for trial grants, top-ups, refunds.
	InsertCredit(ctx context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) error

	// List returns the most recent `limit` entries for the
	// workspace, newest first. Drives the "billing history" pane.
	List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*CreditLedgerEntry, error)
}

// ToolCall is one audit row (see migrations/0003_billing.sql for
// column docs).
type ToolCall struct {
	ID               uuid.UUID
	WorkspaceID      uuid.UUID
	UserID           uuid.UUID // uuid.Nil for system flows
	ToolName         string
	ArgsHash         []byte
	ResultSummary    string
	DebitAmountMicro int64
	DurationMS       int
	Attempt          int
	FallbackUsed     string
	Error            string
	CreatedAt        time.Time
}

// ToolCalls is the audit-trail persistence for every tool invocation
// — fed by the X3b WrapWithBilling decorator. Insert-only.
type ToolCalls interface {
	Insert(ctx context.Context, call ToolCall) error
	List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*ToolCall, error)
}

// ─── Idempotency ───────────────────────────────────────────────────

// IdempotencyKeys backs the tool.WrapWithIdempotency decorator. The
// key is (workspace_id, tool_name, args_hash); the value is the
// JSON-encoded result the cached invocation returned.
type IdempotencyKeys interface {
	// Get returns the cached value if it exists and is unexpired;
	// ErrNotFound otherwise. Callers do NOT distinguish "missing"
	// from "expired" — both mean "run the tool".
	Get(ctx context.Context, workspaceID uuid.UUID, toolName string, argsHash []byte) (json.RawMessage, error)
	// Put records the result with a TTL. The store does not enforce
	// uniqueness on (workspace_id, tool_name, args_hash) — Put is
	// upsert so concurrent retries don't fail at the DB layer.
	Put(ctx context.Context, workspaceID uuid.UUID, toolName string, argsHash []byte, result json.RawMessage, ttl time.Time) error
}

// ─── Chat events ────────────────────────────────────────────────────

// ChatEvent is one append-only entry in the per-session event log.
// Mirrors event.Event but stays string-typed (Kind) and uses
// json.RawMessage for Data so the store layer doesn't depend on the
// event package (which would import cycle).
type ChatEvent struct {
	SessionID uuid.UUID       `json:"session_id"`
	Seq       int64           `json:"seq"`
	Kind      string          `json:"kind"`
	Data      json.RawMessage `json:"data"`
	At        time.Time       `json:"at"`
}

// ChatEvents is the persistence boundary for the WS event log. Sprint
// AA introduces this so /slides/[id] can render the full chat history
// on cold load (Turn[] was previously in-memory React state, lost on
// refresh) AND so WS reconnects can fetch the gap they missed instead
// of going stale.
type ChatEvents interface {
	// Append a single event. The seq must be the next monotonic
	// value for the session — callers compute it via NextSeq below.
	// Failure is non-fatal at the call site (Hub.Emit logs and
	// continues so live WS broadcast isn't blocked by DB hiccups).
	Append(ctx context.Context, ev ChatEvent) error
	// NextSeq returns the next seq value to use for this session.
	// Implemented as `coalesce(max(seq), 0) + 1` per session — small
	// race window if two writers contend, but Append's primary-key
	// constraint catches conflicts and the caller retries.
	NextSeq(ctx context.Context, sessionID uuid.UUID) (int64, error)
	// ListSince returns events strictly greater than `since`, ordered
	// by seq ascending, capped at `limit`. `since=0` returns the full
	// history.
	ListSince(ctx context.Context, sessionID uuid.UUID, since int64, limit int) ([]ChatEvent, error)
}

// ─── Aggregate Store ────────────────────────────────────────────────

// Store is the single dependency main.go threads into api.Dependencies.
// Skill code pulls only the interface it needs (e.g. AgentRunner takes
// store.Users), keeping unit-test fakes small.
type Store struct {
	Users           Users
	Workspaces      Workspaces
	SlideJobs       SlideJobs
	GameJobs        GameJobs
	VideoRuns       VideoRuns
	DesignAssets    DesignAssets
	DesignSessions  DesignSessions // BA — ChatGPT-style design threads
	DesignMemory    DesignMemory   // BB — cross-session persistent memory
	IdempotencyKeys IdempotencyKeys
	CreditLedger    CreditLedger // X3a
	ToolCalls       ToolCalls    // X3a
	UserTemplates   UserTemplates // T2 — user-saved theme/brand presets
	ChatEvents      ChatEvents    // AA.1 — WS event log for replay + persistence
	ReferenceDecks  ReferenceDecks // BR.3 — RAG corpus for outline planning

	// closer is set by the constructor that owns external resources
	// (e.g. the pgx pool). main.go defers Close on shutdown.
	closer func() error
}

func (s *Store) Close() error {
	if s == nil || s.closer == nil {
		return nil
	}
	return s.closer()
}
