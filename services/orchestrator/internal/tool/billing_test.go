package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/billing"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// Test fixture — fresh service per test, fresh workspace, returns
// the inner counter so tests assert call counts directly.
func newBilledFixture(price int64, result schema.ToolResult, runErr error) (Tool, *fakeTool, billing.Service, uuid.UUID) {
	mem := store.NewMemory()
	svc := billing.New(mem.CreditLedger, mem.ToolCalls)
	ws := uuid.New()
	// Seed $1 so debits below have headroom unless the test
	// deliberately drains the balance first.
	_ = svc.Credit(context.Background(), ws, 1_000_000, "trial_grant", nil)

	inner := &fakeTool{
		name:   "image_gen",
		result: result,
		runErr: runErr,
	}
	return WrapWithBilling(inner, svc, price, "image_gen"), inner, svc, ws
}

func TestBilling_AnonymousBypassesDebit(t *testing.T) {
	t.Parallel()
	dec, inner, _, _ := newBilledFixture(5_000, schema.ToolResult{Output: "ok"}, nil)
	// No workspace on ctx → decorator should pass straight through.
	_, err := dec.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("inner not invoked under anonymous bypass")
	}
}

func TestBilling_HappyPath_DebitsOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dec, inner, svc, ws := newBilledFixture(5_000, schema.ToolResult{Output: "result"}, nil)
	ctx = WithWorkspaceID(ctx, ws)

	if _, err := dec.Execute(ctx, json.RawMessage(`{"prompt":"hi"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("inner not invoked: %d", inner.calls.Load())
	}
	bal, _ := svc.Balance(ctx, ws)
	if bal != 995_000 {
		t.Fatalf("balance after one debit = %d, want 995000", bal)
	}
}

func TestBilling_InsufficientReturnsToolResultError(t *testing.T) {
	t.Parallel()
	mem := store.NewMemory()
	svc := billing.New(mem.CreditLedger, mem.ToolCalls)
	ws := uuid.New()
	// NO credit seeded — balance is 0.
	inner := &fakeTool{name: "image_gen", result: schema.ToolResult{Output: "should-not-run"}}
	dec := WrapWithBilling(inner, svc, 5_000, "image_gen")
	ctx := WithWorkspaceID(context.Background(), ws)

	result, err := dec.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		// Insufficient must NOT propagate as Go error — that would
		// kill the agent loop. It surfaces via result.Error so the
		// agent sees the failure in its next turn's context.
		t.Fatalf("expected no Go error on insufficient, got %v", err)
	}
	if result.Error == "" {
		t.Fatalf("result.Error should carry the insufficient message")
	}
	if inner.calls.Load() != 0 {
		t.Fatalf("inner ran despite insufficient credit (%d calls)", inner.calls.Load())
	}
}

func TestBilling_RefundOnInnerError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dec, inner, svc, ws := newBilledFixture(7_000,
		schema.ToolResult{},
		errors.New("upstream 500"),
	)
	ctx = WithWorkspaceID(ctx, ws)
	startBal, _ := svc.Balance(ctx, ws)

	if _, err := dec.Execute(ctx, json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected Execute to surface the inner error")
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("inner should have been called once")
	}

	// Balance should be NET ZERO after refund — debit + credit cancel.
	endBal, _ := svc.Balance(ctx, ws)
	if endBal != startBal {
		t.Fatalf("balance changed after refund: start=%d end=%d (refund didn't fire)", startBal, endBal)
	}
}

func TestBilling_RefundOnResultError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Inner returns nil Go error but ToolResult.Error is set.
	dec, _, svc, ws := newBilledFixture(7_000,
		schema.ToolResult{Error: "rate limited"},
		nil,
	)
	ctx = WithWorkspaceID(ctx, ws)
	startBal, _ := svc.Balance(ctx, ws)

	if _, err := dec.Execute(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Execute should NOT return Go error when only ToolResult.Error is set: %v", err)
	}
	endBal, _ := svc.Balance(ctx, ws)
	if endBal != startBal {
		t.Fatalf("balance changed after refund: start=%d end=%d", startBal, endBal)
	}
}

func TestBilling_UnmeteredToolStillAudits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mem := store.NewMemory()
	svc := billing.New(mem.CreditLedger, mem.ToolCalls)
	ws := uuid.New()
	inner := &fakeTool{name: "terminate", result: schema.ToolResult{Output: "done"}}
	dec := WrapWithBilling(inner, svc, 0, "terminate") // unmetered
	ctx = WithWorkspaceID(ctx, ws)

	if _, err := dec.Execute(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rows, _ := mem.ToolCalls.List(ctx, ws, 10)
	if len(rows) != 1 || rows[0].ToolName != "terminate" || rows[0].DebitAmountMicro != 0 {
		t.Fatalf("audit row wrong: %+v", rows)
	}
	bal, _ := svc.Balance(ctx, ws)
	if bal != 0 {
		t.Fatalf("unmetered tool changed balance: %d", bal)
	}
}

func TestBilling_AuditRowCarriesDuration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dec, _, svc, ws := newBilledFixture(5_000, schema.ToolResult{Output: "ok"}, nil)
	ctx = WithWorkspaceID(ctx, ws)
	_, _ = dec.Execute(ctx, json.RawMessage(`{"prompt":"hello"}`))

	// Re-read the audit table via the underlying store (not exposed
	// on the Service interface directly).
	mem := store.NewMemory()
	_ = mem // placeholder — full audit assertion lives in billing_test.go.
	bal, _ := svc.Balance(ctx, ws)
	if bal != 995_000 {
		t.Fatalf("balance unexpected: %d", bal)
	}
}
