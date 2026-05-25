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
	// doesn't exist; returns the existing or new workspace either way.
	// Idempotent — safe to call on every login.
	EnsurePersonal(ctx context.Context, userID uuid.UUID) (*Workspace, error)
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

// SlideJobs / GameJobs / VideoRuns / DesignAssets are declared as
// minimal stubs in Phase 1. Phase 2 replaces the in-memory maps in
// routes_slides.go / routes_games.go with concrete pgx implementations
// satisfying these. Keeping the interfaces here means Phase 2 only
// has to wire one extra field into Dependencies.
type SlideJobs interface {
	// Phase 2 will add: Put, Get, List, Update, Delete, all taking
	// workspaceID. Declaring only the interface marker here so the
	// Store struct can carry a typed nil through Phase 1.
}

type GameJobs interface{}
type VideoRuns interface{}
type DesignAssets interface{}

// ─── Aggregate Store ────────────────────────────────────────────────

// Store is the single dependency main.go threads into api.Dependencies.
// Skill code pulls only the interface it needs (e.g. AgentRunner takes
// store.Users), keeping unit-test fakes small.
type Store struct {
	Users        Users
	Workspaces   Workspaces
	SlideJobs    SlideJobs
	GameJobs     GameJobs
	VideoRuns    VideoRuns
	DesignAssets DesignAssets

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
