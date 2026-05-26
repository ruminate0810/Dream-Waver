package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMemCreditLedger_SumBalance_Zero(t *testing.T) {
	t.Parallel()
	ledger := newMemCreditLedger()
	bal, err := ledger.SumBalance(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("SumBalance: %v", err)
	}
	if bal != 0 {
		t.Fatalf("empty workspace balance = %d, want 0", bal)
	}
}

func TestMemCreditLedger_Credit_Then_Debit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ledger := newMemCreditLedger()
	ws := uuid.New()

	if err := ledger.InsertCredit(ctx, ws, 1_000_000, "trial_grant", nil); err != nil {
		t.Fatalf("InsertCredit: %v", err)
	}
	entry, err := ledger.InsertDebit(ctx, ws, 100_000, "tool_call", map[string]any{"tool": "video_clip"})
	if err != nil {
		t.Fatalf("InsertDebit: %v", err)
	}
	if entry.AmountMicro != -100_000 {
		t.Fatalf("debit row amount = %d, want -100000 (negative for debit)", entry.AmountMicro)
	}
	if entry.BalanceAfter != 900_000 {
		t.Fatalf("BalanceAfter = %d, want 900000", entry.BalanceAfter)
	}

	bal, _ := ledger.SumBalance(ctx, ws)
	if bal != 900_000 {
		t.Fatalf("SumBalance after debit = %d, want 900000", bal)
	}
}

func TestMemCreditLedger_Debit_InsufficientLeavesBalanceUntouched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ledger := newMemCreditLedger()
	ws := uuid.New()
	_ = ledger.InsertCredit(ctx, ws, 5_000, "trial_grant", nil)

	if _, err := ledger.InsertDebit(ctx, ws, 6_000, "tool_call", nil); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected ErrInsufficient, got %v", err)
	}

	// CRITICAL: balance unchanged + no debit row written.
	bal, _ := ledger.SumBalance(ctx, ws)
	if bal != 5_000 {
		t.Fatalf("balance after failed debit = %d, want 5000 (debit must be transactional)", bal)
	}
	rows, _ := ledger.List(ctx, ws, 10)
	if len(rows) != 1 || rows[0].Reason != "trial_grant" {
		t.Fatalf("failed debit left a phantom row: %+v", rows)
	}
}

func TestMemCreditLedger_Debit_ExactBalancePasses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ledger := newMemCreditLedger()
	ws := uuid.New()
	_ = ledger.InsertCredit(ctx, ws, 1_000, "trial_grant", nil)

	// Exact-balance debit must succeed and leave 0.
	entry, err := ledger.InsertDebit(ctx, ws, 1_000, "tool_call", nil)
	if err != nil {
		t.Fatalf("exact-balance debit should pass, got %v", err)
	}
	if entry.BalanceAfter != 0 {
		t.Fatalf("BalanceAfter = %d, want 0", entry.BalanceAfter)
	}
	// Subsequent debit of even 1 micro must fail.
	if _, err := ledger.InsertDebit(ctx, ws, 1, "tool_call", nil); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("post-exhaustion debit should ErrInsufficient, got %v", err)
	}
}

func TestMemCreditLedger_WorkspaceIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ledger := newMemCreditLedger()
	wsA := uuid.New()
	wsB := uuid.New()
	_ = ledger.InsertCredit(ctx, wsA, 1_000, "trial_grant", nil)

	balB, _ := ledger.SumBalance(ctx, wsB)
	if balB != 0 {
		t.Fatalf("wsB sees wsA's balance: %d", balB)
	}

	// Debiting wsB with wsA's funds must fail.
	if _, err := ledger.InsertDebit(ctx, wsB, 1, "tool_call", nil); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("wsB should be insufficient (its own balance is 0), got %v", err)
	}
}

func TestMemCreditLedger_List_NewestFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ledger := newMemCreditLedger()
	ws := uuid.New()
	_ = ledger.InsertCredit(ctx, ws, 1_000_000, "trial_grant", nil)
	_, _ = ledger.InsertDebit(ctx, ws, 5_000, "tool_call", nil)
	_, _ = ledger.InsertDebit(ctx, ws, 8_000, "tool_call", nil)

	rows, _ := ledger.List(ctx, ws, 10)
	if len(rows) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(rows))
	}
	// Most recent first.
	if rows[0].Reason != "tool_call" || rows[0].AmountMicro != -8_000 {
		t.Fatalf("first row should be the latest debit, got %+v", rows[0])
	}
	if rows[2].Reason != "trial_grant" {
		t.Fatalf("last row should be the grant, got %+v", rows[2])
	}
}

func TestMemCreditLedger_Reject_NonPositive(t *testing.T) {
	t.Parallel()
	ledger := newMemCreditLedger()
	ws := uuid.New()
	if _, err := ledger.InsertDebit(context.Background(), ws, 0, "tool_call", nil); err == nil {
		t.Fatalf("InsertDebit(0) should fail")
	}
	if err := ledger.InsertCredit(context.Background(), ws, -100, "trial_grant", nil); err == nil {
		t.Fatalf("InsertCredit(<0) should fail")
	}
}
