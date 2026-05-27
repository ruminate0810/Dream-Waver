"use client";

import { useEffect, useRef } from "react";

import type { SlideJob } from "@/lib/api";

import { Brief } from "./Bubble";
import { EditTurnTrace } from "./EditTurnTrace";
import { FollowupInput } from "./FollowupInput";
import { SlidesOpening } from "./SlidesOpening";
import { useAgentSession } from "./session";

// Chat is the orchestrator of the editorial generation timeline, but
// reduced — by design — to a thin presentation layer. Two responsibilities:
//
//   1. Render the page chrome (Masthead + Brief) — derived from job.
//   2. Iterate `turns` from useAgentSession() and dispatch to the right
//      per-turn renderer:
//        - turn.kind === "initial" → <SlidesOpening>  (4-phase editorial chrome)
//        - turn.kind === "edit"    → <EditTurnTrace>  (uniform step timeline)
//
// All state lives in useAgentSession; all WS plumbing lives in
// <AgentSessionProvider> (mounted by the page). Chat itself is a leaf.

export function Chat({
  job,
  sessionId,
  compact = false,
}: {
  job: SlideJob;
  sessionId: string;
  // compact=true means the right-hand <LivePreviewStack> is rendering
  // the live editable deck, so the issue-phase PNG grid is suppressed
  // (avoids showing the same deck twice).
  compact?: boolean;
}) {
  const session = useAgentSession(job);
  const tailRef = useRef<HTMLDivElement | null>(null);

  // Scroll to bottom whenever a new turn appears or the current turn
  // closes — gives the user a gentle "we made progress" cue.
  useEffect(() => {
    tailRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [session.turns.length, session.busy]);

  const input = job.input ?? {};
  const initialTurn = session.turns.find((t) => t.kind === "initial");
  const editTurns = session.turns.filter((t) => t.kind === "edit");
  const initialDone = initialTurn?.status === "done";

  return (
    <article
      className={
        compact
          ? "relative w-full px-1 pb-32 pt-6"
          : "relative mx-auto max-w-3xl px-6 pb-40 pt-14 md:max-w-4xl md:px-12"
      }
    >
      <Masthead startedAt={job.started_at} sessionId={sessionId} mode={job.mode} />

      <Brief
        topic={input.topic || job.title || ""}
        audience={input.audience}
        slides={input.slide_count}
        style={styleLabel(input.style)}
      />

      <SlidesOpening
        turn={initialTurn}
        job={job}
        previewVersion={session.previewVersion}
        compact={compact}
      />

      {editTurns.map((turn, i) => (
        <EditTurnTrace
          // The editorial number starts at V (after the 4 opening phases),
          // so the first follow-up reads "05 · Revision". Matches what
          // the magazine spread used to do.
          key={turn.id}
          turn={turn}
          index={i + 5}
          // Sprint U2.1 — retry re-dispatches the user's original
          // message, opening a NEW turn (the old errored turn stays
          // visible as history). Only enabled when we have a userMessage
          // to replay (edit turns always do; defensive for safety).
          onRetry={
            turn.userMessage
              ? () => session.dispatchUserMessage(turn.userMessage!)
              : undefined
          }
          retrying={session.busy}
        />
      ))}

      {/* Follow-up input appears once the initial generation closes,
          regardless of mode — pipeline-mode decks can still accept
          edits via the agent path on the backend. */}
      {initialDone && job.mode !== "pipeline" ? (
        <FollowupInput
          busy={session.busy}
          onSubmit={session.dispatchUserMessage}
        />
      ) : null}

      <div ref={tailRef} />
    </article>
  );
}

// ─── Masthead ─────────────────────────────────────────────────────────

function Masthead({
  startedAt,
  sessionId,
  mode,
}: {
  startedAt?: string;
  sessionId: string;
  mode?: SlideJob["mode"];
}) {
  const date = startedAt
    ? new Date(startedAt).toISOString().slice(0, 10)
    : new Date().toISOString().slice(0, 10);
  const time = startedAt
    ? new Date(startedAt).toISOString().slice(11, 16) + " UTC"
    : "";
  const ref = sessionId ? sessionId.slice(0, 4).toUpperCase() : "————";
  return (
    <header className="mb-14 border-b border-[color:var(--rule)] pb-6">
      <p className="flex flex-wrap items-baseline gap-x-5 gap-y-1 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
        <span>Dream-Waver</span>
        <Sep />
        <span>Composition Log</span>
        <Sep />
        <span className="tabular-nums">No. {ref}</span>
        <Sep />
        <span className="tabular-nums">{date}</span>
        {time && (
          <>
            <Sep />
            <span className="tabular-nums">{time}</span>
          </>
        )}
        {mode && (
          <>
            <Sep />
            <span className="tabular-nums text-[color:var(--vermillion)]">
              {mode === "agent" ? "Agent" : "Pipeline"} mode
            </span>
          </>
        )}
      </p>
    </header>
  );
}

function Sep() {
  return (
    <span aria-hidden className="text-[color:var(--ink-faint)]">
      ·
    </span>
  );
}

// styleLabel maps the freeform `style` input into a 1-2-word editorial
// kicker that fits the Brief layout. Falls through to the raw value if
// no canonical match.
function styleLabel(s?: string): string | undefined {
  if (!s) return undefined;
  const lower = s.toLowerCase();
  if (lower.includes("corporate") || lower.includes("商务")) return "Corporate";
  if (lower.includes("minimal") || lower.includes("极简")) return "Minimalist";
  if (lower.includes("pitch") || lower.includes("路演")) return "Pitch Deck";
  if (lower.includes("academic") || lower.includes("学术")) return "Academic";
  if (lower.includes("playful") || lower.includes("活泼")) return "Playful";
  return s;
}
