"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { Check, Loader2, Sparkles } from "lucide-react";
import clsx from "clsx";

import type { WizardStepView } from "./transport";

// QuestionBubble — Sprint AA.4 inline replacement for the modal
// WizardCard. Renders the agent's wizard question as a chat bubble
// living in the message stream (instead of a floating card pinned to
// the bottom). For `select` kind we render the options as inline
// chips that auto-submit on click; for `free-text` kind a small
// text input + send button. Once the user answers, the bubble
// collapses to a compact "answered" form — the matching user-answer
// bubble renders right below in the dialogue.
//
// Visual register matches the chat thread:
//   - vermillion 2px left bleed strip = "agent speaking"
//   - kicker mono caps + step tracker
//   - display-serif question body
//   - chip rail (select) OR composer-style input (free-text)
//
// Deliberately smaller / more conversational than the WizardCard
// modal: no radio-card lucide icons, no breadcrumb (the message
// thread above IS the breadcrumb), no separate "back" button (the
// previous question's bubble is still visible — click 修改 there
// instead). Power-user features can be added back later; the chat
// surface stays clean.

const ANSWER_PLACEHOLDER_SKIP = "（跳过）";

export function QuestionBubble({
  view,
  answered,
  answerText,
  busy,
  onSubmit,
}: {
  view: WizardStepView;
  /** True when a matching user-answer message already exists in the
   *  turn's messages timeline — render in collapsed/answered mode. */
  answered: boolean;
  /** The user's reply when `answered`. Drives the inline echo. */
  answerText?: string;
  busy?: boolean;
  onSubmit: (step: number, answer: string, skip: boolean) => void | Promise<void>;
}) {
  const isSelect = view.kind === "select" || view.kind === "scenario";

  const [freeText, setFreeText] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  // Auto-focus the free-text input the moment a new free-text
  // question appears so the user can start typing without a click.
  useEffect(() => {
    if (!answered && !isSelect && inputRef.current) {
      inputRef.current.focus();
    }
  }, [view.step, view.kind, answered, isSelect]);

  const handleSelect = (value: string) => {
    if (busy || answered) return;
    void onSubmit(view.step, value, false);
  };

  const handleFreeSubmit = (e: FormEvent) => {
    e.preventDefault();
    const trimmed = freeText.trim();
    if (!trimmed || busy || answered) return;
    void onSubmit(view.step, trimmed, false);
  };

  const handleSkip = () => {
    if (!view.optional || busy || answered) return;
    void onSubmit(view.step, "", true);
  };

  // ─── Answered (collapsed) — compact summary ────────────────────
  if (answered) {
    const display = answerText ?? "";
    const isSkip = display === ANSWER_PLACEHOLDER_SKIP;
    return (
      <div className="relative flex flex-col gap-2 border-l-2 border-l-ink-faint pl-4 py-2 opacity-80">
        <div className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-ink-faint">
          Q{view.step} / {view.total} · 已回答
        </div>
        <div className="font-display text-base leading-snug text-ink-soft">
          {view.question}
        </div>
        <div className="flex items-center gap-2 font-display text-sm italic text-ink">
          <Check className="h-3.5 w-3.5 text-[#B5371E]" strokeWidth={2.5} />
          <span className={clsx(isSkip && "italic text-ink-faint")}>{display || "—"}</span>
        </div>
      </div>
    );
  }

  // ─── Active question ───────────────────────────────────────────
  return (
    <div className="relative flex flex-col gap-3 border-l-2 border-l-[#B5371E] pl-4 py-2">
      {/* Kicker line — step tracker + AI tag */}
      <div className="flex items-center gap-2 font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[#B5371E]">
        <Sparkles className="h-3 w-3" strokeWidth={2} />
        <span>Agent 问</span>
        <span className="text-ink-faint">·</span>
        <span className="text-ink-faint">
          STEP {view.step} / {view.total}
        </span>
        {view.optional ? (
          <>
            <span className="text-ink-faint">·</span>
            <span className="text-ink-faint">可跳过</span>
          </>
        ) : null}
      </div>

      {/* Question body */}
      <div className="font-display text-lg leading-snug text-ink">
        {view.question}
      </div>

      {/* Body — select chips OR free-text composer */}
      {isSelect ? (
        <div className="flex flex-wrap gap-2 pt-1">
          {(view.options ?? []).map((opt) => (
            <button
              key={opt.value}
              type="button"
              disabled={busy}
              onClick={() => handleSelect(opt.value)}
              className={clsx(
                "group inline-flex items-center gap-1.5 rounded-full border border-ink/15 bg-paper px-3.5 py-1.5",
                "font-display text-sm text-ink",
                "transition-all duration-200 ease-out",
                "hover:border-[#B5371E] hover:bg-[#FFF6F2] hover:text-[#B5371E]",
                "active:scale-[0.98]",
                busy && "cursor-not-allowed opacity-40 hover:border-ink/15 hover:bg-paper hover:text-ink",
              )}
            >
              {opt.label}
            </button>
          ))}
          {view.optional ? (
            <button
              type="button"
              disabled={busy}
              onClick={handleSkip}
              className={clsx(
                "inline-flex items-center gap-1.5 rounded-full border border-dashed border-ink/20 bg-transparent px-3.5 py-1.5",
                "font-mono-jb text-[11px] uppercase tracking-[0.2em] text-ink-faint",
                "transition-colors duration-200 hover:border-ink/40 hover:text-ink-soft",
                busy && "cursor-not-allowed opacity-40",
              )}
            >
              跳过
            </button>
          ) : null}
        </div>
      ) : (
        <form onSubmit={handleFreeSubmit} className="flex items-center gap-2 pt-1">
          <input
            ref={inputRef}
            type="text"
            value={freeText}
            onChange={(e) => setFreeText(e.target.value)}
            disabled={busy}
            placeholder={view.placeholder || "回答 agent 的问题…"}
            className={clsx(
              "flex-1 rounded-full border border-ink/15 bg-paper px-4 py-2",
              "font-display text-base text-ink placeholder:text-ink-faint",
              "focus:border-[#B5371E] focus:outline-none focus:ring-1 focus:ring-[#B5371E]/30",
              "transition-colors duration-200",
              busy && "cursor-not-allowed opacity-50",
            )}
          />
          <button
            type="submit"
            disabled={busy || !freeText.trim()}
            className={clsx(
              "inline-flex items-center gap-1.5 rounded-full bg-[#B5371E] px-4 py-2",
              "font-display text-sm text-paper",
              "transition-all duration-200 hover:bg-[#9a2e16] active:scale-[0.98]",
              "disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-[#B5371E]",
            )}
          >
            {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
            <span>发送</span>
          </button>
          {view.optional ? (
            <button
              type="button"
              disabled={busy}
              onClick={handleSkip}
              className={clsx(
                "inline-flex items-center rounded-full border border-dashed border-ink/20 bg-transparent px-3 py-2",
                "font-mono-jb text-[10px] uppercase tracking-[0.2em] text-ink-faint",
                "transition-colors duration-200 hover:border-ink/40 hover:text-ink-soft",
                busy && "cursor-not-allowed opacity-40",
              )}
            >
              跳过
            </button>
          ) : null}
        </form>
      )}

      {/* Busy line — shows the user that the agent is on the next step */}
      {busy ? (
        <div className="flex items-center gap-1.5 font-display text-xs italic text-ink-faint">
          <Loader2 className="h-3 w-3 animate-spin" />
          <span>处理中…agent 正在准备下一步</span>
        </div>
      ) : null}
    </div>
  );
}

// UserAnswerBubble renders the user-side bubble for a `user-answer`
// ConversationMessage. Sits below the matching QuestionBubble in the
// thread so the wizard reads as a real dialogue. Visually contrasts
// the agent bubble (left vermillion bleed) by sitting right-aligned
// with a soft ink border — same convention edit-turn user messages
// use elsewhere in the thread.
export function UserAnswerBubble({ text }: { text: string }) {
  const isSkip = text === ANSWER_PLACEHOLDER_SKIP;
  const isBack = text === "（返回上一步）";
  return (
    <div className="flex justify-end">
      <div
        className={clsx(
          "relative max-w-[80%] rounded-2xl border bg-paper px-4 py-2",
          isSkip || isBack
            ? "border-dashed border-ink/20"
            : "border-ink/12 bg-[#FBF7F4]",
        )}
      >
        <div className="font-mono-jb text-[9px] uppercase tracking-[0.3em] text-ink-faint mb-0.5">
          你
        </div>
        <div
          className={clsx(
            "font-display text-base leading-snug",
            isSkip || isBack ? "italic text-ink-faint" : "text-ink",
          )}
        >
          {text}
        </div>
      </div>
    </div>
  );
}
