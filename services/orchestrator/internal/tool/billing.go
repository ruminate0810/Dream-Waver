package tool

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/billing"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// WrapWithBilling returns a Tool that:
//
//   1. Debits the configured `price` (micro-units) BEFORE running
//      the inner tool. Returns ErrInsufficient as a ToolResult.Error
//      string so the agent loop sees a graceful failure rather than
//      a panic. Anonymous (no workspace on ctx) skips billing — same
//      posture as the idempotency decorator.
//
//   2. Refunds the debit if the inner tool returns an error OR the
//      result carries an Error field. Refund is best-effort —
//      logged on failure but not propagated.
//
//   3. RecordToolCall always fires (success + failure + skipped),
//      so the audit table has a row per attempt. Fields capture
//      duration, attempt count (currently always 1; X3b-follow-up
//      may add retry tracking), and the fallback_used label when
//      the Resilient decorator wraps this one.
//
// Wiring: agent_runner.go would do something like:
//
//     registry := tool.NewRegistry(
//         tool.WrapWithBilling(t1, billingSvc, billing.PriceOf("generate_image"), "generate_image"),
//         ...
//     )
//
// Routes that call bridges DIRECTLY (routes_design.go, routes_video.go)
// don't use this decorator — they use the chargeOrReject /
// refundOnFailure helpers in the api package instead because the
// "thing being billed" isn't a Tool object.
//
// `service` may be nil — in which case the decorator becomes a pure
// pass-through (useful for tests + dev mode without billing). Same
// posture as nil-store in idempotency.
func WrapWithBilling(inner Tool, service billing.Service, price int64, toolName string) Tool {
	return &billedTool{
		inner:     inner,
		service:   service,
		price:     price,
		toolName:  toolName,
	}
}

type billedTool struct {
	inner    Tool
	service  billing.Service
	price    int64
	toolName string
}

func (t *billedTool) Name() string                { return t.inner.Name() }
func (t *billedTool) Description() string         { return t.inner.Description() }
func (t *billedTool) Parameters() json.RawMessage { return t.inner.Parameters() }

func (t *billedTool) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	wsID, hasWS := workspaceFromCtx(ctx)
	// No service configured OR no workspace on ctx → pass through.
	// The audit row is skipped in the no-workspace case (same as
	// the recordDesignAsset posture in routes_design.go).
	if t.service == nil || !hasWS {
		return t.inner.Execute(ctx, args)
	}
	// Unmetered tool — no debit, no refund, but still write an
	// audit row so we can see what the agent did.
	if t.price <= 0 {
		start := time.Now()
		result, err := t.inner.Execute(ctx, args)
		t.recordAudit(ctx, wsID, args, 0, time.Since(start), err, result)
		return result, err
	}

	// Pre-debit: rejects gracefully before any upstream cost.
	if _, debitErr := t.service.Debit(ctx, wsID, t.price, "tool_call",
		map[string]any{"tool": t.toolName, "args_hash": hexHash(canonicalHashOrNil(args))},
	); debitErr != nil {
		if errors.Is(debitErr, billing.ErrInsufficient) {
			// Surface as ToolResult.Error so the agent loop carries
			// the failure into the next turn's context (and the user
			// sees a sensible "ran out of credit" message). Don't
			// return a Go error — that aborts the loop.
			return schema.ToolResult{
				Error: "insufficient credits — top up the workspace and retry",
			}, nil
		}
		// Other debit errors (DB down) — log and pass through so the
		// inner tool can still run. The audit row will surface the
		// missed debit during reconciliation.
		slog.WarnContext(ctx, "billing.Debit failed — running tool without charge",
			"tool", t.toolName, "err", debitErr)
		start := time.Now()
		result, err := t.inner.Execute(ctx, args)
		t.recordAudit(ctx, wsID, args, 0, time.Since(start), err, result)
		return result, err
	}

	// Charged. Run the tool; refund on failure.
	start := time.Now()
	result, err := t.inner.Execute(ctx, args)
	duration := time.Since(start)

	if err != nil || result.Error != "" {
		// Refund — best-effort. The original debit row stays in
		// the ledger; the refund is a separate positive row that
		// references it via meta.refund_of. Net effect on balance
		// is zero.
		if refundErr := t.service.Credit(ctx, wsID, t.price, "refund",
			map[string]any{
				"tool":      t.toolName,
				"reason":    errOrResultMessage(err, result),
				"refund_of": "tool_call:" + t.toolName,
			},
		); refundErr != nil {
			slog.WarnContext(ctx, "refund failed (balance is now $price low)",
				"tool", t.toolName, "amount_micro", t.price, "err", refundErr)
		}
		t.recordAudit(ctx, wsID, args, 0, duration, err, result)
		return result, err
	}

	// Success — debit stands, audit row carries the charged amount.
	t.recordAudit(ctx, wsID, args, t.price, duration, nil, result)
	return result, nil
}

// ─── Helpers ───────────────────────────────────────────────────────

func (t *billedTool) recordAudit(ctx context.Context, wsID uuid.UUID, args json.RawMessage, debited int64, dur time.Duration, runErr error, result schema.ToolResult) {
	hash := canonicalHashOrNil(args)
	call := billing.ToolCall{
		WorkspaceID:      wsID,
		ToolName:         t.toolName,
		ArgsHash:         hash,
		ResultSummary:    truncate(result.Output, 400),
		DebitAmountMicro: debited,
		DurationMS:       int(dur.Milliseconds()),
		Attempt:          1,
	}
	if runErr != nil {
		call.Error = runErr.Error()
	} else if result.Error != "" {
		call.Error = result.Error
	}
	if err := t.service.RecordToolCall(ctx, call); err != nil {
		slog.WarnContext(ctx, "RecordToolCall failed (audit only)",
			"tool", t.toolName, "err", err)
	}
}

// canonicalHashOrNil reuses idempotency.go's canonicalHash for the
// audit row's args_hash. Skips empty / unhashable args gracefully.
func canonicalHashOrNil(args json.RawMessage) []byte {
	if len(args) == 0 {
		return nil
	}
	h, err := canonicalHash(args)
	if err != nil {
		return nil
	}
	return h
}

func errOrResultMessage(err error, r schema.ToolResult) string {
	if err != nil {
		return err.Error()
	}
	return r.Error
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// hexHash is a tiny hex encoder so the meta jsonb stays readable when
// inspecting the ledger by hand. encoding/hex would do the same but
// dragging in another import for a 4-line helper isn't worth it.
func hexHash(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = digits[c>>4]
		out[i*2+1] = digits[c&0x0F]
	}
	return string(out)
}
