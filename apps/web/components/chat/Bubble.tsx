"use client";

import clsx from "clsx";
import type { ReactNode } from "react";

// Editorial timeline primitives. The page now reads as a typeset article
// being written before the reader: a numbered left rail, a single body
// column, hairline rules between sections. NO chat bubbles. NO emoji
// stand-ins. Status is conveyed by a 2.5px vermillion dot (running),
// a hand-drawn check (done), or an X (error) sitting under the section
// number — same visual weight as the number itself, so the rhythm reads
// as bullet points in a published table of contents.
//
// Exports:
//   <Brief … />   — the opening section that frames the user's prompt
//   <Phase … />   — every subsequent step of the pipeline
//
// (The old "UserBubble / AgentBubble" names are retired — the design no
// longer thinks in conversation pairs.)

export type SectionStatus = "pending" | "running" | "done" | "error";

// ── Brief: the user's prompt, framed as a published epigraph ────────

export function Brief({
  topic,
  audience,
  slides,
  style,
}: {
  topic: string;
  audience?: string;
  slides?: number;
  style?: string;
}) {
  return (
    <section className="grid grid-cols-[64px_1fr] gap-x-8 gap-y-0 border-b border-[color:var(--rule)] pb-14 md:grid-cols-[96px_1fr] md:gap-x-12">
      <Rail index="00" label="Brief" status="done" hideStatus />
      <div className="min-w-0">
        <blockquote className="border-l-[3px] border-[color:var(--vermillion)] pl-6 font-display text-[34px] italic leading-[1.18] tracking-tight text-[color:var(--ink)] md:text-[40px]">
          {topic || "（无主题）"}
        </blockquote>
        {(audience || slides || style) && (
          <dl className="mt-10 grid grid-cols-3 gap-x-8 border-t border-[color:var(--rule)] pt-5">
            {audience && <Pair label="Audience" value={audience} />}
            {slides ? <Pair label="Length" value={`${slides} pp.`} /> : null}
            {style && <Pair label="Style" value={style} />}
          </dl>
        )}
      </div>
    </section>
  );
}

function Pair({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
        {label}
      </dt>
      <dd className="mt-1.5 truncate font-display text-lg text-[color:var(--ink)]">
        {value}
      </dd>
    </div>
  );
}

// ── Phase: one numbered section of the generation timeline ──────────

export function Phase({
  index,
  status,
  kicker,
  zhTitle,
  enSubtitle,
  children,
  emphasis,
}: {
  index: string;          // "01", "02", …
  status: SectionStatus;
  kicker: string;         // "COMPOSITION"
  zhTitle: string;        // "构思"
  enSubtitle?: string;    // "Drafting the architecture"
  children?: ReactNode;
  emphasis?: boolean;     // true for the final "Issue" section that hosts preview + CTAs
}) {
  return (
    <section
      className={clsx(
        "grid grid-cols-[64px_1fr] gap-x-8 border-t border-[color:var(--rule)] py-14 md:grid-cols-[96px_1fr] md:gap-x-12",
        status !== "pending" && "animate-phase-in",
        status === "pending" && "opacity-45",
      )}
    >
      <Rail index={index} label={kicker} status={status} />
      <div className="min-w-0">
        <h2
          className={clsx(
            "font-display tracking-tight text-[color:var(--ink)]",
            emphasis
              ? "text-[56px] leading-[0.98]"
              : "text-[46px] leading-[1.02] md:text-[52px]",
          )}
        >
          {zhTitle}
        </h2>
        {enSubtitle && (
          <p className="mt-2 font-display italic text-[20px] leading-snug text-[color:var(--ink-faint)]">
            {enSubtitle}
          </p>
        )}
        {children && (
          <div className="mt-8 space-y-4 text-[15px] leading-relaxed text-[color:var(--ink-soft)]">
            {children}
          </div>
        )}
      </div>
    </section>
  );
}

// ── Rail: number + label + status glyph, vertically aligned ─────────

function Rail({
  index,
  label,
  status,
  hideStatus,
}: {
  index: string;
  label: string;
  status: SectionStatus;
  hideStatus?: boolean;
}) {
  return (
    <aside className="flex flex-col">
      <span
        className={clsx(
          "font-mono-jb text-[11px] font-medium uppercase tracking-[0.24em] tabular-nums",
          status === "pending" ? "text-[color:var(--ink-faint)]" : "text-[color:var(--ink)]",
        )}
      >
        {index}
      </span>
      <span
        className={clsx(
          "mt-1 font-mono-jb text-[10px] uppercase tracking-[0.22em]",
          status === "running" && "text-[color:var(--vermillion)]",
          status === "done" && "text-[color:var(--ink-soft)]",
          status === "error" && "text-red-700",
          status === "pending" && "text-[color:var(--ink-faint)]",
        )}
      >
        {label}
      </span>
      {!hideStatus && <StatusGlyph status={status} className="mt-7" />}
    </aside>
  );
}

function StatusGlyph({
  status,
  className,
}: {
  status: SectionStatus;
  className?: string;
}) {
  if (status === "pending") {
    return (
      <span
        aria-hidden
        className={clsx(
          "block h-2 w-2 rounded-full border border-[color:var(--ink-faint)]",
          className,
        )}
      />
    );
  }
  if (status === "running") {
    return (
      <span
        role="status"
        aria-label="running"
        className={clsx("relative block h-2 w-2", className)}
      >
        <span className="absolute inset-0 rounded-full bg-[color:var(--vermillion)]" />
        <span
          aria-hidden
          className="absolute -inset-[5px] rounded-full border border-[color:var(--vermillion)]/40"
          style={{ animation: "ping 1800ms cubic-bezier(0,0,0.2,1) infinite" }}
        />
      </span>
    );
  }
  if (status === "done") {
    return (
      <svg
        aria-label="done"
        viewBox="0 0 14 14"
        className={clsx("h-3.5 w-3.5 text-[color:var(--ink)]", className)}
        fill="none"
      >
        <path
          d="M2.5 7.5L5.5 10.5L11.5 3.5"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }
  return (
    <svg
      aria-label="error"
      viewBox="0 0 14 14"
      className={clsx("h-3 w-3 text-red-700", className)}
      fill="none"
    >
      <path
        d="M3 3L11 11M11 3L3 11"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
      />
    </svg>
  );
}

// ── Press progress: the per-slide ticker used in the Rendering phase ─

export function PressTicker({
  now,
  total,
}: {
  now: number;
  total: number;
}) {
  const pct = total > 0 ? Math.min(100, Math.round((now / total) * 100)) : 0;
  const cells = Math.max(total, 1);
  return (
    <div>
      <div className="flex items-baseline justify-between font-mono-jb text-[11px] uppercase tracking-[0.22em] text-[color:var(--ink-soft)]">
        <span>
          <span className="tabular-nums text-[color:var(--ink)]">{now}</span>
          <span className="mx-1.5 text-[color:var(--ink-faint)]">of</span>
          <span className="tabular-nums">{total}</span>
          <span className="ml-2 text-[color:var(--ink-faint)]">plates set</span>
        </span>
        <span className="tabular-nums text-[color:var(--ink-faint)]">{pct}%</span>
      </div>
      <div
        className="mt-3 grid gap-1.5"
        style={{
          gridTemplateColumns: `repeat(${cells}, minmax(0, 1fr))`,
        }}
      >
        {Array.from({ length: cells }).map((_, i) => (
          <span
            key={i}
            className={clsx(
              "h-[3px] rounded-full transition-colors duration-500",
              i < now
                ? "bg-[color:var(--vermillion)]"
                : "bg-[color:var(--rule)]",
            )}
          />
        ))}
      </div>
    </div>
  );
}
