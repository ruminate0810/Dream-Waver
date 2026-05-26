package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserTemplate is one row in user_templates — a saved
// theme+brand preset users can re-apply from /slides/new's
// "我的模板" tab.
type UserTemplate struct {
	ID            uuid.UUID
	WorkspaceID   uuid.UUID
	CreatedBy     uuid.UUID // uuid.Nil if creator is gone
	Name          string
	Theme         string  // schema.Theme key
	BrandPrimary  string  // #RRGGBB, may be empty
	BrandAccent   string  // optional
	FontFamily    string  // optional, e.g. "Source Han Sans SC, sans-serif"
	CreatedAt     time.Time
}

// UserTemplates is the persistence boundary. Mirrors the shape of
// SlideJobs / DesignAssets — workspace-scoped reads, app-level checks
// + RLS as backstop.
type UserTemplates interface {
	List(ctx context.Context, workspaceID uuid.UUID) ([]*UserTemplate, error)
	Create(ctx context.Context, workspaceID, createdBy uuid.UUID, t *UserTemplate) (*UserTemplate, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
}

// ─── pgx implementation ────────────────────────────────────────────

type pgxUserTemplates struct{ pool *pgxpool.Pool }

func (s *pgxUserTemplates) List(ctx context.Context, workspaceID uuid.UUID) ([]*UserTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		select id, workspace_id, coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid),
		       name, theme,
		       coalesce(brand_primary, ''), coalesce(brand_accent, ''),
		       coalesce(font_family, ''),
		       created_at
		from user_templates
		where workspace_id = $1
		order by created_at desc
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*UserTemplate{}
	for rows.Next() {
		t := &UserTemplate{}
		if err := rows.Scan(
			&t.ID, &t.WorkspaceID, &t.CreatedBy,
			&t.Name, &t.Theme,
			&t.BrandPrimary, &t.BrandAccent, &t.FontFamily,
			&t.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *pgxUserTemplates) Create(ctx context.Context, workspaceID, createdBy uuid.UUID, t *UserTemplate) (*UserTemplate, error) {
	if t == nil {
		return nil, fmt.Errorf("template is nil")
	}
	t.Name = strings.TrimSpace(t.Name)
	t.Theme = strings.TrimSpace(t.Theme)
	if t.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if t.Theme == "" {
		return nil, fmt.Errorf("theme is required")
	}
	if len(t.Name) > 120 {
		t.Name = t.Name[:120]
	}

	var createdByArg any
	if createdBy == uuid.Nil {
		createdByArg = nil
	} else {
		createdByArg = createdBy
	}

	row := s.pool.QueryRow(ctx, `
		insert into user_templates
		  (workspace_id, created_by, name, theme, brand_primary, brand_accent, font_family)
		values ($1, $2, $3, $4, nullif($5, ''), nullif($6, ''), nullif($7, ''))
		returning id, workspace_id,
		          coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid),
		          name, theme,
		          coalesce(brand_primary, ''), coalesce(brand_accent, ''),
		          coalesce(font_family, ''),
		          created_at
	`,
		workspaceID, createdByArg, t.Name, t.Theme,
		t.BrandPrimary, t.BrandAccent, t.FontFamily,
	)
	out := &UserTemplate{}
	if err := row.Scan(
		&out.ID, &out.WorkspaceID, &out.CreatedBy,
		&out.Name, &out.Theme,
		&out.BrandPrimary, &out.BrandAccent, &out.FontFamily,
		&out.CreatedAt,
	); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *pgxUserTemplates) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from user_templates where workspace_id = $1 and id = $2`,
		workspaceID, id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── In-memory implementation ──────────────────────────────────────

type memUserTemplates struct {
	mu   lock
	byID map[uuid.UUID]*UserTemplate
}

func newMemUserTemplates() *memUserTemplates {
	return &memUserTemplates{byID: map[uuid.UUID]*UserTemplate{}}
}

func (m *memUserTemplates) List(_ context.Context, workspaceID uuid.UUID) ([]*UserTemplate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*UserTemplate{}
	for _, t := range m.byID {
		if t.WorkspaceID == workspaceID {
			out = append(out, t)
		}
	}
	// In-memory variant doesn't bother sorting; tests don't depend on order.
	return out, nil
}

func (m *memUserTemplates) Create(_ context.Context, workspaceID, createdBy uuid.UUID, t *UserTemplate) (*UserTemplate, error) {
	if t == nil {
		return nil, fmt.Errorf("template is nil")
	}
	t.Name = strings.TrimSpace(t.Name)
	t.Theme = strings.TrimSpace(t.Theme)
	if t.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if t.Theme == "" {
		return nil, fmt.Errorf("theme is required")
	}
	if len(t.Name) > 120 {
		t.Name = t.Name[:120]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := &UserTemplate{
		ID:           uuid.New(),
		WorkspaceID:  workspaceID,
		CreatedBy:    createdBy,
		Name:         t.Name,
		Theme:        t.Theme,
		BrandPrimary: t.BrandPrimary,
		BrandAccent:  t.BrandAccent,
		FontFamily:   t.FontFamily,
		CreatedAt:    time.Now().UTC(),
	}
	m.byID[out.ID] = out
	return out, nil
}

func (m *memUserTemplates) Delete(_ context.Context, workspaceID, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byID[id]
	if !ok {
		return ErrNotFound
	}
	if t.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	delete(m.byID, id)
	return nil
}

// ─── Defensive aliases ─────────────────────────────────────────────

// Compile-time guard that both implementations satisfy the interface.
var (
	_ UserTemplates = (*pgxUserTemplates)(nil)
	_ UserTemplates = (*memUserTemplates)(nil)
)

// Avoid an unused-import warning when only one path uses pgx errors.
var _ = pgx.ErrNoRows
var _ = errors.New
