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
// action. Layout: 3px vermillion bleed strip (registration mark) +
// hairline border + ink-on-paper body + optional mono-caps CTA on
// right edge. Width adapts to container.

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
        className={
          isErr
            ? "h-[3px] w-[36px] bg-[color:var(--vermillion)]"
            : "h-[3px] w-[36px] bg-[color:var(--ink-soft)]"
        }
        aria-hidden
      />
      <div
        role="alert"
        className={
          "flex flex-wrap items-start gap-3 border px-4 py-3 " +
          (isErr
            ? "border-[color:var(--vermillion)]/45 bg-[color:var(--vermillion)]/[0.06]"
            : "border-[color:var(--ink-soft)]/30 bg-[color:var(--ink-soft)]/[0.04]")
        }
      >
        <div className="flex flex-1 min-w-0 items-start gap-2">
          <span
            className={
              "mt-[2px] font-mono-jb text-[10px] uppercase tracking-[0.26em] " +
              (isErr
                ? "text-[color:var(--vermillion)]"
                : "text-[color:var(--ink-soft)]")
            }
          >
            {kickerLabel}
          </span>
          <p className="font-display text-[14px] italic leading-snug text-[color:var(--ink)]">
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
        <ArrowUpRight
          size={11}
          strokeWidth={1.8}
          className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]"
        />
      )}
      <span>{label}</span>
    </>
  );
  const cls =
    "group inline-flex items-center gap-1.5 border border-[color:var(--ink)] bg-[color:var(--ink)] px-3 py-1.5 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--paper)] no-underline transition-all hover:bg-[color:var(--vermillion)] active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50";
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
