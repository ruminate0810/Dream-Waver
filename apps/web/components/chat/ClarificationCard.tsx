"use client";

import { useState, type FormEvent } from "react";
import { ArrowUpRight, Loader2 } from "lucide-react";
import clsx from "clsx";

// ClarificationCard — Sprint L1.H2 gate UI.
//
// Shown when the topic is too vague to plan a confident outline. The
// backend's needsClarification heuristic generated 1-2 short questions;
// the user types short answers and clicks 「继续生成」. We POST the
// answers and the orchestrator resumes Phase 1 (outline) with the
// enriched topic.
//
// Design: same paper-card register as EditPopover — vermillion press-
// strip on top, mono-caps kicker, italic serif marginalia for the
// questions, plain inputs underlined with a hairline that goes
// vermillion on focus.

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
    <div className="mx-auto my-4 w-full max-w-2xl">
      {/* Top vermillion bleed strip — press registration mark */}
      <div className="h-[3px] w-[42px] bg-[color:var(--vermillion)]" />

      <form
        onSubmit={handleSubmit}
        className="relative border border-[color:var(--ink)]/15 bg-[#FBF9F2] p-6"
        style={{
          boxShadow:
            "0 2px 0 rgba(26,22,20,0.04), 0 18px 32px -20px rgba(50,40,32,0.32)",
        }}
      >
        {/* Header — kicker */}
        <div className="mb-4 flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
          <span className="text-[color:var(--vermillion)]">§</span>
          <span>Clarification</span>
          <span className="text-[color:var(--ink-faint)]">/</span>
          <span>{questions.length} question{questions.length > 1 ? "s" : ""}</span>
        </div>

        {/* Marginalia preamble */}
        <p className="mb-5 font-display text-[14px] italic leading-snug text-[color:var(--ink-soft)]">
          先确认两件事，agent 就能挑出更贴近你想法的大纲。
        </p>

        {/* Question rows */}
        <div className="flex flex-col gap-5">
          {questions.map((q, i) => (
            <div key={i}>
              <label className="block font-display text-[15px] leading-snug text-[color:var(--ink)]">
                <span className="mr-2 font-mono-jb text-[10px] uppercase tracking-[0.2em] text-[color:var(--vermillion)]">
                  Q{i + 1}
                </span>
                {q}
              </label>
              <div className="mt-2 border-b border-[color:var(--ink)]/40 pb-1 transition-colors focus-within:border-[color:var(--vermillion)]">
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
                  className="w-full bg-transparent font-display text-[17px] leading-snug text-[color:var(--ink)] placeholder:font-display placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
                />
              </div>
            </div>
          ))}
        </div>

        {/* Footer */}
        <div className="mt-6 flex items-center justify-between">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
            空着可以跳过 · 至少答一个
          </span>
          <button
            type="submit"
            disabled={!canSubmit}
            className={clsx(
              "group inline-flex items-center gap-2 px-4 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] transition-all duration-200",
              !canSubmit
                ? "cursor-not-allowed text-[color:var(--ink-faint)]"
                : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)] active:translate-y-[1px]",
            )}
          >
            {busy ? (
              <Loader2 size={12} strokeWidth={1.8} className="animate-spin" />
            ) : (
              <ArrowUpRight size={12} strokeWidth={1.8} className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]" />
            )}
            <span>继续生成</span>
          </button>
        </div>
      </form>
    </div>
  );
}
