"use client";

import { ArrowUpRight, Loader2 } from "lucide-react";

// Sprint AF.3 — single source of truth for error / warning callouts
// across the app. Replaces 5 different in-place implementations that
// had subtly different paddings, kicker labels, button styles:
//   - ChatThread.ErrorRow         (plain red text, missable)
//   - EditTurnTrace.ErrorBanner    (vermillion strip + retry button)
//   - SlidesOpening.PhaseBody     (vermillion strip + 新建相似 link)
//   - SlideFrame loadError card   (dashed border + retry button)
//   - /slides/new alert           (vermillion strip + retry button)
//
// All five now render via <ErrorCallout> with severity + optional
// action. Layout: hard-bordered pixel callout — 2px ink border +
// offset pixel shadow over a red-family tint, mono kicker + body,
// optional accent press-button CTA on the right edge. Width adapts
// to container.

export type ErrorAction = {
  label: string; // "重试" / "新建相似 deck"
  /** When set, click runs this handler. */
  onClick?: () => void;
  /** When set, renders an <a> anchor instead of <button>. */
  href?: string;
  /** True while the action is in-flight (button disabled + spinner). */
  busy?: boolean;
  /** Button label override while busy (e.g. "重试中"). Defaults to label. */
  busyLabel?: string;
};

export function ErrorCallout({
  severity = "error",
  kicker,
  message,
  action,
  className,
}: {
  /** Visual intensity. "error" = saturated vermillion; "warning" = softer. */
  severity?: "error" | "warning";
  /** Short label on the left (defaults to "Err" / "Note"). */
  kicker?: string;
  /** Body copy. Supports plain text only — no markdown. */
  message: string;
  action?: ErrorAction;
  className?: string;
}) {
  const isErr = severity === "error";
  const kickerLabel = kicker ?? (isErr ? "Err" : "Note");
  return (
    <div className={`relative ${className ?? ""}`}>
      <div
        role="alert"
        className={
          "flex flex-wrap items-start gap-3 rounded-pixel border-2 border-ink px-4 py-3 shadow-pixel-sm " +
          (isErr ? "bg-[#fdece9]" : "bg-[#fff7e8]")
        }
      >
        <div className="flex flex-1 min-w-0 items-start gap-2">
          <span
            className={
              "mt-[2px] font-mono text-[11px] font-semibold uppercase tracking-wide " +
              (isErr ? "text-[#a23a2a]" : "text-[#9a6b15]")
            }
          >
            {kickerLabel}
          </span>
          <p className="font-mono text-[13px] leading-snug text-ink">
            {message}
          </p>
        </div>
        {action ? <ActionButton action={action} /> : null}
      </div>
    </div>
  );
}

function ActionButton({ action }: { action: ErrorAction }) {
  const label = action.busy && action.busyLabel ? action.busyLabel : action.label;
  const inner = (
    <>
      {action.busy ? (
        <Loader2 size={11} strokeWidth={1.8} className="animate-spin" />
      ) : (
        <ArrowUpRight size={11} strokeWidth={1.8} />
      )}
      <span>{label}</span>
    </>
  );
  const cls =
    "group inline-flex items-center gap-1.5 rounded-pixel border-2 border-ink bg-accent px-3 py-1.5 font-mono text-[11px] font-semibold text-white no-underline shadow-pixel-sm transition-transform hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none disabled:cursor-not-allowed disabled:opacity-50 disabled:translate-x-0 disabled:translate-y-0";
  if (action.href) {
    return (
      <a href={action.href} className={cls}>
        {inner}
      </a>
    );
  }
  return (
    <button
      type="button"
      onClick={action.onClick}
      disabled={action.busy}
      className={cls}
    >
      {inner}
    </button>
  );
}
