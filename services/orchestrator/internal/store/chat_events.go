package store

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── pgx implementation ────────────────────────────────────────────

type pgxChatEvents struct {
	pool *pgxpool.Pool
	// seqMu guards the in-process next-seq cache. NextSeq is called
	// once per emit; serialising the MAX(seq) read+increment per
	// session avoids two concurrent emits computing the same seq and
	// colliding on the (session_id, seq) primary key.
	seqMu sync.Mutex
	next  map[uuid.UUID]int64 // session → next seq to hand out
}

func newPgxChatEvents(pool *pgxpool.Pool) *pgxChatEvents {
	return &pgxChatEvents{pool: pool, next: map[uuid.UUID]int64{}}
}

func (s *pgxChatEvents) NextSeq(ctx context.Context, sessionID uuid.UUID) (int64, error) {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	if v, ok := s.next[sessionID]; ok {
		s.next[sessionID] = v + 1
		return v, nil
	}
	// Cold: read the current MAX(seq) from PG (handles a process bounce
	// mid-session — the in-memory counter resets but PG remembers).
	var maxSeq int64
	err := s.pool.QueryRow(ctx,
		`select coalesce(max(seq), 0) from chat_events where session_id = $1`,
		sessionID,
	).Scan(&maxSeq)
	if err != nil {
		return 0, err
	}
	seq := maxSeq + 1
	s.next[sessionID] = seq + 1
	return seq, nil
}

func (s *pgxChatEvents) Append(ctx context.Context, ev ChatEvent) error {
	_, err := s.pool.Exec(ctx, `
		insert into chat_events (session_id, seq, kind, data, at)
		values ($1, $2, $3, $4, $5)
		on conflict (session_id, seq) do nothing
	`, ev.SessionID, ev.Seq, ev.Kind, nullableJSON(ev.Data), ev.At)
	return err
}

func (s *pgxChatEvents) ListSince(ctx context.Context, sessionID uuid.UUID, since int64, limit int) ([]ChatEvent, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		select session_id, seq, kind, data, at
		from chat_events
		where session_id = $1 and seq > $2
		order by seq asc
		limit $3
	`, sessionID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatEvent{}
	for rows.Next() {
		var ev ChatEvent
		if err := rows.Scan(&ev.SessionID, &ev.Seq, &ev.Kind, &ev.Data, &ev.At); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ─── In-memory implementation ──────────────────────────────────────

type memChatEvents struct {
	mu   lock
	byID map[uuid.UUID][]ChatEvent // session → events (seq-ordered, append-only)
}

func newMemChatEvents() *memChatEvents {
	return &memChatEvents{byID: map[uuid.UUID][]ChatEvent{}}
}

func (m *memChatEvents) NextSeq(_ context.Context, sessionID uuid.UUID) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.byID[sessionID])) + 1, nil
}

func (m *memChatEvents) Append(_ context.Context, ev ChatEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID[ev.SessionID] = append(m.byID[ev.SessionID], ev)
	return nil
}

func (m *memChatEvents) ListSince(_ context.Context, sessionID uuid.UUID, since int64, limit int) ([]ChatEvent, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := m.byID[sessionID]
	out := []ChatEvent{}
	for _, ev := range all {
		if ev.Seq > since {
			out = append(out, ev)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// ─── Compile-time guards ───────────────────────────────────────────

var (
	_ ChatEvents = (*pgxChatEvents)(nil)
	_ ChatEvents = (*memChatEvents)(nil)
)
