package store

import "sync"

// NewMemory builds an in-memory Store for tests and for local dev when
// DATABASE_URL is empty. Phase 1 wires Users + Workspaces only; the
// rest of the interfaces are typed-nil placeholders the Phase 2 work
// will replace.
//
// Memory store is NOT concurrent-safe across goroutines for a multi-
// member workspace — the per-entity types use sync.RWMutex via the
// `lock` alias below. Good enough for solo dev / unit tests; the pgx
// store is the real concurrency story.
func NewMemory() *Store {
	return &Store{
		Users:      newMemUsers(),
		Workspaces: newMemWorkspaces(),
		// SlideJobs / GameJobs / VideoRuns / DesignAssets all nil for
		// Phase 1 — the existing in-memory SessionStores in the
		// skill packages remain the source of truth until Phase 2.
	}
}

// lock is a thin wrapper around sync.RWMutex so the per-entity mem
// stores read naturally (`m.mu.RLock()`). Lifting it to a type alias
// lets us swap for an instrumented mutex later without touching every
// call site.
type lock = sync.RWMutex
