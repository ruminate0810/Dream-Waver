package billing

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// Helper — fresh in-process service for each test.
func newTestService() (Service, uuid.UUID) {
	mem := store.NewMemory()
	return New(mem.CreditLedger, mem.ToolCalls), uuid.New()
}

func TestService_Balance_ZeroForNewWorkspace(t *testing.T) {
	t.Parallel()
	svc, ws := newTestService()
	bal, err := svc.Balance(context.Background(), ws)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if bal != 0 {
		t.Fatalf("Balance = %d, want 0", bal)
	}
}

func TestService_Credit_Debit_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ws := newTestService()

	if err := svc.Credit(ctx, ws, 10_000, "trial_grant", nil); err != nil {
		t.Fatalf("Credit: %v", err)
	}
	bal, _ := svc.Balance(ctx, ws)
	if bal != 10_000 {
		t.Fatalf("Balance after credit = %d, want 10000", bal)
	}

	newBal, err := svc.Debit(ctx, ws, 3_000, "tool_call", nil)
	if err != nil {
		t.Fatalf("Debit: %v", err)
	}
	if newBal != 7_000 {
		t.Fatalf("Debit returned new balance %d, want 7000", newBal)
	}

	bal, _ = svc.Balance(ctx, ws)
	if bal != 7_000 {
		t.Fatalf("Balance after debit = %d, want 7000", bal)
	}
}

func TestService_Debit_InsufficientWhenEmpty(t *testing.T) {
	t.Parallel()
	svc, ws := newTestService()
	_, err := svc.Debit(context.Background(), ws, 1_000, "tool_call", nil)
	if !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected ErrInsufficient, got %v", err)
	}
}

func TestService_Debit_PartialBalance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ws := newTestService()
	_ = svc.Credit(ctx, ws, 5_000, "trial_grant", nil)

	if _, err := svc.Debit(ctx, ws, 6_000, "tool_call", nil); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("debit > balance should fail with ErrInsufficient, got %v", err)
	}

	// Verify NO debit row was written — the balance is unchanged.
	bal, _ := svc.Balance(ctx, ws)
	if bal != 5_000 {
		t.Fatalf("failed debit changed balance: %d", bal)
	}
}

// TestService_Debit_ConcurrentRace is the regression gate for the
// "two concurrent debits each see enough balance" race. With the
// in-memory store's mutex (and the pgx CTE-with-WHERE-balance >=
// amount on the production path), exactly N×amount should succeed
// when N×amount fits in the balance, and surplus debits get
// ErrInsufficient. The race window we'd be vulnerable to without
// proper atomicity is "all N succeed when balance only covers N-1".
func TestService_Debit_ConcurrentRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ws := newTestService()
	_ = svc.Credit(ctx, ws, 10_000, "trial_grant", nil) // covers 10 debits of 1000

	const workers = 50
	const perDebit = 1_000

	var wg sync.WaitGroup
	wg.Add(workers)
	successes := make(chan struct{}, workers)
	insufficients := make(chan struct{}, workers)
	other := make(chan error, workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Debit(ctx, ws, perDebit, "tool_call", nil)
			switch {
			case err == nil:
				successes <- struct{}{}
			case errors.Is(err, ErrInsufficient):
				insufficients <- struct{}{}
			default:
				other <- err
			}
		}()
	}
	wg.Wait()
	close(successes)
	close(insufficients)
	close(other)

	for err := range other {
		t.Fatalf("unexpected debit error: %v", err)
	}

	successCount := 0
	for range successes {
		successCount++
	}
	if successCount != 10 {
		t.Fatalf("concurrent debits race: %d successes, want exactly 10", successCount)
	}

	bal, _ := svc.Balance(ctx, ws)
	if bal != 0 {
		t.Fatalf("balance after race = %d, want 0", bal)
	}
}

func TestService_Debit_AnonymousNoOp(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService()
	// uuid.Nil workspace = anonymous → debit returns 0, nil (free pass).
	// Existing anonymous slides/games/video/design flows continue to
	// work without auth until X3b flips them to required.
	bal, err := svc.Debit(context.Background(), uuid.Nil, 1_000, "tool_call", nil)
	if err != nil {
		t.Fatalf("anonymous debit returned error: %v", err)
	}
	if bal != 0 {
		t.Fatalf("anonymous debit returned non-zero balance: %d", bal)
	}
}

func TestService_Debit_RejectsNonPositiveAmount(t *testing.T) {
	t.Parallel()
	svc, ws := newTestService()
	if _, err := svc.Debit(context.Background(), ws, 0, "tool_call", nil); err == nil {
		t.Fatalf("Debit(0) should fail — caller bug surface")
	}
	if _, err := svc.Debit(context.Background(), ws, -100, "tool_call", nil); err == nil {
		t.Fatalf("Debit(<0) should fail")
	}
}

func TestService_RecordToolCall_PersistsAuditRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mem := store.NewMemory()
	svc := New(mem.CreditLedger, mem.ToolCalls)
	ws := uuid.New()

	call := ToolCall{
		WorkspaceID:      ws,
		UserID:           uuid.New(),
		ToolName:         "image_gen",
		DebitAmountMicro: 5_000,
		DurationMS:       42_000,
		Attempt:          1,
	}
	if err := svc.RecordToolCall(ctx, call); err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}

	rows, err := mem.ToolCalls.List(ctx, ws, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].ToolName != "image_gen" || rows[0].DebitAmountMicro != 5_000 {
		t.Fatalf("audit row: %+v", rows)
	}
}

// ─── Prices ──────────────────────────────────────────────────────────

func TestPriceOf_KnownTool(t *testing.T) {
	t.Parallel()
	if PriceOf("generate_image") != 5_000 {
		t.Fatalf("generate_image price drift — update Prices map and PRICING.md together")
	}
	if PriceOf("video_clip") != 75_000 {
		t.Fatalf("video_clip price drift")
	}
}

func TestPriceOf_UnknownIsZero(t *testing.T) {
	t.Parallel()
	if PriceOf("not_a_real_tool") != 0 {
		t.Fatalf("unknown tools must return 0 so Debit() can be skipped")
	}
}

func TestFormatUSD(t *testing.T) {
	t.Parallel()
	cases := []struct {
		micro int64
		want  string
	}{
		{0, "$0.0000"},
		{500, "$0.0005"},
		{5_000, "$0.0050"},
		{1_000_000, "$1.00"},
		{2_500_000, "$2.50"},
	}
	for _, tc := range cases {
		if got := FormatUSD(tc.micro); got != tc.want {
			t.Errorf("FormatUSD(%d) = %q, want %q", tc.micro, got, tc.want)
		}
	}
}
