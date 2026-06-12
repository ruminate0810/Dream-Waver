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

type pgxClawRuns struct{ pool *pgxpool.Pool }

func (s *pgxClawRuns) Put(ctx context.Context, r *ClawRun) error {
	_, err := s.pool.Exec(ctx, `
		insert into claw_runs
		  (id, workspace_id, session_id, created_by, status,
		   prompt, title, plan, artifact, artifact_version,
		   figures, videos, deck, memory, error, started_at, finished_at)
		values ($1,$2,$3,nullif($4,'00000000-0000-0000-0000-000000000000'::uuid),
		        $5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		on conflict (id) do update set
		  status           = excluded.status,
		  prompt           = excluded.prompt,
		  title            = excluded.title,
		  plan             = excluded.plan,
		  artifact         = excluded.artifact,
		  artifact_version = excluded.artifact_version,
		  figures          = excluded.figures,
		  videos           = excluded.videos,
		  deck             = excluded.deck,
		  memory           = excluded.memory,
		  error            = excluded.error,
		  finished_at      = excluded.finished_at
	`,
		r.ID, r.WorkspaceID, r.SessionID, r.CreatedBy,
		r.Status, nullableText(r.Prompt), nullableText(r.Title),
		nullableJSON(r.Plan), nullableText(r.Artifact), r.ArtifactVersion,
		nullableJSON(r.Figures), nullableJSON(r.Videos), nullableJSON(r.Deck), nullableJSON(r.Memory), nullableText(r.Error),
		r.StartedAt, r.FinishedAt,
	)
	return err
}

const clawRunSelect = `
	select id, workspace_id, session_id,
	       coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid),
	       status, coalesce(prompt, ''), coalesce(title, ''),
	       plan, coalesce(artifact, ''), coalesce(artifact_version, 0),
	       figures, videos, deck, memory, coalesce(error, ''), started_at, finished_at
	from claw_runs
`

func scanClawRun(row scanner) (*ClawRun, error) {
	r := &ClawRun{}
	var plan, figures, videos, deck, memory *json.RawMessage
	err := row.Scan(
		&r.ID, &r.WorkspaceID, &r.SessionID, &r.CreatedBy,
		&r.Status, &r.Prompt, &r.Title,
		&plan, &r.Artifact, &r.ArtifactVersion,
		&figures, &videos, &deck, &memory, &r.Error, &r.StartedAt, &r.FinishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if plan != nil {
		r.Plan = *plan
	}
	if figures != nil {
		r.Figures = *figures
	}
	if videos != nil {
		r.Videos = *videos
	}
	if deck != nil {
		r.Deck = *deck
	}
	if memory != nil {
		r.Memory = *memory
	}
	return r, nil
}

func (s *pgxClawRuns) Get(ctx context.Context, workspaceID, runID uuid.UUID) (*ClawRun, error) {
	return scanClawRun(s.pool.QueryRow(ctx, clawRunSelect+`
		where workspace_id = $1 and id = $2
	`, workspaceID, runID))
}

func (s *pgxClawRuns) List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*ClawRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, clawRunSelect+`
		where workspace_id = $1
		order by started_at desc
		limit $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ClawRun{}
	for rows.Next() {
		r, err := scanClawRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *pgxClawRuns) UpdateStatus(ctx context.Context, workspaceID, runID uuid.UUID, status, errMsg string, finishedAt *time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		update claw_runs
		set status = $3,
		    error = coalesce(nullif($4, ''), error),
		    finished_at = coalesce($5, finished_at)
		where workspace_id = $1 and id = $2
	`, workspaceID, runID, status, errMsg, finishedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxClawRuns) SaveCheckpoint(ctx context.Context, workspaceID, runID uuid.UUID, memory, plan json.RawMessage, artifact string, artifactVersion int) error {
	tag, err := s.pool.Exec(ctx, `
		update claw_runs
		set memory   = coalesce($3, memory),
		    plan     = coalesce($4, plan),
		    artifact = case when $5 <> '' then $5 else artifact end,
		    artifact_version = case when $6 > 0 then $6 else artifact_version end
		where workspace_id = $1 and id = $2
	`, workspaceID, runID,
		nullableJSON(memory), nullableJSON(plan), artifact, artifactVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxClawRuns) Delete(ctx context.Context, workspaceID, runID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from claw_runs where workspace_id = $1 and id = $2`,
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

type memClawRuns struct {
	mu   lock
	rows map[uuid.UUID]*ClawRun
}

func newMemClawRuns() *memClawRuns {
	return &memClawRuns{rows: map[uuid.UUID]*ClawRun{}}
}

func (m *memClawRuns) Put(_ context.Context, r *ClawRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := *r
	stored.Plan = copyJSON(r.Plan)
	stored.Figures = copyJSON(r.Figures)
	stored.Videos = copyJSON(r.Videos)
	stored.Deck = copyJSON(r.Deck)
	stored.Memory = copyJSON(r.Memory)
	m.rows[r.ID] = &stored
	return nil
}

func (m *memClawRuns) Get(_ context.Context, workspaceID, runID uuid.UUID) (*ClawRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rows[runID]
	if !ok || r.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	out := *r
	return &out, nil
}

func (m *memClawRuns) List(_ context.Context, workspaceID uuid.UUID, limit int) ([]*ClawRun, error) {
	if limit <= 0 {
		limit = 50
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*ClawRun{}
	for _, r := range m.rows {
		if r.WorkspaceID == workspaceID {
			cp := *r
			out = append(out, &cp)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memClawRuns) UpdateStatus(_ context.Context, workspaceID, runID uuid.UUID, status, errMsg string, finishedAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[runID]
	if !ok || r.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	r.Status = status
	if errMsg != "" {
		r.Error = errMsg
	}
	if finishedAt != nil {
		r.FinishedAt = finishedAt
	}
	return nil
}

func (m *memClawRuns) SaveCheckpoint(_ context.Context, workspaceID, runID uuid.UUID, memory, plan json.RawMessage, artifact string, artifactVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[runID]
	if !ok || r.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	if memory != nil {
		r.Memory = copyJSON(memory)
	}
	if plan != nil {
		r.Plan = copyJSON(plan)
	}
	if artifact != "" {
		r.Artifact = artifact
	}
	if artifactVersion > 0 {
		r.ArtifactVersion = artifactVersion
	}
	return nil
}

func (m *memClawRuns) Delete(_ context.Context, workspaceID, runID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[runID]
	if !ok || r.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	delete(m.rows, runID)
	return nil
}
