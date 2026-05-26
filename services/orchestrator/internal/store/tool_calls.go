package store

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── pgx implementation ────────────────────────────────────────────

type pgxToolCalls struct{ pool *pgxpool.Pool }

func (s *pgxToolCalls) Insert(ctx context.Context, call ToolCall) error {
	if call.ID == uuid.Nil {
		call.ID = uuid.New()
	}
	if call.CreatedAt.IsZero() {
		call.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		insert into tool_calls
		  (id, workspace_id, user_id, tool_name, args_hash,
		   result_summary, debit_amount_micro, duration_ms, attempt,
		   fallback_used, error, created_at)
		values ($1, $2,
		        nullif($3, '00000000-0000-0000-0000-000000000000'::uuid),
		        $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		call.ID, call.WorkspaceID, call.UserID,
		call.ToolName, nullableBytes(call.ArgsHash),
		call.ResultSummary, call.DebitAmountMicro,
		call.DurationMS, call.Attempt,
		call.FallbackUsed, call.Error,
		call.CreatedAt,
	)
	return err
}

func (s *pgxToolCalls) List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*ToolCall, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		select id, workspace_id,
		       coalesce(user_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       tool_name, args_hash, result_summary, debit_amount_micro,
		       duration_ms, attempt, fallback_used, error, created_at
		from tool_calls
		where workspace_id = $1
		order by created_at desc
		limit $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ToolCall{}
	for rows.Next() {
		c := &ToolCall{}
		if err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.UserID,
			&c.ToolName, &c.ArgsHash, &c.ResultSummary,
			&c.DebitAmountMicro, &c.DurationMS, &c.Attempt,
			&c.FallbackUsed, &c.Error, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ─── In-memory implementation ──────────────────────────────────────

type memToolCalls struct {
	mu   lock
	rows map[uuid.UUID][]*ToolCall // workspace_id → list
}

func newMemToolCalls() *memToolCalls {
	return &memToolCalls{rows: map[uuid.UUID][]*ToolCall{}}
}

func (m *memToolCalls) Insert(_ context.Context, call ToolCall) error {
	if call.ID == uuid.Nil {
		call.ID = uuid.New()
	}
	if call.CreatedAt.IsZero() {
		call.CreatedAt = time.Now().UTC()
	}
	// Defensive copy of slice fields so caller mutations don't
	// leak. ArgsHash is the only []byte here.
	if len(call.ArgsHash) > 0 {
		h := make([]byte, len(call.ArgsHash))
		copy(h, call.ArgsHash)
		call.ArgsHash = h
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[call.WorkspaceID] = append(m.rows[call.WorkspaceID], &call)
	return nil
}

func (m *memToolCalls) List(_ context.Context, workspaceID uuid.UUID, limit int) ([]*ToolCall, error) {
	if limit <= 0 {
		limit = 100
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.rows[workspaceID]
	out := make([]*ToolCall, len(src))
	for i, c := range src {
		cp := *c
		out[i] = &cp
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// nullableBytes — pgx variant that distinguishes empty-bytes from
// nil so the column accepts SQL NULL. Lives here so tool_calls
// doesn't need to import slide_jobs.go's helpers.
func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
