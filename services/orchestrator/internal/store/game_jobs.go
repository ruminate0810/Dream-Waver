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

type pgxGameJobs struct{ pool *pgxpool.Pool }

func (s *pgxGameJobs) Put(ctx context.Context, j *GameJob) error {
	_, err := s.pool.Exec(ctx, `
		insert into game_jobs
		  (id, workspace_id, session_id, created_by, status,
		   input, title, bytes, play_url, files, revisions, error,
		   pending, memory, started_at, finished_at)
		values ($1,$2,$3,nullif($4,'00000000-0000-0000-0000-000000000000'::uuid),
		        $5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		on conflict (id) do update set
		  status      = excluded.status,
		  input       = excluded.input,
		  title       = excluded.title,
		  bytes       = excluded.bytes,
		  play_url    = excluded.play_url,
		  files       = excluded.files,
		  revisions   = excluded.revisions,
		  error       = excluded.error,
		  pending     = excluded.pending,
		  memory      = excluded.memory,
		  finished_at = excluded.finished_at
	`,
		j.ID, j.WorkspaceID, j.SessionID, j.CreatedBy,
		j.Status, nullableJSON(j.Input),
		nullableText(j.Title), j.Bytes, nullableText(j.PlayURL),
		nullableJSON(j.Files), nullableJSON(j.Revisions),
		nullableText(j.Error),
		nullableJSON(j.Pending), nullableJSON(j.Memory),
		j.StartedAt, j.FinishedAt,
	)
	return err
}

const gameJobSelect = `
	select id, workspace_id, session_id,
	       coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid),
	       status, coalesce(input, '{}'::jsonb),
	       coalesce(title, ''), coalesce(bytes, 0), coalesce(play_url, ''),
	       files, revisions, coalesce(error, ''),
	       pending, memory, started_at, finished_at
	from game_jobs
`

func scanGameJob(row scanner) (*GameJob, error) {
	j := &GameJob{}
	var files, revisions, pending, memory *json.RawMessage
	err := row.Scan(
		&j.ID, &j.WorkspaceID, &j.SessionID, &j.CreatedBy,
		&j.Status, &j.Input,
		&j.Title, &j.Bytes, &j.PlayURL,
		&files, &revisions, &j.Error,
		&pending, &memory,
		&j.StartedAt, &j.FinishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	for _, p := range []struct {
		src *json.RawMessage
		dst *json.RawMessage
	}{
		{files, &j.Files}, {revisions, &j.Revisions},
		{pending, &j.Pending}, {memory, &j.Memory},
	} {
		if p.src != nil {
			*p.dst = *p.src
		}
	}
	return j, nil
}

func (s *pgxGameJobs) Get(ctx context.Context, workspaceID, jobID uuid.UUID) (*GameJob, error) {
	return scanGameJob(s.pool.QueryRow(ctx, gameJobSelect+`
		where workspace_id = $1 and id = $2
	`, workspaceID, jobID))
}

func (s *pgxGameJobs) List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*GameJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, gameJobSelect+`
		where workspace_id = $1
		order by started_at desc
		limit $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*GameJob{}
	for rows.Next() {
		j, err := scanGameJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *pgxGameJobs) UpdateStatus(ctx context.Context, workspaceID, jobID uuid.UUID, status, errMsg, playURL string, bytes int, finishedAt *time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		update game_jobs
		set status = $3,
		    error = coalesce(nullif($4, ''), error),
		    play_url = coalesce(nullif($5, ''), play_url),
		    bytes = case when $6 > 0 then $6 else bytes end,
		    finished_at = coalesce($7, finished_at)
		where workspace_id = $1 and id = $2
	`, workspaceID, jobID, status, errMsg, playURL, bytes, finishedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxGameJobs) SaveCheckpoint(ctx context.Context, workspaceID, jobID uuid.UUID, memory, files, revisions, pending json.RawMessage) error {
	tag, err := s.pool.Exec(ctx, `
		update game_jobs
		set memory    = coalesce($3, memory),
		    files     = coalesce($4, files),
		    revisions = coalesce($5, revisions),
		    pending   = $6
		where workspace_id = $1 and id = $2
	`, workspaceID, jobID,
		nullableJSON(memory), nullableJSON(files),
		nullableJSON(revisions), nullableJSON(pending))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxGameJobs) SavePending(ctx context.Context, workspaceID, jobID uuid.UUID, pending json.RawMessage, status string) error {
	tag, err := s.pool.Exec(ctx, `
		update game_jobs
		set pending = $3, status = $4
		where workspace_id = $1 and id = $2
	`, workspaceID, jobID, nullableJSON(pending), status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxGameJobs) Delete(ctx context.Context, workspaceID, jobID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from game_jobs where workspace_id = $1 and id = $2`,
		workspaceID, jobID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── In-memory implementation ──────────────────────────────────────

type memGameJobs struct {
	mu   lock
	rows map[uuid.UUID]*GameJob
}

func newMemGameJobs() *memGameJobs {
	return &memGameJobs{rows: map[uuid.UUID]*GameJob{}}
}

func (m *memGameJobs) Put(_ context.Context, j *GameJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := *j
	for _, p := range []struct {
		src json.RawMessage
		dst *json.RawMessage
	}{
		{j.Input, &stored.Input}, {j.Files, &stored.Files},
		{j.Revisions, &stored.Revisions}, {j.Pending, &stored.Pending},
		{j.Memory, &stored.Memory},
	} {
		*p.dst = copyJSON(p.src)
	}
	m.rows[j.ID] = &stored
	return nil
}

func (m *memGameJobs) Get(_ context.Context, workspaceID, jobID uuid.UUID) (*GameJob, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.rows[jobID]
	if !ok || j.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	out := *j
	return &out, nil
}

func (m *memGameJobs) List(_ context.Context, workspaceID uuid.UUID, limit int) ([]*GameJob, error) {
	if limit <= 0 {
		limit = 50
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*GameJob{}
	for _, j := range m.rows {
		if j.WorkspaceID == workspaceID {
			cp := *j
			out = append(out, &cp)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memGameJobs) UpdateStatus(_ context.Context, workspaceID, jobID uuid.UUID, status, errMsg, playURL string, bytes int, finishedAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.rows[jobID]
	if !ok || j.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	j.Status = status
	if errMsg != "" {
		j.Error = errMsg
	}
	if playURL != "" {
		j.PlayURL = playURL
	}
	if bytes > 0 {
		j.Bytes = bytes
	}
	if finishedAt != nil {
		j.FinishedAt = finishedAt
	}
	return nil
}

func (m *memGameJobs) SaveCheckpoint(_ context.Context, workspaceID, jobID uuid.UUID, memory, files, revisions, pending json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.rows[jobID]
	if !ok || j.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	if memory != nil {
		j.Memory = copyJSON(memory)
	}
	if files != nil {
		j.Files = copyJSON(files)
	}
	if revisions != nil {
		j.Revisions = copyJSON(revisions)
	}
	j.Pending = copyJSON(pending)
	return nil
}

func (m *memGameJobs) SavePending(_ context.Context, workspaceID, jobID uuid.UUID, pending json.RawMessage, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.rows[jobID]
	if !ok || j.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	j.Pending = copyJSON(pending)
	j.Status = status
	return nil
}

func (m *memGameJobs) Delete(_ context.Context, workspaceID, jobID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.rows[jobID]
	if !ok || j.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	delete(m.rows, jobID)
	return nil
}
