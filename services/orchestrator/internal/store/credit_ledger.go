package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── pgx implementation ────────────────────────────────────────────

type pgxCreditLedger struct{ pool *pgxpool.Pool }

func (s *pgxCreditLedger) SumBalance(ctx context.Context, workspaceID uuid.UUID) (int64, error) {
	var sum int64
	err := s.pool.QueryRow(ctx,
		`select coalesce(sum(amount_micro), 0)::bigint from credit_ledger where workspace_id = $1`,
		workspaceID,
	).Scan(&sum)
	return sum, err
}

// InsertDebit writes a negative-amount row ATOMICALLY only if the
// current balance covers it. The single-statement form is the only
// safe way to do this under concurrent writers — splitting it into
// "SELECT balance, then INSERT" leaves a race window where two
// callers each see enough balance and both succeed, taking the
// workspace negative.
//
// Returns the ErrInsufficient sentinel when the CTE produces zero
// rows (the balance check failed). The caller (billing.Service) maps
// it to billing.ErrInsufficient.
func (s *pgxCreditLedger) InsertDebit(ctx context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) (*CreditLedgerEntry, error) {
	if amount <= 0 {
		return nil, errors.New("InsertDebit: amount must be > 0 (the store negates internally)")
	}
	var metaJSON []byte
	if len(meta) > 0 {
		metaJSON, _ = json.Marshal(meta)
	}

	// The CTE pattern: compute current balance, then INSERT IFF it
	// covers the debit. RETURNING gives us the row id + new balance
	// in one round trip. Postgres's MVCC + the implicit row-level
	// SELECT lock during the INSERT makes this race-free.
	//
	// On insufficient funds the CTE produces zero rows → Scan
	// returns pgx.ErrNoRows → we map to ErrInsufficient.
	// $2 carries explicit ::bigint casts everywhere it's used in
	// arithmetic / comparison. Without them pgx sends the param as an
	// untyped "unknown", and Postgres can't resolve the unary/binary
	// `-` or `>=` operator against unknown → "operator is not unique:
	// - unknown (SQLSTATE 42725)". Casting once at each site fixes it.
	const q = `
		with bal as (
			select coalesce(sum(amount_micro), 0)::bigint as current
			from credit_ledger
			where workspace_id = $1
		)
		insert into credit_ledger (workspace_id, amount_micro, reason, meta)
		select $1, -$2::bigint, $3, $4
		from bal
		where current >= $2::bigint
		returning
			id,
			created_at,
			(select current from bal) - $2::bigint as balance_after
	`
	row := s.pool.QueryRow(ctx, q, workspaceID, amount, reason, nullableJSONBytes(metaJSON))
	entry := &CreditLedgerEntry{
		WorkspaceID: workspaceID,
		AmountMicro: -amount,
		Reason:      reason,
		Meta:        metaJSON,
	}
	if err := row.Scan(&entry.ID, &entry.CreatedAt, &entry.BalanceAfter); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInsufficient
		}
		return nil, err
	}
	return entry, nil
}

func (s *pgxCreditLedger) InsertCredit(ctx context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) error {
	if amount <= 0 {
		return errors.New("InsertCredit: amount must be > 0")
	}
	var metaJSON []byte
	if len(meta) > 0 {
		metaJSON, _ = json.Marshal(meta)
	}
	_, err := s.pool.Exec(ctx,
		`insert into credit_ledger (workspace_id, amount_micro, reason, meta)
		 values ($1, $2, $3, $4)`,
		workspaceID, amount, reason, nullableJSONBytes(metaJSON),
	)
	return err
}

func (s *pgxCreditLedger) List(ctx context.Context, workspaceID uuid.UUID, limit int) ([]*CreditLedgerEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		select id, workspace_id, amount_micro, reason, meta, created_at
		from credit_ledger
		where workspace_id = $1
		order by created_at desc
		limit $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*CreditLedgerEntry{}
	for rows.Next() {
		e := &CreditLedgerEntry{}
		var meta *json.RawMessage
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.AmountMicro, &e.Reason, &meta, &e.CreatedAt); err != nil {
			return nil, err
		}
		if meta != nil {
			e.Meta = *meta
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─── In-memory implementation ──────────────────────────────────────

type memCreditLedger struct {
	mu   lock
	rows map[uuid.UUID][]*CreditLedgerEntry
}

func newMemCreditLedger() *memCreditLedger {
	return &memCreditLedger{rows: map[uuid.UUID][]*CreditLedgerEntry{}}
}

func (m *memCreditLedger) SumBalance(_ context.Context, workspaceID uuid.UUID) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return sumEntries(m.rows[workspaceID]), nil
}

// InsertDebit holds the write lock for the entire balance-check +
// insert pair. Mirrors the pgx version's race-free guarantee under
// concurrent goroutines.
func (m *memCreditLedger) InsertDebit(_ context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) (*CreditLedgerEntry, error) {
	if amount <= 0 {
		return nil, errors.New("InsertDebit: amount must be > 0")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := sumEntries(m.rows[workspaceID])
	if current < amount {
		return nil, ErrInsufficient
	}
	var metaJSON []byte
	if len(meta) > 0 {
		metaJSON, _ = json.Marshal(meta)
	}
	e := &CreditLedgerEntry{
		ID:           uuid.New(),
		WorkspaceID:  workspaceID,
		AmountMicro:  -amount,
		Reason:       reason,
		Meta:         metaJSON,
		CreatedAt:    time.Now().UTC(),
		BalanceAfter: current - amount,
	}
	m.rows[workspaceID] = append(m.rows[workspaceID], e)
	return e, nil
}

func (m *memCreditLedger) InsertCredit(_ context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) error {
	if amount <= 0 {
		return errors.New("InsertCredit: amount must be > 0")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var metaJSON []byte
	if len(meta) > 0 {
		metaJSON, _ = json.Marshal(meta)
	}
	e := &CreditLedgerEntry{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		AmountMicro: amount,
		Reason:      reason,
		Meta:        metaJSON,
		CreatedAt:   time.Now().UTC(),
	}
	m.rows[workspaceID] = append(m.rows[workspaceID], e)
	return nil
}

func (m *memCreditLedger) List(_ context.Context, workspaceID uuid.UUID, limit int) ([]*CreditLedgerEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.rows[workspaceID]
	// Copy + sort desc by CreatedAt so callers can't mutate the
	// store's slice through the returned pointers.
	out := make([]*CreditLedgerEntry, len(src))
	for i, e := range src {
		cp := *e
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

// ─── helpers ───────────────────────────────────────────────────────

func sumEntries(entries []*CreditLedgerEntry) int64 {
	var total int64
	for _, e := range entries {
		total += e.AmountMicro
	}
	return total
}

// nullableJSONBytes is the pgx variant of nullableJSON for plain
// []byte (not json.RawMessage). Lifted here to keep the credit
// ledger file self-sufficient since the rest of nullable* live in
// slide_jobs.go.
func nullableJSONBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
