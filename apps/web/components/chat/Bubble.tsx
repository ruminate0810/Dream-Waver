"use client";

import clsx from "clsx";
import type { ReactNode } from "react";

// Pixel timeline primitives. The page reads as a retro-terminal build
// log being written before the reader: a numbered left rail, a single
// body column, hairline rules between sections. NO chat bubbles. NO
// emoji stand-ins. Status is conveyed by a pulsing violet dot (running),
// a check (done), or an X (error) sitting under the section number —
// same visual weight as the number itself, so the rhythm reads as
// bullet points in a table of contents.
//
// Exports:
//   <Brief … />   — the opening section that frames the user's prompt
//   <Phase … />   — every subsequent step of the pipeline
//
// (The old "UserBubble / AgentBubble" names are retired — the design no
// longer thinks in conversation pairs.)

export type SectionStatus = "pending" | "running" | "done" | "error";

// ── Brief: the user's prompt, framed as an opening epigraph ─────────

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
    <section className="grid grid-cols-[64px_1fr] gap-x-8 gap-y-0 border-b border-line-2 pb-14 md:grid-cols-[96px_1fr] md:gap-x-12">
      <Rail index="00" label="Brief" status="done" hideStatus />
      <div className="min-w-0">
        <blockquote className="border-l-4 border-accent pl-6 font-mono text-[26px] font-extrabold leading-[1.25] tracking-tight text-ink md:text-[30px]">
          {topic || "（无主题）"}
        </blockquote>
        {(audience || slides || style) && (
          <dl className="mt-10 grid grid-cols-3 gap-x-8 border-t border-line pt-5">
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
      <dt className="font-pixel text-[0.55rem] tracking-wide text-muted">
        {label}
      </dt>
      <dd className="mt-1.5 truncate font-mono text-base font-semibold text-ink">
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
        "grid grid-cols-[64px_1fr] gap-x-8 border-t border-line-2 py-14 md:grid-cols-[96px_1fr] md:gap-x-12",
        status !== "pending" && "animate-phase-in",
        status === "pending" && "opacity-45",
      )}
    >
      <Rail index={index} label={kicker} status={status} />
      <div className="min-w-0">
        <h2
          className={clsx(
            "font-mono font-extrabold tracking-tight text-ink",
            emphasis
              ? "text-[40px] leading-[1.02]"
              : "text-[32px] leading-[1.05] md:text-[36px]",
          )}
        >
          {zhTitle}
        </h2>
        {enSubtitle && (
          <p className="mt-2 font-mono text-[15px] leading-snug text-muted">
            {enSubtitle}
          </p>
        )}
        {children && (
          <div className="mt-8 space-y-4 text-[15px] leading-relaxed text-ink-2">
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
          "font-pixel text-[0.6rem] tracking-wide tabular-nums",
          status === "pending" ? "text-muted" : "text-ink",
        )}
      >
        {index}
      </span>
      <span
        className={clsx(
          "mt-1 font-pixel text-[0.55rem] tracking-wide",
          status === "running" && "text-accent",
          status === "done" && "text-ink-2",
          status === "error" && "text-[#a23a2a]",
          status === "pending" && "text-muted",
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
  // Sprint Z.3 — crossfade between status icons. The `key={status}`
  // forces React to remount the inner glyph when status changes, and
  // the .dw-status-flip class kicks off a 200ms opacity+scale fade-in
  // (defined in globals.css). Pending → running → done now feels like
  // a progress beat instead of a hard icon swap. Reduced-motion users
  // get the icon swap without animation.
  return (
    <span key={status} className={clsx("dw-status-flip", className)}>
      {status === "pending" ? (
        <span
          aria-hidden
          className="block h-2 w-2 rounded-full border border-muted"
        />
      ) : status === "running" ? (
        <span
          role="status"
          aria-label="running"
          className="relative block h-2 w-2"
        >
          <span className="absolute inset-0 rounded-full bg-accent" />
          <span
            aria-hidden
            className="absolute -inset-[5px] rounded-full border border-accent/40"
            style={{ animation: "ping 1800ms cubic-bezier(0,0,0.2,1) infinite" }}
          />
        </span>
      ) : status === "done" ? (
        <svg
          aria-label="done"
          viewBox="0 0 14 14"
          className="h-3.5 w-3.5 text-grass"
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
      ) : (
        <svg
          aria-label="error"
          viewBox="0 0 14 14"
          className="h-3 w-3 text-[#d4503a]"
          fill="none"
        >
          <path
            d="M3 3L11 11M11 3L3 11"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
          />
        </svg>
      )}
    </span>
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
      <div className="flex items-baseline justify-between font-pixel text-[0.55rem] tracking-wide text-ink-2">
        <span>
          <span className="tabular-nums text-ink">{now}</span>
          <span className="mx-1.5 text-muted">of</span>
          <span className="tabular-nums">{total}</span>
          <span className="ml-2 text-muted">plates set</span>
        </span>
        <span className="tabular-nums text-muted">{pct}%</span>
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
              "h-[5px] transition-colors duration-500",
              i < now ? "bg-accent" : "bg-line",
            )}
          />
        ))}
      </div>
    </div>
  );
}
