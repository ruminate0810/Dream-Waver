package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// ─── canonicalHash ────────────────────────────────────────────────

func TestCanonicalHash_KeyOrderInsensitive(t *testing.T) {
	t.Parallel()
	a, err := canonicalHash(json.RawMessage(`{"a":1,"b":2,"c":3}`))
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	b, err := canonicalHash(json.RawMessage(`{"c":3,"a":1,"b":2}`))
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if string(a) != string(b) {
		// Without sorted keys, two semantically-identical payloads
		// would hash to different values and the cache would miss
		// for every legitimate retry. Catch any regression here.
		t.Fatalf("hash differs across key order: %x vs %x", a, b)
	}
}

func TestCanonicalHash_StripsNoiseKeys(t *testing.T) {
	t.Parallel()
	clean := json.RawMessage(`{"prompt":"hi"}`)
	noisy := json.RawMessage(`{"prompt":"hi","request_id":"abc","_ts":12345,"nonce":"xyz","idempotency_key":"k"}`)

	a, _ := canonicalHash(clean)
	b, _ := canonicalHash(noisy)
	if string(a) != string(b) {
		t.Fatalf("noise keys leaked into hash — retries with fresh request_id will all miss")
	}
}

func TestCanonicalHash_StripsNoiseKeys_Nested(t *testing.T) {
	t.Parallel()
	// Noise keys should be stripped at ANY depth — clients sometimes
	// embed correlation IDs under a nested envelope.
	clean := json.RawMessage(`{"args":{"prompt":"hi"}}`)
	noisy := json.RawMessage(`{"args":{"prompt":"hi","_ts":123}}`)
	a, _ := canonicalHash(clean)
	b, _ := canonicalHash(noisy)
	if string(a) != string(b) {
		t.Fatalf("nested noise key not stripped")
	}
}

func TestCanonicalHash_DifferentArgsDifferent(t *testing.T) {
	t.Parallel()
	a, _ := canonicalHash(json.RawMessage(`{"prompt":"a"}`))
	b, _ := canonicalHash(json.RawMessage(`{"prompt":"b"}`))
	if string(a) == string(b) {
		t.Fatalf("different args should not collide")
	}
}

func TestCanonicalHash_EmptyStable(t *testing.T) {
	t.Parallel()
	a, err := canonicalHash(nil)
	if err != nil {
		t.Fatalf("nil args: %v", err)
	}
	b, err := canonicalHash(json.RawMessage(``))
	if err != nil {
		t.Fatalf("empty args: %v", err)
	}
	if string(a) != string(b) || len(a) == 0 {
		t.Fatalf("empty args should produce a stable non-empty hash")
	}
}

func TestCanonicalHash_ArrayOfObjects_OrderInsensitive(t *testing.T) {
	t.Parallel()
	// Arrays of objects (e.g. variants list) should hash the same
	// regardless of element order — they represent unordered sets
	// in our usage. Plain string/number arrays keep their order
	// (semantic difference: bullet lists, scene ordering, etc.).
	a, _ := canonicalHash(json.RawMessage(`[{"a":1},{"b":2}]`))
	b, _ := canonicalHash(json.RawMessage(`[{"b":2},{"a":1}]`))
	if string(a) != string(b) {
		t.Fatalf("array-of-objects ordering should not affect hash")
	}

	// Counter-example: string array order DOES matter.
	c, _ := canonicalHash(json.RawMessage(`["a","b"]`))
	d, _ := canonicalHash(json.RawMessage(`["b","a"]`))
	if string(c) == string(d) {
		t.Fatalf("primitive array ordering should affect hash")
	}
}

// ─── Execute (decorator) ──────────────────────────────────────────

// fakeTool counts calls so we can assert "ran once + cache hit on
// the second" without timing dependence.
type fakeTool struct {
	name    string
	calls   atomic.Int32
	result  schema.ToolResult
	runErr  error
}

func (f *fakeTool) Name() string                 { return f.name }
func (f *fakeTool) Description() string          { return "fake" }
func (f *fakeTool) Parameters() json.RawMessage  { return json.RawMessage(`{}`) }
func (f *fakeTool) Execute(_ context.Context, args json.RawMessage) (schema.ToolResult, error) {
	f.calls.Add(1)
	if f.runErr != nil {
		return schema.ToolResult{}, f.runErr
	}
	return f.result, nil
}

func TestExecute_BypassesWithoutWorkspaceCtx(t *testing.T) {
	t.Parallel()
	inner := &fakeTool{name: "x", result: schema.ToolResult{Output: "out"}}
	dec := WrapWithIdempotency(inner, store.NewMemory().IdempotencyKeys)

	args := json.RawMessage(`{"a":1}`)
	for i := 0; i < 3; i++ {
		_, err := dec.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
	}
	// Without workspace on ctx the decorator bypasses the cache —
	// inner runs every time. Critical for tenant safety: a cache
	// without workspace scoping is a multi-tenant leak.
	if got := inner.calls.Load(); got != 3 {
		t.Fatalf("inner calls = %d, want 3 (no caching without workspace ctx)", got)
	}
}

func TestExecute_CacheHitWithWorkspaceCtx(t *testing.T) {
	t.Parallel()
	inner := &fakeTool{name: "x", result: schema.ToolResult{Output: "result"}}
	mem := store.NewMemory().IdempotencyKeys
	dec := WrapWithIdempotency(inner, mem)

	ctx := WithWorkspaceID(context.Background(), uuid.New())
	args := json.RawMessage(`{"prompt":"hello"}`)

	// First call → cache miss → inner runs → result cached.
	r1, _ := dec.Execute(ctx, args)
	if r1.Output != "result" {
		t.Fatalf("first call result: %+v", r1)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("after first call inner ran %d times", got)
	}

	// Second call with identical args → cache hit → inner NOT invoked.
	r2, _ := dec.Execute(ctx, args)
	if r2.Output != "result" {
		t.Fatalf("second call result: %+v", r2)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("inner ran on cache hit (calls=%d)", got)
	}

	// Different args → cache miss again → inner runs.
	other := json.RawMessage(`{"prompt":"different"}`)
	_, _ = dec.Execute(ctx, other)
	if got := inner.calls.Load(); got != 2 {
		t.Fatalf("different args should miss cache (calls=%d)", got)
	}
}

func TestExecute_WorkspaceIsolation(t *testing.T) {
	t.Parallel()
	inner := &fakeTool{name: "x", result: schema.ToolResult{Output: "v"}}
	mem := store.NewMemory().IdempotencyKeys
	dec := WrapWithIdempotency(inner, mem)

	wsA := WithWorkspaceID(context.Background(), uuid.New())
	wsB := WithWorkspaceID(context.Background(), uuid.New())
	args := json.RawMessage(`{"prompt":"shared"}`)

	_, _ = dec.Execute(wsA, args)
	_, _ = dec.Execute(wsB, args) // same args, different workspace — must MISS

	if got := inner.calls.Load(); got != 2 {
		t.Fatalf("workspace isolation broken — same args cached across workspaces (calls=%d)", got)
	}
}

func TestExecute_DoesNotCacheToolErrors(t *testing.T) {
	t.Parallel()
	// schema.ToolResult.Error != "" means "the tool ran but failed".
	// We don't want to cache these — most are transient and the
	// caller should be free to retry. Same for runtime errors from
	// Execute itself.
	inner := &fakeTool{
		name:   "x",
		result: schema.ToolResult{Error: "transient: rate limited"},
	}
	mem := store.NewMemory().IdempotencyKeys
	dec := WrapWithIdempotency(inner, mem)
	ctx := WithWorkspaceID(context.Background(), uuid.New())
	args := json.RawMessage(`{}`)

	for i := 0; i < 3; i++ {
		_, _ = dec.Execute(ctx, args)
	}
	if got := inner.calls.Load(); got != 3 {
		t.Fatalf("tool-error results were cached (calls=%d), want 3", got)
	}
}

func TestExecute_DoesNotCacheRuntimeErrors(t *testing.T) {
	t.Parallel()
	inner := &fakeTool{
		name:   "x",
		runErr: errors.New("upstream 500"),
	}
	mem := store.NewMemory().IdempotencyKeys
	dec := WrapWithIdempotency(inner, mem)
	ctx := WithWorkspaceID(context.Background(), uuid.New())
	args := json.RawMessage(`{}`)

	for i := 0; i < 3; i++ {
		_, _ = dec.Execute(ctx, args)
	}
	if got := inner.calls.Load(); got != 3 {
		t.Fatalf("runtime errors were cached (calls=%d), want 3", got)
	}
}

// ─── Compile-time check: decorator preserves Tool interface ────────
//
// Catches the "I refactored idempotentTool and broke the interface"
// regression without runtime: if WrapWithIdempotency stops returning
// a value assignable to Tool, this file fails to compile.

var _ Tool = (*idempotentTool)(nil)

// Smoke that the helper text in Name/Description/Parameters is
// inherited from the inner tool — important for the LLM-facing
// schema (the agent's tool list is built from these).
func TestDecorator_PreservesInnerMetadata(t *testing.T) {
	t.Parallel()
	inner := &fakeTool{name: "my_tool"}
	dec := WrapWithIdempotency(inner, store.NewMemory().IdempotencyKeys)
	if dec.Name() != "my_tool" {
		t.Fatalf("Name = %q", dec.Name())
	}
	// Description / Parameters delegate too — sanity check.
	if dec.Description() != "fake" {
		t.Fatalf("Description = %q", dec.Description())
	}
	if string(dec.Parameters()) != `{}` {
		t.Fatalf("Parameters = %s", dec.Parameters())
	}
	// Compile-only: dec satisfies Tool.
	var _ Tool = dec
	_ = fmt.Sprintf("%T", dec) // silence unused-var lint if any
}
