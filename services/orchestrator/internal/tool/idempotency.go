package tool

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// WrapWithIdempotency returns a Tool whose Execute calls cache by
// (workspace_id, tool_name, canonicalised args). Within the TTL (24h
// by default), a second invocation with the same arguments returns
// the cached ToolResult without crossing into the inner tool — useful
// for both client retries and the agent loop's natural tendency to
// re-issue a call after a transient failure.
//
// Workspace id comes from ctx (set by auth middleware on every API
// request). When ctx has no workspace (e.g. internal tests, the dev-
// mode permissive auth path), idempotency is BYPASSED — falling back
// to the inner tool. That's intentional: an idempotency cache without
// tenant scoping is a multi-tenant leak.
//
// Wiring: Phase 2b will wrap the registry-level Execute in
// agent_runner.go so every tool a skill registers transparently gets
// idempotency. For now this is just the primitive; not yet hooked.
//
// Args canonicalisation:
//   - Recursive sort of all object keys (string-ordered)
//   - Strip well-known "noise" keys (`request_id`, `_ts`, `nonce`,
//     `idempotency_key`) at any depth — those vary across retries
//     but don't affect side effects.
//   - Numbers re-emitted as canonical JSON (no trailing zeros / +0).
//   - SHA-256 the result.
func WrapWithIdempotency(inner Tool, store store.IdempotencyKeys) Tool {
	return &idempotentTool{
		inner: inner,
		store: store,
		ttl:   24 * time.Hour,
	}
}

// WrapWithIdempotencyTTL is the variant with an explicit TTL — useful
// for expensive tools (Seedance video clip generation) where a longer
// cache window pays for itself.
func WrapWithIdempotencyTTL(inner Tool, store store.IdempotencyKeys, ttl time.Duration) Tool {
	return &idempotentTool{inner: inner, store: store, ttl: ttl}
}

// ─── Context key ───────────────────────────────────────────────────

// workspaceIDKey is duplicated here (not imported from auth) to avoid
// an import cycle (tool ← auth ← store ← tool). The auth package
// sets the same key shape; this declaration must match it.
//
// The contract is: any handler that runs inside the auth middleware
// MAY put a *workspaceCarrier on ctx via WorkspaceID(); tools that
// want idempotency read it via workspaceFromCtx().
type workspaceCtxKey struct{}

// WithWorkspaceID is the public way for callers OUTSIDE the auth
// middleware to attach a workspace context — used by goroutines that
// inherit a job's workspace (the slides AgentRunner goroutine, etc).
//
// The auth middleware does NOT use this — it uses its own context
// key in package auth and copies into ours via a small adapter
// installed at server boot (see auth.InstallToolBridge below — not
// yet wired, will be a Phase 2b task).
func WithWorkspaceID(ctx context.Context, workspaceID uuid.UUID) context.Context {
	return context.WithValue(ctx, workspaceCtxKey{}, workspaceID)
}

func workspaceFromCtx(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(workspaceCtxKey{}).(uuid.UUID)
	if !ok || v == uuid.Nil {
		return uuid.Nil, false
	}
	return v, true
}

// ─── Decorator ─────────────────────────────────────────────────────

type idempotentTool struct {
	inner Tool
	store store.IdempotencyKeys
	ttl   time.Duration
}

func (t *idempotentTool) Name() string                  { return t.inner.Name() }
func (t *idempotentTool) Description() string           { return t.inner.Description() }
func (t *idempotentTool) Parameters() json.RawMessage   { return t.inner.Parameters() }

func (t *idempotentTool) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	wsID, ok := workspaceFromCtx(ctx)
	if !ok {
		// No workspace on ctx → skip idempotency entirely (multi-tenant
		// leak otherwise). Logs once at debug so we can spot
		// unintended bypass during the Phase 2b rollout.
		slog.DebugContext(ctx, "idempotency: no workspace on ctx, bypassing cache", "tool", t.inner.Name())
		return t.inner.Execute(ctx, args)
	}

	hash, err := canonicalHash(args)
	if err != nil {
		// Non-JSON args: skip caching, run as normal. Don't fail the
		// tool call — fall through.
		slog.WarnContext(ctx, "idempotency: canonical hash failed, bypassing", "tool", t.inner.Name(), "err", err)
		return t.inner.Execute(ctx, args)
	}

	// Cache lookup.
	cached, err := t.store.Get(ctx, wsID, t.inner.Name(), hash)
	if err == nil {
		var result schema.ToolResult
		if jerr := json.Unmarshal(cached, &result); jerr == nil {
			slog.DebugContext(ctx, "idempotency: cache hit",
				"tool", t.inner.Name(),
				"workspace_id", wsID,
			)
			return result, nil
		}
		// Cached value corrupted (impossible in practice but defend) —
		// fall through to re-execute and overwrite.
		slog.WarnContext(ctx, "idempotency: cache entry malformed, re-running", "tool", t.inner.Name())
	} else if !errors.Is(err, store.ErrNotFound) {
		// Real DB error: log but DON'T fail the tool call — the cache
		// is an optimization, not a correctness primitive.
		slog.WarnContext(ctx, "idempotency: cache get failed, bypassing", "tool", t.inner.Name(), "err", err)
	}

	// Cache miss (or bypass): run the real tool.
	result, runErr := t.inner.Execute(ctx, args)
	if runErr != nil {
		// Don't cache tool-level errors — caller might want to retry.
		// We only cache successful tool invocations (and tool-result-
		// level errors that are deterministic, see below).
		return result, runErr
	}
	if result.Error != "" {
		// schema.ToolResult.Error is the "tool ran but the operation
		// failed" channel. Don't cache these either — most are
		// transient and the caller should retry.
		return result, nil
	}

	// Cache the successful result.
	payload, err := json.Marshal(result)
	if err == nil {
		if perr := t.store.Put(ctx, wsID, t.inner.Name(), hash, payload, time.Now().Add(t.ttl)); perr != nil {
			slog.WarnContext(ctx, "idempotency: cache put failed", "tool", t.inner.Name(), "err", perr)
		}
	}
	return result, nil
}

// ─── Canonicalisation ──────────────────────────────────────────────

// canonicalHash returns a SHA-256 over the args after:
//   - parsing the JSON,
//   - recursively sorting object keys,
//   - dropping known noise keys (request_id, _ts, nonce,
//     idempotency_key) at any depth.
//
// We use the standard library encoder rather than a deterministic
// hash function over the AST, because json.Marshal of a
// pre-canonicalised map[string]any in Go produces lexicographically-
// sorted keys (per spec since Go 1.12). One pass through marshal
// gives us a stable byte sequence.
func canonicalHash(args json.RawMessage) ([]byte, error) {
	if len(args) == 0 {
		// Empty args still need a stable hash so the cache key is
		// non-degenerate. Use a sentinel.
		h := sha256.Sum256([]byte(`null`))
		return h[:], nil
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return nil, err
	}
	clean := stripNoise(v)
	b, err := marshalCanonical(clean)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(b)
	return h[:], nil
}

// noiseKeys is the set of arg-keys stripped before hashing. These
// vary across retries but don't change side effects — keeping them in
// the hash would defeat the cache entirely for retry-aware clients.
var noiseKeys = map[string]struct{}{
	"request_id":      {},
	"_ts":             {},
	"_timestamp":      {},
	"nonce":           {},
	"idempotency_key": {},
}

// stripNoise walks the JSON tree and removes any key in noiseKeys at
// any depth. Returns a freshly-allocated structure so the caller can
// safely re-emit it.
func stripNoise(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if _, drop := noiseKeys[k]; drop {
				continue
			}
			out[k] = stripNoise(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = stripNoise(item)
		}
		return out
	default:
		return v
	}
}

// marshalCanonical serialises v with sorted object keys. json.Marshal
// since Go 1.12 already sorts map[string]any keys lexicographically,
// but I'm being explicit here so future maintainers don't accidentally
// rely on a different runtime detail.
func marshalCanonical(v any) ([]byte, error) {
	// Sort top-level array values that contain maps so re-ordered
	// argument lists (e.g. [{"a":1},{"b":2}] vs [{"b":2},{"a":1}])
	// hash the same. Cheap and predictable.
	if arr, ok := v.([]any); ok {
		sortObjectArray(arr)
	}
	return json.Marshal(v)
}

// sortObjectArray sorts an array whose elements are JSON objects, by
// the canonical-JSON encoding of each element. Stable. Non-object
// arrays are left alone (order is semantic for them, e.g. bullet lists).
func sortObjectArray(arr []any) {
	allObjects := true
	for _, e := range arr {
		if _, ok := e.(map[string]any); !ok {
			allObjects = false
			break
		}
	}
	if !allObjects {
		return
	}
	encoded := make([][]byte, len(arr))
	for i, e := range arr {
		b, _ := json.Marshal(e)
		encoded[i] = b
	}
	idx := make([]int, len(arr))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool {
		return string(encoded[idx[i]]) < string(encoded[idx[j]])
	})
	sorted := make([]any, len(arr))
	for i, oi := range idx {
		sorted[i] = arr[oi]
	}
	copy(arr, sorted)
}
