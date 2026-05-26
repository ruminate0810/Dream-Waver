package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── pgx implementation ────────────────────────────────────────────

type pgxWorkspaces struct {
	pool *pgxpool.Pool
}

// EnsurePersonal looks up the user's personal workspace; creates it
// (with a single owner member row) if not found. Idempotent — auth
// middleware calls this on every request, the steady-state cost is
// one SELECT. Returns `created=true` only on the actual insert path
// so callers can fire one-time hooks (X3b trial credit grant).
func (w *pgxWorkspaces) EnsurePersonal(ctx context.Context, userID uuid.UUID) (*Workspace, bool, error) {
	// Fast path: hit a single index lookup.
	row := w.pool.QueryRow(ctx, `
		select id, name, owner_user_id, kind, created_at
		from workspaces
		where owner_user_id = $1 and kind = 'personal'
		limit 1
	`, userID)
	out := &Workspace{}
	err := row.Scan(&out.ID, &out.Name, &out.OwnerUserID, &out.Kind, &out.CreatedAt)
	if err == nil {
		return out, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}

	// Slow path: create both workspace + membership in one txn so we
	// never end up with an orphan workspace.
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row = tx.QueryRow(ctx, `
		insert into workspaces (name, owner_user_id, kind)
		values ('Personal', $1, 'personal')
		returning id, name, owner_user_id, kind, created_at
	`, userID)
	if err := row.Scan(&out.ID, &out.Name, &out.OwnerUserID, &out.Kind, &out.CreatedAt); err != nil {
		return nil, false, err
	}
	if _, err := tx.Exec(ctx, `
		insert into workspace_members (workspace_id, user_id, role)
		values ($1, $2, 'owner')
	`, out.ID, userID); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (w *pgxWorkspaces) CreateTeam(ctx context.Context, ownerID uuid.UUID, name string) (*Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("workspace name is required")
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	out := &Workspace{}
	row := tx.QueryRow(ctx, `
		insert into workspaces (name, owner_user_id, kind)
		values ($1, $2, 'team')
		returning id, name, owner_user_id, kind, created_at
	`, name, ownerID)
	if err := row.Scan(&out.ID, &out.Name, &out.OwnerUserID, &out.Kind, &out.CreatedAt); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		insert into workspace_members (workspace_id, user_id, role)
		values ($1, $2, 'owner')
	`, out.ID, ownerID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (w *pgxWorkspaces) Get(ctx context.Context, id uuid.UUID) (*Workspace, error) {
	row := w.pool.QueryRow(ctx, `
		select id, name, owner_user_id, kind, created_at from workspaces where id = $1
	`, id)
	out := &Workspace{}
	if err := row.Scan(&out.ID, &out.Name, &out.OwnerUserID, &out.Kind, &out.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return out, nil
}

func (w *pgxWorkspaces) ListForUser(ctx context.Context, userID uuid.UUID) ([]*Workspace, error) {
	rows, err := w.pool.Query(ctx, `
		select w.id, w.name, w.owner_user_id, w.kind, w.created_at
		from workspaces w
		join workspace_members m on m.workspace_id = w.id
		where m.user_id = $1
		order by w.created_at desc
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Workspace{}
	for rows.Next() {
		w := &Workspace{}
		if err := rows.Scan(&w.ID, &w.Name, &w.OwnerUserID, &w.Kind, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (w *pgxWorkspaces) IsMember(ctx context.Context, workspaceID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := w.pool.QueryRow(ctx, `
		select exists (
			select 1 from workspace_members
			where workspace_id = $1 and user_id = $2
		)
	`, workspaceID, userID).Scan(&ok)
	return ok, err
}

func (w *pgxWorkspaces) AddMember(ctx context.Context, workspaceID, userID uuid.UUID, role string) error {
	if role == "" {
		role = "member"
	}
	_, err := w.pool.Exec(ctx, `
		insert into workspace_members (workspace_id, user_id, role)
		values ($1, $2, $3)
	`, workspaceID, userID, role)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (w *pgxWorkspaces) RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	// Cannot remove the workspace owner — they must delete the
	// workspace or transfer ownership first. Enforced here so the
	// constraint travels with the storage layer.
	var ownerID uuid.UUID
	err := w.pool.QueryRow(ctx,
		`select owner_user_id from workspaces where id = $1`, workspaceID,
	).Scan(&ownerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if ownerID == userID {
		return fmt.Errorf("cannot remove the workspace owner")
	}
	tag, err := w.pool.Exec(ctx,
		`delete from workspace_members where workspace_id = $1 and user_id = $2`,
		workspaceID, userID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (w *pgxWorkspaces) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]*WorkspaceMember, error) {
	rows, err := w.pool.Query(ctx, `
		select workspace_id, user_id, role, created_at
		from workspace_members
		where workspace_id = $1
		order by created_at asc
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*WorkspaceMember{}
	for rows.Next() {
		m := &WorkspaceMember{}
		if err := rows.Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ─── In-memory implementation ──────────────────────────────────────

type memWorkspaces struct {
	byID    map[uuid.UUID]*Workspace
	members map[uuid.UUID]map[uuid.UUID]*WorkspaceMember // workspace_id -> user_id -> member
	mu      lock
}

func newMemWorkspaces() *memWorkspaces {
	return &memWorkspaces{
		byID:    map[uuid.UUID]*Workspace{},
		members: map[uuid.UUID]map[uuid.UUID]*WorkspaceMember{},
	}
}

func (m *memWorkspaces) EnsurePersonal(_ context.Context, userID uuid.UUID) (*Workspace, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.byID {
		if w.OwnerUserID == userID && w.Kind == "personal" {
			return w, false, nil
		}
	}
	w := &Workspace{
		ID:          uuid.New(),
		Name:        "Personal",
		OwnerUserID: userID,
		Kind:        "personal",
		CreatedAt:   time.Now().UTC(),
	}
	m.byID[w.ID] = w
	if m.members[w.ID] == nil {
		m.members[w.ID] = map[uuid.UUID]*WorkspaceMember{}
	}
	m.members[w.ID][userID] = &WorkspaceMember{
		WorkspaceID: w.ID, UserID: userID, Role: "owner", CreatedAt: time.Now().UTC(),
	}
	return w, true, nil
}

func (m *memWorkspaces) CreateTeam(_ context.Context, ownerID uuid.UUID, name string) (*Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("workspace name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w := &Workspace{
		ID:          uuid.New(),
		Name:        name,
		OwnerUserID: ownerID,
		Kind:        "team",
		CreatedAt:   time.Now().UTC(),
	}
	m.byID[w.ID] = w
	m.members[w.ID] = map[uuid.UUID]*WorkspaceMember{
		ownerID: {WorkspaceID: w.ID, UserID: ownerID, Role: "owner", CreatedAt: time.Now().UTC()},
	}
	return w, nil
}

func (m *memWorkspaces) Get(_ context.Context, id uuid.UUID) (*Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if w, ok := m.byID[id]; ok {
		return w, nil
	}
	return nil, ErrNotFound
}

func (m *memWorkspaces) ListForUser(_ context.Context, userID uuid.UUID) ([]*Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*Workspace{}
	for wsID, members := range m.members {
		if _, ok := members[userID]; ok {
			if w, ok := m.byID[wsID]; ok {
				out = append(out, w)
			}
		}
	}
	// No stable sort key in the test impl — list order is unspecified.
	// In production the pgx impl sorts by created_at desc.
	return out, nil
}

func (m *memWorkspaces) IsMember(_ context.Context, workspaceID, userID uuid.UUID) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if members, ok := m.members[workspaceID]; ok {
		_, ok := members[userID]
		return ok, nil
	}
	return false, nil
}

func (m *memWorkspaces) AddMember(_ context.Context, workspaceID, userID uuid.UUID, role string) error {
	if role == "" {
		role = "member"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[workspaceID]; !ok {
		return ErrNotFound
	}
	if m.members[workspaceID] == nil {
		m.members[workspaceID] = map[uuid.UUID]*WorkspaceMember{}
	}
	if _, exists := m.members[workspaceID][userID]; exists {
		return ErrConflict
	}
	m.members[workspaceID][userID] = &WorkspaceMember{
		WorkspaceID: workspaceID, UserID: userID, Role: role, CreatedAt: time.Now().UTC(),
	}
	return nil
}

func (m *memWorkspaces) RemoveMember(_ context.Context, workspaceID, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.byID[workspaceID]
	if !ok {
		return ErrNotFound
	}
	if w.OwnerUserID == userID {
		return fmt.Errorf("cannot remove the workspace owner")
	}
	if _, exists := m.members[workspaceID][userID]; !exists {
		return ErrNotFound
	}
	delete(m.members[workspaceID], userID)
	return nil
}

func (m *memWorkspaces) ListMembers(_ context.Context, workspaceID uuid.UUID) ([]*WorkspaceMember, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	members, ok := m.members[workspaceID]
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]*WorkspaceMember, 0, len(members))
	for _, m := range members {
		out = append(out, m)
	}
	return out, nil
}
