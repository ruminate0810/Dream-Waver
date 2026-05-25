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

type pgxDesignAssets struct{ pool *pgxpool.Pool }

func (s *pgxDesignAssets) Put(ctx context.Context, a *DesignAsset) error {
	// Insert-only on the hot path. We still use ON CONFLICT DO UPDATE
	// so the call is idempotent if a retry sends the same ID twice.
	_, err := s.pool.Exec(ctx, `
		insert into design_assets
		  (id, workspace_id, created_by, kind, image_url,
		   width, height, task_id, metadata, created_at)
		values ($1,$2,nullif($3,'00000000-0000-0000-0000-000000000000'::uuid),
		        $4,$5,$6,$7,$8,$9,$10)
		on conflict (id) do update set
		  image_url = excluded.image_url,
		  width     = excluded.width,
		  height    = excluded.height,
		  task_id   = excluded.task_id,
		  metadata  = excluded.metadata
	`,
		a.ID, a.WorkspaceID, a.CreatedBy,
		a.Kind, a.ImageURL,
		nullableInt(a.Width), nullableInt(a.Height),
		nullableText(a.TaskID), nullableJSON(a.Metadata),
		nullableTime(a.CreatedAt),
	)
	return err
}

const designAssetSelect = `
	select id, workspace_id,
	       coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid),
	       kind, image_url,
	       coalesce(width, 0), coalesce(height, 0),
	       coalesce(task_id, ''), metadata, created_at
	from design_assets
`

func scanDesignAsset(row scanner) (*DesignAsset, error) {
	a := &DesignAsset{}
	var metadata *json.RawMessage
	err := row.Scan(
		&a.ID, &a.WorkspaceID, &a.CreatedBy,
		&a.Kind, &a.ImageURL,
		&a.Width, &a.Height,
		&a.TaskID, &metadata, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if metadata != nil {
		a.Metadata = *metadata
	}
	return a, nil
}

func (s *pgxDesignAssets) Get(ctx context.Context, workspaceID, assetID uuid.UUID) (*DesignAsset, error) {
	return scanDesignAsset(s.pool.QueryRow(ctx, designAssetSelect+`
		where workspace_id = $1 and id = $2
	`, workspaceID, assetID))
}

func (s *pgxDesignAssets) List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*DesignAsset, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, designAssetSelect+`
		where workspace_id = $1
		order by created_at desc
		limit $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*DesignAsset{}
	for rows.Next() {
		a, err := scanDesignAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *pgxDesignAssets) Delete(ctx context.Context, workspaceID, assetID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from design_assets where workspace_id = $1 and id = $2`,
		workspaceID, assetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── In-memory implementation ──────────────────────────────────────

type memDesignAssets struct {
	mu   lock
	rows map[uuid.UUID]*DesignAsset
}

func newMemDesignAssets() *memDesignAssets {
	return &memDesignAssets{rows: map[uuid.UUID]*DesignAsset{}}
}

func (m *memDesignAssets) Put(_ context.Context, a *DesignAsset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := *a
	stored.Metadata = copyJSON(a.Metadata)
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
	}
	m.rows[a.ID] = &stored
	return nil
}

func (m *memDesignAssets) Get(_ context.Context, workspaceID, assetID uuid.UUID) (*DesignAsset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.rows[assetID]
	if !ok || a.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	out := *a
	return &out, nil
}

func (m *memDesignAssets) List(_ context.Context, workspaceID uuid.UUID, limit int) ([]*DesignAsset, error) {
	if limit <= 0 {
		limit = 100
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*DesignAsset{}
	for _, a := range m.rows {
		if a.WorkspaceID == workspaceID {
			cp := *a
			out = append(out, &cp)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memDesignAssets) Delete(_ context.Context, workspaceID, assetID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.rows[assetID]
	if !ok || a.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	delete(m.rows, assetID)
	return nil
}

// ─── Helpers — int/time variants of nullable* ──────────────────────

func nullableInt(i int) any {
	if i == 0 {
		return nil
	}
	return i
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
