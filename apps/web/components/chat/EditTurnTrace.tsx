"use client";

import { ArrowUpRight, Loader2 } from "lucide-react";

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
        <blockquote className="border-l-[3px] border-[color:var(--vermillion)]/55 pl-5 font-display text-[22px] italic leading-snug text-[color:var(--ink)]">
          {turn.userMessage}
        </blockquote>
      ) : null}

      {turn.errorMsg ? (
        <ErrorBanner
          message={turn.errorMsg}
          onRetry={onRetry}
          retrying={retrying}
        />
      ) : null}

      <ToolStrip calls={toolCalls} />
      <ThoughtCollapse thoughts={thoughts} />
    </Phase>
  );
}

// ErrorBanner — Sprint U2.1. Replaces the original mt-3 italic red `<p>`
// with a card that the user can't miss + retry control. Shape mirrors
// the /slides/new error banner (vermillion bleed strip + tight border)
// so the visual language is consistent.
function ErrorBanner({
  message,
  onRetry,
  retrying,
}: {
  message: string;
  onRetry?: () => void;
  retrying?: boolean;
}) {
  return (
    <div className="mt-4 mb-1">
      <div className="h-[3px] w-[36px] bg-[color:var(--vermillion)]" />
      <div
        role="alert"
        className="flex flex-wrap items-start gap-3 border border-[color:var(--vermillion)]/45 bg-[color:var(--vermillion)]/[0.06] px-4 py-3"
      >
        <div className="flex flex-1 min-w-0 items-start gap-2">
          <span className="mt-[2px] font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--vermillion)]">
            Err
          </span>
          <p className="font-display text-[14px] italic leading-snug text-[color:var(--ink)]">
            {message}
          </p>
        </div>
        {onRetry ? (
          <button
            type="button"
            onClick={onRetry}
            disabled={retrying}
            className="group inline-flex items-center gap-1.5 border border-[color:var(--ink)] bg-[color:var(--ink)] px-3 py-1.5 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--paper)] transition-all hover:bg-[color:var(--vermillion)] active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
          >
            {retrying ? (
              <Loader2 size={11} strokeWidth={1.8} className="animate-spin" />
            ) : (
              <ArrowUpRight
                size={11}
                strokeWidth={1.8}
                className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]"
              />
            )}
            <span>{retrying ? "重试中" : "重试"}</span>
          </button>
        ) : null}
      </div>
    </div>
  );
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`;
}
