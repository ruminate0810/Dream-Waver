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

type pgxSlideJobs struct{ pool *pgxpool.Pool }

func (s *pgxSlideJobs) Put(ctx context.Context, j *SlideJob) error {
	_, err := s.pool.Exec(ctx, `
		insert into slide_jobs
		  (id, workspace_id, session_id, created_by, status, mode,
		   input, title, slide_count, pptx_path, error,
		   pending, memory, deck, started_at, finished_at)
		values ($1,$2,$3,nullif($4,'00000000-0000-0000-0000-000000000000'::uuid),
		        $5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		on conflict (id) do update set
		  status        = excluded.status,
		  mode          = excluded.mode,
		  input         = excluded.input,
		  title         = excluded.title,
		  slide_count   = excluded.slide_count,
		  pptx_path     = excluded.pptx_path,
		  error         = excluded.error,
		  pending       = excluded.pending,
		  memory        = excluded.memory,
		  deck          = excluded.deck,
		  finished_at   = excluded.finished_at
	`,
		j.ID, j.WorkspaceID, j.SessionID, j.CreatedBy,
		j.Status, nullableText(j.Mode), nullableJSON(j.Input),
		nullableText(j.Title), j.SlideCount, nullableText(j.PptxPath),
		nullableText(j.Error), nullableJSON(j.Pending),
		nullableJSON(j.Memory), nullableJSON(j.Deck),
		j.StartedAt, j.FinishedAt,
	)
	return err
}

func (s *pgxSlideJobs) Get(ctx context.Context, workspaceID, jobID uuid.UUID) (*SlideJob, error) {
	row := s.pool.QueryRow(ctx, slideJobSelect+`
		where workspace_id = $1 and id = $2
	`, workspaceID, jobID)
	return scanSlideJob(row)
}

func (s *pgxSlideJobs) List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*SlideJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, slideJobSelect+`
		where workspace_id = $1
		order by started_at desc
		limit $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SlideJob{}
	for rows.Next() {
		j, err := scanSlideJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *pgxSlideJobs) UpdateStatus(ctx context.Context, workspaceID, jobID uuid.UUID, status, errMsg, pptxPath string, finishedAt *time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		update slide_jobs
		set status = $3,
		    error = coalesce(nullif($4, ''), error),
		    pptx_path = coalesce(nullif($5, ''), pptx_path),
		    finished_at = coalesce($6, finished_at)
		where workspace_id = $1 and id = $2
	`, workspaceID, jobID, status, errMsg, pptxPath, finishedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxSlideJobs) SaveCheckpoint(ctx context.Context, workspaceID, jobID uuid.UUID, memory, deck, pending json.RawMessage) error {
	tag, err := s.pool.Exec(ctx, `
		update slide_jobs
		set memory = coalesce($3, memory),
		    deck = coalesce($4, deck),
		    pending = $5
		where workspace_id = $1 and id = $2
	`, workspaceID, jobID, nullableJSON(memory), nullableJSON(deck), nullableJSON(pending))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxSlideJobs) SavePending(ctx context.Context, workspaceID, jobID uuid.UUID, pending json.RawMessage, status string) error {
	tag, err := s.pool.Exec(ctx, `
		update slide_jobs
		set pending = $3,
		    status = $4
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

func (s *pgxSlideJobs) Delete(ctx context.Context, workspaceID, jobID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from slide_jobs where workspace_id = $1 and id = $2`,
		workspaceID, jobID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanSlideJob shares column ordering between Get / List. The unified
// select clause is below so we don't drift between them.
const slideJobSelect = `
	select id, workspace_id, session_id,
	       coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid),
	       status, coalesce(mode, ''), coalesce(input, '{}'::jsonb),
	       coalesce(title, ''), coalesce(slide_count, 0),
	       coalesce(pptx_path, ''), coalesce(error, ''),
	       pending, memory, deck, started_at, finished_at
	from slide_jobs
`

type scanner interface {
	Scan(dest ...any) error
}

func scanSlideJob(row scanner) (*SlideJob, error) {
	j := &SlideJob{}
	var pending, memory, deck *json.RawMessage
	err := row.Scan(
		&j.ID, &j.WorkspaceID, &j.SessionID, &j.CreatedBy,
		&j.Status, &j.Mode, &j.Input,
		&j.Title, &j.SlideCount,
		&j.PptxPath, &j.Error,
		&pending, &memory, &deck,
		&j.StartedAt, &j.FinishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if pending != nil {
		j.Pending = *pending
	}
	if memory != nil {
		j.Memory = *memory
	}
	if deck != nil {
		j.Deck = *deck
	}
	return j, nil
}

// ─── In-memory implementation ──────────────────────────────────────

type memSlideJobs struct {
	mu   lock
	rows map[uuid.UUID]*SlideJob
}

func newMemSlideJobs() *memSlideJobs {
	return &memSlideJobs{rows: map[uuid.UUID]*SlideJob{}}
}

func (m *memSlideJobs) Put(_ context.Context, j *SlideJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Deep-copy the json.RawMessage slices so a caller mutating the
	// passed-in struct later doesn't corrupt our stored copy.
	stored := *j
	stored.Input = copyJSON(j.Input)
	stored.Pending = copyJSON(j.Pending)
	stored.Memory = copyJSON(j.Memory)
	stored.Deck = copyJSON(j.Deck)
	m.rows[j.ID] = &stored
	return nil
}

func (m *memSlideJobs) Get(_ context.Context, workspaceID, jobID uuid.UUID) (*SlideJob, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.rows[jobID]
	if !ok || j.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	out := *j
	return &out, nil
}

func (m *memSlideJobs) List(_ context.Context, workspaceID uuid.UUID, limit int) ([]*SlideJob, error) {
	if limit <= 0 {
		limit = 50
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*SlideJob{}
	for _, j := range m.rows {
		if j.WorkspaceID == workspaceID {
			cp := *j
			out = append(out, &cp)
		}
	}
	// No stable sort in mem — production pgx sorts by started_at desc.
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memSlideJobs) UpdateStatus(_ context.Context, workspaceID, jobID uuid.UUID, status, errMsg, pptxPath string, finishedAt *time.Time) error {
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
	if pptxPath != "" {
		j.PptxPath = pptxPath
	}
	if finishedAt != nil {
		j.FinishedAt = finishedAt
	}
	return nil
}

func (m *memSlideJobs) SaveCheckpoint(_ context.Context, workspaceID, jobID uuid.UUID, memory, deck, pending json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.rows[jobID]
	if !ok || j.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	if memory != nil {
		j.Memory = copyJSON(memory)
	}
	if deck != nil {
		j.Deck = copyJSON(deck)
	}
	// Pending is always written — nil intentionally clears.
	j.Pending = copyJSON(pending)
	return nil
}

func (m *memSlideJobs) SavePending(_ context.Context, workspaceID, jobID uuid.UUID, pending json.RawMessage, status string) error {
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

func (m *memSlideJobs) Delete(_ context.Context, workspaceID, jobID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.rows[jobID]
	if !ok || j.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	delete(m.rows, jobID)
	return nil
}

// ─── Helpers shared across entity files ────────────────────────────

// nullableText turns "" into a real NULL so the DB stores absence
// distinctly from empty string. Useful for columns where empty has
// semantic meaning (e.g. mode = "" vs mode = "agent").
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableJSON does the same dance for json.RawMessage — nil or
// empty becomes SQL NULL.
func nullableJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return []byte(b)
}

// copyJSON returns a deep copy of the underlying bytes so the
// in-memory store doesn't share backing arrays with callers.
func copyJSON(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(b))
	copy(out, b)
	return out
}
