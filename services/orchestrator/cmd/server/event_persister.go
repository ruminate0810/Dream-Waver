package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// chatEventPersister bridges event.Hub → store.ChatEvents (Sprint AA.1).
// It satisfies event.Persister; the Hub calls Persist() on every emit.
// We do the seq-allocation + DB write on a DETACHED goroutine so a
// slow / failing Postgres write never stalls the live WS broadcast —
// durability is best-effort, the live stream is sacred.
//
// Failures are WARN-logged and dropped. The worst case is a gap in the
// replay log; the live UX is unaffected and the next successful event
// still lands (NextSeq reads MAX from PG so a dropped write just leaves
// a seq hole, which ListSince tolerates — it orders by seq, doesn't
// require contiguity).
type chatEventPersister struct {
	store store.ChatEvents
}

func newChatEventPersister(s store.ChatEvents) *chatEventPersister {
	return &chatEventPersister{store: s}
}

func (p *chatEventPersister) Persist(ev event.Event) {
	if p == nil || p.store == nil {
		return
	}
	sessionID, err := uuid.Parse(ev.SessionID)
	if err != nil {
		// Non-UUID session id (shouldn't happen for slides) — skip.
		return
	}
	// Snapshot the fields we need before the goroutine; ev is a value
	// copy already so this is just for clarity.
	kind := string(ev.Kind)
	at := ev.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	data, mErr := json.Marshal(ev.Data)
	if mErr != nil {
		slog.Warn("chat_events: marshal event data failed", "kind", kind, "err", mErr)
		return
	}

	go func() {
		// Detached context — the request that triggered the emit may
		// already be done; we still want the event persisted. Cap at
		// 5s so a wedged DB doesn't leak goroutines forever.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		seq, err := p.store.NextSeq(ctx, sessionID)
		if err != nil {
			slog.Warn("chat_events: NextSeq failed", "session", ev.SessionID, "err", err)
			return
		}
		if err := p.store.Append(ctx, store.ChatEvent{
			SessionID: sessionID,
			Seq:       seq,
			Kind:      kind,
			Data:      data,
			At:        at,
		}); err != nil {
			slog.Warn("chat_events: append failed", "session", ev.SessionID, "seq", seq, "err", err)
		}
	}()
}
