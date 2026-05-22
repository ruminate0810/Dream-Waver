"use client";

import { Phase, type SectionStatus } from "./Bubble";
import { ThoughtCollapse } from "./ThoughtCollapse";
import { ToolStrip } from "./ToolStrip";

import type { Thought, ToolCallEntry, Turn } from "./session";

// EditTurnTrace is the uniform renderer for every Turn after Turn 0.
// One <Phase> block per turn, labelled "Revision · 修订", with:
//
//   1. The user's typed instruction shown as italic serif marginalia
//      (a vermillion-rule blockquote, like an editor's hand-written note)
//   2. A flat ToolStrip of every tool call this turn made
//   3. A collapsed ThoughtCollapse of every LLM thought across steps
//
// This intentionally does NOT split into editorial phases — the slides
// skill's 4-phase chrome belongs to Turn 0 only. Edits are always one
// or two tool calls, so a flat list is the honest representation.

export function EditTurnTrace({
  turn,
  index,
}: {
  turn: Turn;
  index: number;
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
        <blockquote className="border-l-[3px] border-[color:var(--vermillion)]/55 pl-5 font-display text-[22px] italic leading-snug text-[color:var(--ink)]">
          {turn.userMessage}
        </blockquote>
      ) : null}

      {turn.errorMsg ? (
        <p className="mt-3 font-display text-base italic text-red-800">
          {turn.errorMsg}
        </p>
      ) : null}

      <ToolStrip calls={toolCalls} />
      <ThoughtCollapse thoughts={thoughts} />
    </Phase>
  );
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`;
}
