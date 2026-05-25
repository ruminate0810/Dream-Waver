package llm

import (
	"context"
	"fmt"
	"sync"
)

// MultiRouter picks an underlying Client based on a task tag. It allows
// cost-conscious routing: cheap models for high-volume work, expensive ones
// for plan/critic steps. Mirrors the Mixture-of-Agents idea from Genspark
// without the answer-merging step (that comes later).
type MultiRouter struct {
	mu        sync.RWMutex
	clients   map[string]Client // task → client
	fallback  Client
	modelMap  map[string]string // task → model
}

func NewMultiRouter(fallback Client) *MultiRouter {
	return &MultiRouter{
		clients:  make(map[string]Client),
		modelMap: make(map[string]string),
		fallback: fallback,
	}
}

// Bind associates a task tag with a client and a default model.
func (r *MultiRouter) Bind(task string, client Client, model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[task] = client
	r.modelMap[task] = model
}

func (r *MultiRouter) For(task string) Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if c, ok := r.clients[task]; ok {
		return c
	}
	return r.fallback
}

func (r *MultiRouter) Name() string { return "multi-router" }

// AskTool with an empty Model and no task hint just delegates to fallback.
// Most call sites should use For(task).AskTool(...) instead.
func (r *MultiRouter) AskTool(ctx context.Context, req AskToolRequest) (*AskToolResponse, error) {
	if r.fallback == nil {
		return nil, fmt.Errorf("router: no fallback client configured")
	}
	return r.fallback.AskTool(ctx, req)
}

// AskToolStream delegates to the fallback client's streaming method.
// Like AskTool, the typical call site is For(task).AskToolStream(...).
func (r *MultiRouter) AskToolStream(ctx context.Context, req AskToolRequest, onChunk func(string)) (*AskToolResponse, error) {
	if r.fallback == nil {
		return nil, fmt.Errorf("router: no fallback client configured")
	}
	return r.fallback.AskToolStream(ctx, req, onChunk)
}

// ModelFor returns the configured default model for a task, or empty string.
func (r *MultiRouter) ModelFor(task string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modelMap[task]
}
