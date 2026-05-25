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

type pgxVideoRuns struct{ pool *pgxpool.Pool }

func (s *pgxVideoRuns) Put(ctx context.Context, v *VideoRun) error {
	_, err := s.pool.Exec(ctx, `
		insert into video_runs
		  (id, workspace_id, created_by, opendream_run_id, title,
		   status, spec, started_at, finished_at)
		values ($1,$2,nullif($3,'00000000-0000-0000-0000-000000000000'::uuid),
		        $4,$5,$6,$7,$8,$9)
		on conflict (id) do update set
		  status      = excluded.status,
		  title       = excluded.title,
		  spec        = excluded.spec,
		  finished_at = excluded.finished_at
	`,
		v.ID, v.WorkspaceID, v.CreatedBy,
		v.OpendreamRunID, nullableText(v.Title),
		v.Status, nullableJSON(v.Spec),
		v.StartedAt, v.FinishedAt,
	)
	return err
}

const videoRunSelect = `
	select id, workspace_id,
	       coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid),
	       opendream_run_id, coalesce(title, ''), status,
	       spec, started_at, finished_at
	from video_runs
`

func scanVideoRun(row scanner) (*VideoRun, error) {
	v := &VideoRun{}
	var spec *json.RawMessage
	err := row.Scan(
		&v.ID, &v.WorkspaceID, &v.CreatedBy,
		&v.OpendreamRunID, &v.Title, &v.Status,
		&spec, &v.StartedAt, &v.FinishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if spec != nil {
		v.Spec = *spec
	}
	return v, nil
}

func (s *pgxVideoRuns) Get(ctx context.Context, workspaceID, runID uuid.UUID) (*VideoRun, error) {
	return scanVideoRun(s.pool.QueryRow(ctx, videoRunSelect+`
		where workspace_id = $1 and id = $2
	`, workspaceID, runID))
}

func (s *pgxVideoRuns) GetByOpendreamRunID(ctx context.Context, opendreamRunID string) (*VideoRun, error) {
	return scanVideoRun(s.pool.QueryRow(ctx, videoRunSelect+`
		where opendream_run_id = $1
		order by started_at desc
		limit 1
	`, opendreamRunID))
}

func (s *pgxVideoRuns) List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*VideoRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, videoRunSelect+`
		where workspace_id = $1
		order by started_at desc
		limit $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*VideoRun{}
	for rows.Next() {
		v, err := scanVideoRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *pgxVideoRuns) UpdateStatus(ctx context.Context, workspaceID, runID uuid.UUID, status string, finishedAt *time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		update video_runs
		set status = $3,
		    finished_at = coalesce($4, finished_at)
		where workspace_id = $1 and id = $2
	`, workspaceID, runID, status, finishedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxVideoRuns) Delete(ctx context.Context, workspaceID, runID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from video_runs where workspace_id = $1 and id = $2`,
		workspaceID, runID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── In-memory implementation ──────────────────────────────────────

type memVideoRuns struct {
	mu             lock
	rows           map[uuid.UUID]*VideoRun
	byOpendreamRun map[string]uuid.UUID
}

func newMemVideoRuns() *memVideoRuns {
	return &memVideoRuns{
		rows:           map[uuid.UUID]*VideoRun{},
		byOpendreamRun: map[string]uuid.UUID{},
	}
}

func (m *memVideoRuns) Put(_ context.Context, v *VideoRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := *v
	stored.Spec = copyJSON(v.Spec)
	m.rows[v.ID] = &stored
	if v.OpendreamRunID != "" {
		m.byOpendreamRun[v.OpendreamRunID] = v.ID
	}
	return nil
}

func (m *memVideoRuns) Get(_ context.Context, workspaceID, runID uuid.UUID) (*VideoRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.rows[runID]
	if !ok || v.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	out := *v
	return &out, nil
}

func (m *memVideoRuns) GetByOpendreamRunID(_ context.Context, opendreamRunID string) (*VideoRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byOpendreamRun[opendreamRunID]
	if !ok {
		return nil, ErrNotFound
	}
	v, ok := m.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	out := *v
	return &out, nil
}

func (m *memVideoRuns) List(_ context.Context, workspaceID uuid.UUID, limit int) ([]*VideoRun, error) {
	if limit <= 0 {
		limit = 50
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*VideoRun{}
	for _, v := range m.rows {
		if v.WorkspaceID == workspaceID {
			cp := *v
			out = append(out, &cp)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memVideoRuns) UpdateStatus(_ context.Context, workspaceID, runID uuid.UUID, status string, finishedAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.rows[runID]
	if !ok || v.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	v.Status = status
	if finishedAt != nil {
		v.FinishedAt = finishedAt
	}
	return nil
}

func (m *memVideoRuns) Delete(_ context.Context, workspaceID, runID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.rows[runID]
	if !ok || v.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	delete(m.rows, runID)
	if v.OpendreamRunID != "" {
		delete(m.byOpendreamRun, v.OpendreamRunID)
	}
	return nil
}
