"use client";

import { useState, type FormEvent } from "react";
import { ArrowUpRight, Loader2 } from "lucide-react";
import clsx from "clsx";

import { WindowCard } from "@/components/ui/pixel";

// ClarificationCard — Sprint L1.H2 gate UI.
//
// Shown when the topic is too vague to plan a confident outline. The
// backend's needsClarification heuristic generated 1-2 short questions;
// the user types short answers and clicks 「继续生成」. We POST the
// answers and the orchestrator resumes Phase 1 (outline) with the
// enriched topic.
//
// Design: pixel window-chrome card — pixel kicker in the title bar,
// mono question labels, boxed pixel inputs whose border snaps to ink
// (+ hard shadow) on focus.

export function ClarificationCard({
  questions,
  onSubmit,
  busy,
}: {
  questions: string[];
  onSubmit: (answers: string[]) => void | Promise<void>;
  busy?: boolean;
}) {
  // One answer slot per question; controlled inputs so the parent can
  // submit the array atomically when ready.
  const [answers, setAnswers] = useState<string[]>(() =>
    new Array(questions.length).fill(""),
  );

  const canSubmit =
    !busy && answers.some((a) => a.trim().length > 0);

  const handleSubmit = (e?: FormEvent) => {
    e?.preventDefault();
    if (!canSubmit) return;
    onSubmit(answers.map((a) => a.trim()));
  };

  return (
    <div className="mx-auto my-4 w-full max-w-2xl animate-rise">
      <form onSubmit={handleSubmit}>
        <WindowCard
          title={
            <span className="font-pixel text-[0.55rem] uppercase tracking-wide text-muted">
              <span className="text-accent">§</span> Clarification{" "}
              <span className="text-line-2">/</span>{" "}
              {questions.length} question{questions.length > 1 ? "s" : ""}
            </span>
          }
          bodyClassName="p-6"
        >
          {/* Preamble */}
          <p className="mb-5 font-mono text-[13px] leading-snug text-ink-2">
            先确认两件事，agent 就能挑出更贴近你想法的大纲。
          </p>

          {/* Question rows */}
          <div className="flex flex-col gap-5">
            {questions.map((q, i) => (
              <div key={i}>
                <label className="block font-mono text-[14px] font-semibold leading-snug text-ink">
                  <span className="mr-2 font-pixel text-[0.55rem] uppercase tracking-wide text-accent">
                    Q{i + 1}
                  </span>
                  {q}
                </label>
                <div className="mt-2 rounded-pixel border-2 border-line-2 bg-surface px-3 py-2 transition-all focus-within:border-ink focus-within:shadow-pixel-sm">
                  <input
                    type="text"
                    value={answers[i] ?? ""}
                    onChange={(e) => {
                      const next = answers.slice();
                      next[i] = e.target.value;
                      setAnswers(next);
                    }}
                    disabled={busy}
                    placeholder="一两句就够"
                    className="w-full bg-transparent font-mono text-[15px] leading-snug text-ink placeholder:text-muted focus:outline-none disabled:opacity-50"
                  />
                </div>
              </div>
            ))}
          </div>

          {/* Footer */}
          <div className="mt-6 flex items-center justify-between">
            <span className="font-mono text-[11px] text-muted">
              空着可以跳过 · 至少答一个
            </span>
            <button
              type="submit"
              disabled={!canSubmit}
              className={clsx(
                "group inline-flex items-center gap-2 rounded-pixel border-2 px-4 py-2 font-mono text-[12px] font-semibold transition-all duration-150",
                !canSubmit
                  ? "cursor-not-allowed border-line-2 bg-surface-2 text-muted"
                  : "border-ink bg-accent text-white shadow-pixel-sm hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none",
              )}
            >
              {busy ? (
                <Loader2 size={12} strokeWidth={1.8} className="animate-spin text-accent" />
              ) : (
                <ArrowUpRight size={12} strokeWidth={1.8} className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]" />
              )}
              <span>继续生成</span>
            </button>
          </div>
        </WindowCard>
      </form>
    </div>
  );
}
