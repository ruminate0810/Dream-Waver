package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// panickyTool deliberately nil-derefs from inside Execute so we can verify
// Registry.Execute's recover catches it without taking the test process
// down (which is what would happen in production without J-1).
type panickyTool struct{}

func (panickyTool) Name() string              { return "boom" }
func (panickyTool) Description() string       { return "deliberately panics — only used in tests" }
func (panickyTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (panickyTool) Execute(_ context.Context, _ json.RawMessage) (schema.ToolResult, error) {
	var p *struct{ X int }
	_ = p.X // nil-deref
	return schema.ToolResult{}, nil
}

func TestRegistry_Execute_RecoversFromPanic(t *testing.T) {
	r := NewRegistry()
	r.Register(panickyTool{})

	// Without J-1's recover this would crash the test binary.
	res, err := r.Execute(context.Background(), "boom", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected nil error (panic should be converted to ToolResult.Error), got %v", err)
	}
	if !strings.Contains(res.Error, "panicked") {
		t.Errorf("expected ToolResult.Error to mention panic, got %q", res.Error)
	}
	if !strings.Contains(res.Error, `"boom"`) {
		t.Errorf("expected ToolResult.Error to name the tool, got %q", res.Error)
	}
}

func TestRegistry_Execute_UnknownTool(t *testing.T) {
	r := NewRegistry()
	res, err := r.Execute(context.Background(), "no-such-tool", json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("expected nil error for unknown tool, got %v", err)
	}
	if !strings.Contains(res.Error, `unknown tool "no-such-tool"`) {
		t.Errorf("expected ToolResult.Error to say unknown tool, got %q", res.Error)
	}
}
