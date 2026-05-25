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

type pgxIdempotencyKeys struct{ pool *pgxpool.Pool }

func (s *pgxIdempotencyKeys) Get(ctx context.Context, workspaceID uuid.UUID, toolName string, argsHash []byte) (json.RawMessage, error) {
	row := s.pool.QueryRow(ctx, `
		select result_json from idempotency_keys
		where workspace_id = $1
		  and tool_name = $2
		  and args_hash = $3
		  and ttl > now()
	`, workspaceID, toolName, argsHash)
	var out json.RawMessage
	if err := row.Scan(&out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return out, nil
}

func (s *pgxIdempotencyKeys) Put(ctx context.Context, workspaceID uuid.UUID, toolName string, argsHash []byte, result json.RawMessage, ttl time.Time) error {
	_, err := s.pool.Exec(ctx, `
		insert into idempotency_keys (workspace_id, tool_name, args_hash, result_json, ttl)
		values ($1, $2, $3, $4, $5)
		on conflict (workspace_id, tool_name, args_hash) do update
		  set result_json = excluded.result_json,
		      ttl         = excluded.ttl
	`, workspaceID, toolName, argsHash, []byte(result), ttl)
	return err
}

// ─── In-memory implementation ──────────────────────────────────────

type memIdempotencyKeys struct {
	mu   lock
	rows map[string]*idemRow // key = workspaceID|tool|hex(argsHash)
}

type idemRow struct {
	result json.RawMessage
	ttl    time.Time
}

func newMemIdempotencyKeys() *memIdempotencyKeys {
	return &memIdempotencyKeys{rows: map[string]*idemRow{}}
}

func idemKey(workspaceID uuid.UUID, toolName string, argsHash []byte) string {
	return workspaceID.String() + "|" + toolName + "|" + string(argsHash)
}

func (m *memIdempotencyKeys) Get(_ context.Context, workspaceID uuid.UUID, toolName string, argsHash []byte) (json.RawMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[idemKey(workspaceID, toolName, argsHash)]
	if !ok || time.Now().After(row.ttl) {
		return nil, ErrNotFound
	}
	return copyJSON(row.result), nil
}

func (m *memIdempotencyKeys) Put(_ context.Context, workspaceID uuid.UUID, toolName string, argsHash []byte, result json.RawMessage, ttl time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[idemKey(workspaceID, toolName, argsHash)] = &idemRow{
		result: copyJSON(result),
		ttl:    ttl,
	}
	return nil
}
