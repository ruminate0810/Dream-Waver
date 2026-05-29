package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── pgx implementation ────────────────────────────────────────────

type pgxDesignSessions struct{ pool *pgxpool.Pool }

func (s *pgxDesignSessions) Create(ctx context.Context, ds *DesignSession) error {
	_, err := s.pool.Exec(ctx, `
		insert into design_sessions
		  (id, workspace_id, created_by, title, thumbnail_url, history,
		   created_at, updated_at)
		values ($1,$2,nullif($3,'00000000-0000-0000-0000-000000000000'::uuid),
		        $4,$5,coalesce($6,'[]'::jsonb),now(),now())
	`,
		ds.ID, ds.WorkspaceID, ds.CreatedBy,
		ds.Title, nullableText(ds.ThumbnailURL), nullableJSON(ds.History),
	)
	return err
}

// listSelect omits the (potentially larger) history column so the
// switcher's list call stays light. history is scanned as NULL → empty.
const designSessionListSelect = `
	select id, workspace_id,
	       coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid),
	       title, coalesce(thumbnail_url, ''), '[]'::jsonb,
	       created_at, updated_at
	from design_sessions
`

const designSessionFullSelect = `
	select id, workspace_id,
	       coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid),
	       title, coalesce(thumbnail_url, ''), coalesce(history, '[]'::jsonb),
	       created_at, updated_at
	from design_sessions
`

func scanDesignSession(row scanner) (*DesignSession, error) {
	ds := &DesignSession{}
	var history json.RawMessage
	err := row.Scan(
		&ds.ID, &ds.WorkspaceID, &ds.CreatedBy,
		&ds.Title, &ds.ThumbnailURL, &history,
		&ds.CreatedAt, &ds.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ds.History = history
	return ds, nil
}

func (s *pgxDesignSessions) List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*DesignSession, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, designSessionListSelect+`
		where workspace_id = $1
		order by updated_at desc
		limit $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*DesignSession{}
	for rows.Next() {
		ds, err := scanDesignSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ds)
	}
	return out, rows.Err()
}

func (s *pgxDesignSessions) Get(ctx context.Context, workspaceID, sessionID uuid.UUID) (*DesignSession, error) {
	return scanDesignSession(s.pool.QueryRow(ctx, designSessionFullSelect+`
		where workspace_id = $1 and id = $2
	`, workspaceID, sessionID))
}

// Update is a partial upsert: COALESCE keeps the existing column when
// the caller passes a zero value (empty title / nil history), so a
// metadata-only PATCH (rename) doesn't wipe history and a history-only
// PATCH doesn't wipe the title. updated_at always bumps. Scoped by
// workspace so a wrong-workspace id is a silent no-op (RowsAffected 0
// → ErrNotFound).
func (s *pgxDesignSessions) Update(ctx context.Context, ds *DesignSession) error {
	tag, err := s.pool.Exec(ctx, `
		update design_sessions set
		  title         = coalesce(nullif($3,''), title),
		  thumbnail_url = coalesce(nullif($4,''), thumbnail_url),
		  history       = coalesce($5, history),
		  updated_at    = now()
		where workspace_id = $1 and id = $2
	`,
		ds.WorkspaceID, ds.ID,
		ds.Title, ds.ThumbnailURL, nullableJSON(ds.History),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxDesignSessions) Delete(ctx context.Context, workspaceID, sessionID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from design_sessions where workspace_id = $1 and id = $2`,
		workspaceID, sessionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── In-memory implementation ──────────────────────────────────────

type memDesignSessions struct {
	mu   lock
	rows map[uuid.UUID]*DesignSession
}

func newMemDesignSessions() *memDesignSessions {
	return &memDesignSessions{rows: map[uuid.UUID]*DesignSession{}}
}

func (m *memDesignSessions) Create(_ context.Context, ds *DesignSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := *ds
	stored.History = copyJSON(ds.History)
	now := time.Now().UTC()
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	stored.UpdatedAt = now
	if len(stored.History) == 0 {
		stored.History = json.RawMessage("[]")
	}
	m.rows[ds.ID] = &stored
	return nil
}

func (m *memDesignSessions) List(_ context.Context, workspaceID uuid.UUID, limit int) ([]*DesignSession, error) {
	if limit <= 0 {
		limit = 100
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*DesignSession{}
	for _, ds := range m.rows {
		if ds.WorkspaceID != workspaceID {
			continue
		}
		cp := *ds
		cp.History = json.RawMessage("[]") // List omits history, like pgx
		out = append(out, &cp)
	}
	// Newest-updated first.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].UpdatedAt.Before(out[j].UpdatedAt); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memDesignSessions) Get(_ context.Context, workspaceID, sessionID uuid.UUID) (*DesignSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ds, ok := m.rows[sessionID]
	if !ok || ds.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	cp := *ds
	cp.History = copyJSON(ds.History)
	return &cp, nil
}

func (m *memDesignSessions) Update(_ context.Context, ds *DesignSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.rows[ds.ID]
	if !ok || cur.WorkspaceID != ds.WorkspaceID {
		return ErrNotFound
	}
	if ds.Title != "" {
		cur.Title = ds.Title
	}
	if ds.ThumbnailURL != "" {
		cur.ThumbnailURL = ds.ThumbnailURL
	}
	if len(ds.History) > 0 {
		cur.History = copyJSON(ds.History)
	}
	cur.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *memDesignSessions) Delete(_ context.Context, workspaceID, sessionID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ds, ok := m.rows[sessionID]
	if !ok || ds.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	delete(m.rows, sessionID)
	return nil
}

var (
	_ DesignSessions = (*pgxDesignSessions)(nil)
	_ DesignSessions = (*memDesignSessions)(nil)
)
