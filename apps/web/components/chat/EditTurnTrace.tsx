"use client";

import { BusyHint } from "./BusyHint";
import { Phase, type SectionStatus } from "./Bubble";
import { ThoughtCollapse } from "./ThoughtCollapse";
import { ToolStrip } from "./ToolStrip";
import { ErrorCallout } from "../ui/ErrorCallout";

import type { Thought, ToolCallEntry, Turn } from "./session";

// EditTurnTrace is the uniform renderer for every Turn after Turn 0.
// One <Phase> block per turn, labelled "Revision · 修订", with:
//
//   1. The user's typed instruction shown as italic serif marginalia
//      (a vermillion-rule blockquote, like an editor's hand-written note)
//   2. A flat ToolStrip of every tool call this turn made
//   3. A collapsed ThoughtCollapse of every LLM thought across steps
//   4. (Sprint U2.1) — if the turn errored, a prominent banner card
//      with "重试" button that re-dispatches the original userMessage.
//      Without this, the silent italic red text in the original design
//      was easy to miss after a long DeepSeek failure.

export function EditTurnTrace({
  turn,
  index,
  onRetry,
  retrying,
}: {
  turn: Turn;
  index: number;
  /** When supplied, the error banner shows a "重试" button that calls this. */
  onRetry?: () => void;
  /** True while the retry is in-flight; disables the button + shows spinner. */
  retrying?: boolean;
}) {
  const status: SectionStatus =
    turn.status === "running" ? "running" : turn.status === "error" ? "error" : "done";

  // Flatten steps for the strip + collapse. Step structure is an
  // agent-loop implementation detail; for a 1-tool edit there's only
  // ever one step anyway.
  const toolCalls: ToolCallEntry[] = turn.steps.flatMap((s) => s.toolCalls);
  const thoughts: Thought[] = turn.steps
    .map((s) => s.thought)
    .filter((t): t is Thought => !!t);

  return (
    <Phase
      index={pad2(index)}
      status={status}
      kicker="Revision"
      zhTitle="修订"
      enSubtitle={
        turn.status === "running" ? "Applying the change…" : "Change applied"
      }
    >
      {turn.userMessage ? (
        // Sprint Z.6 — turn user-message fades in (animate-phase-in
        // gives opacity 0→1 + translateY 10px). Only fires once on
        // turn open, then stays put.
        <blockquote className="animate-phase-in border-l-[3px] border-[color:var(--vermillion)]/55 pl-5 font-display text-[22px] italic leading-snug text-[color:var(--ink)]">
          {turn.userMessage}
        </blockquote>
      ) : null}

      {turn.errorMsg ? (
        <div className="mt-4 mb-1">
          <ErrorCallout
            message={turn.errorMsg}
            action={
              onRetry
                ? { label: "重试", busyLabel: "重试中", onClick: onRetry, busy: retrying }
                : undefined
            }
          />
        </div>
      ) : null}

      {/* Bridge the silent gap between dispatchUserMessage and the
          first backend event. Reducer drops busyHint the moment any
          real progress lands, so this is single-shot per turn. */}
      {turn.busyHint ? <BusyHint kind={turn.busyHint.kind} /> : null}

      <ToolStrip calls={toolCalls} />
      <ThoughtCollapse thoughts={thoughts} />
    </Phase>
  );
}

// (ErrorBanner removed in Sprint AF.3 — replaced by the shared
// <ErrorCallout> imported above. Same visual, single source of truth.)

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`;
}
