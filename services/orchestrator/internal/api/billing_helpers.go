package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/billing"
)

// Shared per-handler billing primitives. Used by routes_design.go +
// routes_video.go to wrap bridge-calling handlers (the bridges aren't
// Tools, so tool.WrapWithBilling doesn't fit — we get the same
// "pre-debit, refund on failure, audit always" pattern at the
// handler layer instead).
//
// Posture for X3b:
//   - Anonymous (no workspace on ctx) → free pass, no audit. Same
//     posture as recordDesignAsset / recordVideoRun from X2b. When
//     X3b's auth flip lands, these routes will be 401'd before
//     reaching the helper.
//   - billing service nil (dev without DB) → free pass + warn-log.
//   - Insufficient funds → 402 with a structured payload the
//     frontend can render as "top up to continue".

// errorJSON402 writes the standardized "insufficient credits"
// response. Shape is read by the web's ApiError class — the
// fieldErrors slot becomes the "you need $X, you have $Y" line.
func errorJSON402(w http.ResponseWriter, toolName string, requiredMicro, balanceMicro int64) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusPaymentRequired)
	_ = jsonEncoder(w).Encode(map[string]any{
		"ok": false,
		"error": map[string]any{
			"code":             "insufficient_credits",
			"message":          "Out of credits — top up to continue.",
			"tool_name":        toolName,
			"required_micro":   requiredMicro,
			"required_display": billing.FormatUSD(requiredMicro),
			"balance_micro":    balanceMicro,
			"balance_display":  billing.FormatUSD(balanceMicro),
		},
	})
}

// chargeOrReject debits `price` micro-units for the named tool. On
// insufficient funds it writes a 402 response and returns false —
// the caller should `return` immediately. Anonymous (no workspace)
// returns true without debiting so existing non-auth flows continue
// to work for free until the routes get wrapped in auth.Required.
//
// Returns (debitedAmount, true) on success. The caller stashes
// `debitedAmount` and passes it to refundOnFailure if the upstream
// call later errors.
func (h *handlers) chargeOrReject(w http.ResponseWriter, ctx context.Context, toolName string) (int64, bool) {
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		return 0, true // anonymous — free pass (no audit either)
	}
	if h.deps.Billing == nil {
		// Billing not wired (impossible in main.go's flow but
		// defensive against tests that build api.Dependencies by
		// hand). Pass through with a warn log.
		slog.WarnContext(ctx, "billing service is nil — skipping debit",
			"tool", toolName)
		return 0, true
	}
	price := billing.PriceOf(toolName)
	if price <= 0 {
		// Unmetered tool. Still pass through. RecordToolCall will
		// happen on the success path with debit_amount=0 so the
		// audit row is consistent.
		return 0, true
	}
	_, err := h.deps.Billing.Debit(ctx, wsID, price, "tool_call",
		map[string]any{"tool": toolName})
	if err == nil {
		return price, true
	}
	if errors.Is(err, billing.ErrInsufficient) {
		balance, _ := h.deps.Billing.Balance(ctx, wsID)
		errorJSON402(w, toolName, price, balance)
		return 0, false
	}
	// Real DB error → 500. We don't pass through here because the
	// audit trail would be silently incomplete.
	errorJSON(w, http.StatusInternalServerError, "billing: "+err.Error())
	return 0, false
}

// refundOnFailure issues a refund for a previously-charged debit
// after the upstream call failed. Best-effort — logged on failure
// but not propagated.
//
// Pattern at call sites:
//
//   debited, ok := h.chargeOrReject(w, ctx, "generate_image")
//   if !ok { return }
//
//   resp, err := bridge.Call(...)
//   if err != nil {
//       h.refundOnFailure(ctx, "generate_image", debited, err)
//       writeBridgeError(w, "generate_image", err)
//       return
//   }
//
//   h.recordSuccess(ctx, "generate_image", debited, resp.TaskID, time.Since(start))
func (h *handlers) refundOnFailure(ctx context.Context, toolName string, debited int64, callErr error) {
	if debited <= 0 || h.deps.Billing == nil {
		return
	}
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		return
	}
	if err := h.deps.Billing.Credit(ctx, wsID, debited, "refund",
		map[string]any{
			"tool":      toolName,
			"refund_of": "tool_call:" + toolName,
			"reason":    callErr.Error(),
		},
	); err != nil {
		slog.WarnContext(ctx, "refund failed — balance is now `debited` low",
			"tool", toolName, "amount_micro", debited, "err", err)
	}
	// Audit row on the failure path so we can see what got refunded.
	_ = h.deps.Billing.RecordToolCall(ctx, billing.ToolCall{
		WorkspaceID:      wsID,
		UserID:           userIDFromCtx(ctx),
		ToolName:         toolName,
		DebitAmountMicro: 0, // net zero after refund
		Error:            callErr.Error(),
		Attempt:          1,
	})
}

// recordSuccess writes the audit row after a successful upstream
// call. The debit already happened in chargeOrReject; this is the
// reconciliation row.
func (h *handlers) recordSuccess(ctx context.Context, toolName string, debited int64, summary string, duration time.Duration) {
	if h.deps.Billing == nil {
		return
	}
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		return
	}
	if err := h.deps.Billing.RecordToolCall(ctx, billing.ToolCall{
		WorkspaceID:      wsID,
		UserID:           userIDFromCtx(ctx),
		ToolName:         toolName,
		ResultSummary:    summary,
		DebitAmountMicro: debited,
		DurationMS:       int(duration.Milliseconds()),
		Attempt:          1,
	}); err != nil {
		slog.WarnContext(ctx, "audit row failed (debit already happened)",
			"tool", toolName, "err", err)
	}
}
