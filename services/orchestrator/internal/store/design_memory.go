package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── pgx implementation ────────────────────────────────────────────

type pgxDesignMemory struct{ pool *pgxpool.Pool }

func (s *pgxDesignMemory) List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*DesignMemoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		select id, workspace_id, content, source, created_at, updated_at
		from design_memory
		where workspace_id = $1
		order by updated_at desc
		limit $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*DesignMemoryEntry{}
	for rows.Next() {
		e := &DesignMemoryEntry{}
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.Content, &e.Source,
			&e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *pgxDesignMemory) Add(ctx context.Context, e *DesignMemoryEntry) error {
	source := e.Source
	if source != "manual" {
		source = "auto"
	}
	_, err := s.pool.Exec(ctx, `
		insert into design_memory (id, workspace_id, content, source, created_at, updated_at)
		values ($1, $2, $3, $4, now(), now())
	`, e.ID, e.WorkspaceID, e.Content, source)
	return err
}

func (s *pgxDesignMemory) UpdateContent(ctx context.Context, workspaceID, id uuid.UUID, content string) error {
	tag, err := s.pool.Exec(ctx, `
		update design_memory set content = $3, updated_at = now()
		where workspace_id = $1 and id = $2
	`, workspaceID, id, content)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgxDesignMemory) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from design_memory where workspace_id = $1 and id = $2`,
		workspaceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── In-memory implementation ──────────────────────────────────────

type memDesignMemory struct {
	mu   lock
	rows map[uuid.UUID]*DesignMemoryEntry
}

func newMemDesignMemory() *memDesignMemory {
	return &memDesignMemory{rows: map[uuid.UUID]*DesignMemoryEntry{}}
}

func (m *memDesignMemory) List(_ context.Context, workspaceID uuid.UUID, limit int) ([]*DesignMemoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*DesignMemoryEntry{}
	for _, e := range m.rows {
		if e.WorkspaceID == workspaceID {
			cp := *e
			out = append(out, &cp)
		}
	}
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

func (m *memDesignMemory) Add(_ context.Context, e *DesignMemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := *e
	if stored.Source != "manual" {
		stored.Source = "auto"
	}
	now := time.Now().UTC()
	stored.CreatedAt, stored.UpdatedAt = now, now
	m.rows[e.ID] = &stored
	return nil
}

func (m *memDesignMemory) UpdateContent(_ context.Context, workspaceID, id uuid.UUID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok || e.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	e.Content = content
	e.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *memDesignMemory) Delete(_ context.Context, workspaceID, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok || e.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	delete(m.rows, id)
	return nil
}

var (
	_ DesignMemory = (*pgxDesignMemory)(nil)
	_ DesignMemory = (*memDesignMemory)(nil)
)
