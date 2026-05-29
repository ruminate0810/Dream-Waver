package store

import "sync"

// NewMemory builds an in-memory Store for tests and for local dev when
// DATABASE_URL is empty. Phase 2a fills in all job/asset entities;
// the in-memory backing keeps local dev working without docker.
// Phase 3 (X3a) adds CreditLedger + ToolCalls.
//
// Memory store is NOT cross-process — each orchestrator instance has
// its own. Fine for solo dev / unit tests; pgx is the real story.
func NewMemory() *Store {
	return &Store{
		Users:           newMemUsers(),
		Workspaces:      newMemWorkspaces(),
		SlideJobs:       newMemSlideJobs(),
		GameJobs:        newMemGameJobs(),
		VideoRuns:       newMemVideoRuns(),
		DesignAssets:    newMemDesignAssets(),
		IdempotencyKeys: newMemIdempotencyKeys(),
		CreditLedger:    newMemCreditLedger(),
		ToolCalls:       newMemToolCalls(),
		UserTemplates:   newMemUserTemplates(),
		ChatEvents:      newMemChatEvents(), // AA.1
	}
}

// lock is a thin wrapper around sync.RWMutex so the per-entity mem
// stores read naturally (`m.mu.RLock()`). Lifting it to a type alias
// lets us swap for an instrumented mutex later without touching every
// call site.
type lock = sync.RWMutex
