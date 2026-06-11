"use client";

import { useState } from "react";
import { HelpCircle, ArrowRight } from "lucide-react";

import { WindowCard } from "@/components/ui/pixel";
import { postClawMessage } from "@/lib/api";

// ClarificationCard renders the adaptive clarification gate: the coordinator
// judged the goal ambiguous and paused with 1-2 questions. The user answers
// (or skips — "you decide"), which resumes the run with the answers folded
// into the brief. Shown in the left column instead of the chat while
// status === "awaiting_input".
export function ClarificationCard({
  jobId,
  questions,
  onResume,
}: {
  jobId: string;
  questions: string[];
  /** Flip the page back to running + restart polling. */
  onResume: () => void;
}) {
  const [answers, setAnswers] = useState<string[]>(() => questions.map(() => ""));
  const [sending, setSending] = useState(false);

  const submit = async (content: string) => {
    if (sending) return;
    setSending(true);
    onResume(); // optimistic: flip to running so the desk/chat take over
    try {
      await postClawMessage(jobId, content);
    } catch {
      /* best-effort — the poll will reflect the real state */
    }
  };

  const onSubmit = () => {
    const filled = questions
      .map((q, i) => ({ q, a: answers[i]?.trim() ?? "" }))
      .filter((x) => x.a !== "");
    if (filled.length === 0) {
      // nothing filled → treat as "you decide"
      void submit("按你的判断直接做。");
      return;
    }
    void submit(filled.map((x) => `${x.q} ${x.a}`).join("\n"));
  };

  return (
    <div className="pb-3 pt-2">
      <WindowCard title="✦ 开工前,先对一下" bodyClassName="p-4">
        <div className="mb-3 flex items-start gap-2">
          <span className="mt-0.5 grid h-6 w-6 flex-none place-items-center rounded-pixel border-2 border-ink bg-accent-soft text-accent">
            <HelpCircle size={13} strokeWidth={2} />
          </span>
          <p className="font-mono text-[12px] leading-relaxed text-ink-2">
            这个目标有点模糊,回答下面的问题能让产出更贴合你的需要(也可以直接「开干」让我自己定)。
          </p>
        </div>

        <div className="flex flex-col gap-3">
          {questions.map((q, i) => (
            <label key={i} className="block">
              <span className="mb-1 block font-mono text-[12px] font-semibold text-ink">
                {i + 1}. {q}
              </span>
              <input
                value={answers[i] ?? ""}
                onChange={(e) =>
                  setAnswers((prev) => {
                    const next = prev.slice();
                    next[i] = e.target.value;
                    return next;
                  })
                }
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    onSubmit();
                  }
                }}
                placeholder="你的回答…"
                className="w-full rounded-pixel border-2 border-line-2 bg-surface px-2.5 py-1.5 font-mono text-[12px] text-ink outline-none transition-colors focus:border-ink"
              />
            </label>
          ))}
        </div>

        <div className="mt-4 flex items-center justify-between gap-2">
          <button
            type="button"
            onClick={() => void submit("按你的判断直接做。")}
            disabled={sending}
            className="font-mono text-[12px] text-muted underline-offset-2 transition-colors hover:text-ink-2 hover:underline disabled:opacity-50"
          >
            直接开干 →
          </button>
          <button
            type="button"
            onClick={onSubmit}
            disabled={sending}
            className="inline-flex items-center gap-1.5 rounded-pixel border-2 border-ink bg-accent px-3.5 py-1.5 font-mono text-[12px] font-semibold text-white shadow-pixel-sm transition-transform hover:translate-x-[1px] hover:translate-y-[1px] active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none disabled:opacity-60"
          >
            {sending ? "开工中…" : "提交并开工"}
            <ArrowRight size={13} strokeWidth={2.2} />
          </button>
        </div>
      </WindowCard>
    </div>
  );
}
