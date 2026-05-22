// Package tool defines the Tool interface and a thread-safe Registry that
// the agent loop uses to dispatch LLM-requested tool calls.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// Tool is one capability exposed to the agent. Each implementation should:
//   - have a stable Name (LLM addresses it by name)
//   - publish a JSON-Schema for its arguments
//   - return a structured ToolResult (Output is the textual observation)
//
// Tools are expected to be safe to call concurrently from multiple agents.
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage // JSON Schema for args
	Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error)
}

// Registry is a name→Tool map with concurrency-safe access.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.Register(t)
	}
	return r
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Schemas returns the JSON-Schema descriptors for every registered tool,
// in deterministic name order. Used by the LLM client to declare available
// tools.
func (r *Registry) Schemas() []schema.ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]schema.ToolSchema, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, schema.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return out
}

// Execute looks up `name` and runs it. The agent loop is expected to wrap
// the result into a tool message and append it to memory.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (schema.ToolResult, error) {
	t, ok := r.Get(name)
	if !ok {
		return schema.ToolResult{Error: fmt.Sprintf("unknown tool %q", name)}, nil
	}
	return t.Execute(ctx, args)
}

// Terminate is a special sentinel tool. When invoked, the agent loop sets
// state to FINISHED and exits. The agent stack ships with this registered.
type Terminate struct{}

func (Terminate) Name() string        { return "terminate" }
func (Terminate) Description() string { return "Signal that the task is complete and no further steps are needed." }
func (Terminate) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string"}}}`)
}
func (Terminate) Execute(_ context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct{ Reason string }
	_ = json.Unmarshal(args, &p)
	if p.Reason == "" {
		p.Reason = "task complete"
	}
	return schema.ToolResult{Output: "Terminated: " + p.Reason}, nil
}

// IsSpecial reports whether a tool name should end the agent loop when called.
// Keep this in sync with whatever sentinels the agent treats specially.
func IsSpecial(name string) bool {
	return name == (Terminate{}).Name()
}
