package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMemIdempotency_GetMiss_ErrNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemIdempotencyKeys()

	if _, err := s.Get(ctx, uuid.New(), "any", []byte("hash")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty store should ErrNotFound, got %v", err)
	}
}

func TestMemIdempotency_PutThenGet_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemIdempotencyKeys()
	ws := uuid.New()
	hash := []byte("hash-1")
	payload := json.RawMessage(`{"ok":true,"value":42}`)

	if err := s.Put(ctx, ws, "tool_x", hash, payload, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, ws, "tool_x", hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload mismatch: %s vs %s", got, payload)
	}
}

func TestMemIdempotency_WorkspaceIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemIdempotencyKeys()
	wsA, wsB := uuid.New(), uuid.New()
	hash := []byte("hash-shared")

	_ = s.Put(ctx, wsA, "tool", hash, json.RawMessage(`"A"`), time.Now().Add(time.Hour))

	// Same hash + tool in a different workspace must miss — the
	// cache key is (workspace_id, tool, hash). Cross-tenant cache
	// pollution is a privacy bug.
	if _, err := s.Get(ctx, wsB, "tool", hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected miss across workspaces, got %v", err)
	}
}

func TestMemIdempotency_TTLExpired_Miss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemIdempotencyKeys()
	ws := uuid.New()
	hash := []byte("h")

	// Put with already-past TTL.
	_ = s.Put(ctx, ws, "tool", hash,
		json.RawMessage(`"stale"`),
		time.Now().Add(-time.Second))

	if _, err := s.Get(ctx, ws, "tool", hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected expired entry to miss, got %v", err)
	}
}

func TestMemIdempotency_Put_Upserts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemIdempotencyKeys()
	ws := uuid.New()
	hash := []byte("h")

	_ = s.Put(ctx, ws, "tool", hash, json.RawMessage(`"v1"`), time.Now().Add(time.Hour))
	_ = s.Put(ctx, ws, "tool", hash, json.RawMessage(`"v2"`), time.Now().Add(time.Hour))

	got, err := s.Get(ctx, ws, "tool", hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != `"v2"` {
		t.Fatalf("expected upsert to overwrite, got %s", got)
	}
}

func TestMemIdempotency_Put_DeepCopies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemIdempotencyKeys()
	ws := uuid.New()
	hash := []byte("h")
	payload := []byte(`{"v":1}`)

	_ = s.Put(ctx, ws, "tool", hash, payload, time.Now().Add(time.Hour))
	payload[0] = 'X' // caller mutates after Put

	got, _ := s.Get(ctx, ws, "tool", hash)
	if string(got) == `X"v":1}` {
		t.Fatalf("store kept reference to caller's payload — mutation leaked")
	}
}
