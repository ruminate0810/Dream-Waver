// Package billing is the per-workspace credit pool + tool-call audit
// trail. Every paid tool invocation goes through here so the bill the
// user sees on month-end is reconstructible from a single ledger.
//
// Design choices:
//
//   1. **Micro-units everywhere.** 1 USD = 1_000_000 micro. Eliminates
//      float arithmetic in pricing tables (no "we charged $0.0049999")
//      and gives plenty of headroom in int64.
//
//   2. **Charge AFTER success.** The decorator (tool.WrapWithBilling,
//      Sprint X3b) runs the inner tool first, then debits if the
//      result is healthy. Failure-after-debit refunds are a
//      classic race condition source; we sidestep them entirely.
//
//   3. **Append-only ledger.** Balance is SUM(amount_micro). Never
//      stored as a denormalised column. Refunds are positive entries
//      that reference the original debit's id via meta.refund_of.
//
//   4. **Per-tool static prices.** Pricing changes are deploys, not
//      runtime config — keeps the invariant "everyone who called X
//      this hour paid the same" trivially true.
//
//   5. **Anonymous bypass.** Calls with no workspace ctx skip
//      billing entirely. Existing anonymous flows (slides /games
//      / video / design before login) continue to work for free
//      until the routes get wrapped in auth.Required (X3b
//      follow-up).
package billing

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// ─── Errors ─────────────────────────────────────────────────────────

var (
	// ErrInsufficient is returned by Debit when the workspace's
	// balance can't cover the request. Callers convert to HTTP 402
	// (Payment Required) at the API boundary.
	ErrInsufficient = errors.New("billing: insufficient credits")
)

// ─── Service ────────────────────────────────────────────────────────

// Service is the credit-pool facade. Implementations wrap the
// store.CreditLedger + store.ToolCalls interfaces; the split between
// "billing service" and "store" is so we can layer policy (refund
// after retry, partial-result discounts, etc.) without leaking it
// into the SQL layer.
type Service interface {
	// Balance returns the current credit pool in micro-units. Zero
	// for workspaces with no ledger entries.
	Balance(ctx context.Context, workspaceID uuid.UUID) (int64, error)

	// Debit atomically deducts `amount` micro-units. Returns
	// ErrInsufficient when the balance would go negative — no row
	// is written in that case. `reason` should be "tool_call" for
	// the common case; widening the closed set in the SQL CHECK
	// requires a migration.
	Debit(ctx context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) (newBalance int64, err error)

	// Credit adds `amount` micro-units. Used for trial grants (on
	// first login), manual top-ups, and refunds.
	Credit(ctx context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) error

	// RecordToolCall writes one audit row. Decoupled from Debit so
	// the caller can record:
	//   - successful debits (debit_amount > 0)
	//   - unmetered runs (debit_amount = 0, e.g. terminate, plan_outline)
	//   - fallback paths (fallback_used set)
	//   - errors (error set, debit_amount = 0 because we didn't charge)
	RecordToolCall(ctx context.Context, call ToolCall) error
}

// ToolCall is the audit row shape. Mirrors store.ToolCall but lives
// here so callers don't reach into the store layer directly.
type ToolCall struct {
	WorkspaceID      uuid.UUID
	UserID           uuid.UUID // uuid.Nil for system / background flows
	ToolName         string
	ArgsHash         []byte
	ResultSummary    string
	DebitAmountMicro int64
	DurationMS       int
	Attempt          int
	FallbackUsed     string
	Error            string
}

// ─── Constructor ────────────────────────────────────────────────────

// New builds the default Service backed by the supplied store
// interfaces. Both must be non-nil — pass store.NewMemory()'s
// CreditLedger / ToolCalls handles when you need an in-process
// implementation (tests, local dev).
func New(ledger store.CreditLedger, audit store.ToolCalls) Service {
	return &serviceImpl{ledger: ledger, audit: audit}
}

type serviceImpl struct {
	ledger store.CreditLedger
	audit  store.ToolCalls
}

func (s *serviceImpl) Balance(ctx context.Context, workspaceID uuid.UUID) (int64, error) {
	if workspaceID == uuid.Nil {
		// Anonymous → no ledger. Return zero so the caller's
		// "balance >= price" check goes through the ErrInsufficient
		// path naturally.
		return 0, nil
	}
	return s.ledger.SumBalance(ctx, workspaceID)
}

func (s *serviceImpl) Debit(ctx context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) (int64, error) {
	if amount <= 0 {
		// Defensive: Debit with non-positive amount is a caller bug;
		// surface loudly instead of silently writing a positive row.
		return 0, errors.New("billing.Debit: amount must be > 0")
	}
	if workspaceID == uuid.Nil {
		// Anonymous request — no debit, no ledger row. Existing
		// flows that haven't been auth-gated yet (slides for
		// non-logged-in users) continue to work for free. When
		// X3b's web flip happens, anonymous requests will 401
		// before reaching the decorator.
		return 0, nil
	}
	// store.CreditLedger.InsertDebit handles the atomic
	// balance-check-and-insert (single SQL statement). On
	// insufficient funds it returns store.ErrInsufficient which we
	// pass through as our typed error.
	row, err := s.ledger.InsertDebit(ctx, workspaceID, amount, reason, meta)
	if err != nil {
		if errors.Is(err, store.ErrInsufficient) {
			return 0, ErrInsufficient
		}
		return 0, err
	}
	return row.BalanceAfter, nil
}

func (s *serviceImpl) Credit(ctx context.Context, workspaceID uuid.UUID, amount int64, reason string, meta map[string]any) error {
	if amount <= 0 {
		return errors.New("billing.Credit: amount must be > 0")
	}
	if workspaceID == uuid.Nil {
		// No anonymous grants — there's no workspace to grant to.
		return nil
	}
	return s.ledger.InsertCredit(ctx, workspaceID, amount, reason, meta)
}

func (s *serviceImpl) RecordToolCall(ctx context.Context, call ToolCall) error {
	if call.WorkspaceID == uuid.Nil {
		// Anonymous → skip audit. The handler's existing logs are
		// the only trace for anonymous traffic; that's fine
		// because anonymous won't outlast the auth-gate flip.
		return nil
	}
	return s.audit.Insert(ctx, store.ToolCall{
		WorkspaceID:      call.WorkspaceID,
		UserID:           call.UserID,
		ToolName:         call.ToolName,
		ArgsHash:         call.ArgsHash,
		ResultSummary:    call.ResultSummary,
		DebitAmountMicro: call.DebitAmountMicro,
		DurationMS:       call.DurationMS,
		Attempt:          call.Attempt,
		FallbackUsed:     call.FallbackUsed,
		Error:            call.Error,
	})
}
