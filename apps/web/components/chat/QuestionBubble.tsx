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
//   - accent 2px left bleed strip = "agent speaking"
//   - kicker mono caps + step tracker
//   - mono question body
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
      <div className="relative flex flex-col gap-2 border-l-2 border-l-line-2 pl-4 py-2 opacity-80">
        <div className="font-mono text-[10px] font-semibold tracking-wide text-muted">
          Q{view.step} / {view.total} · 已回答
        </div>
        <div className="font-mono text-[14px] leading-snug text-ink-2">
          {view.question}
        </div>
        <div className="flex items-center gap-2 font-mono text-[13px] text-ink">
          <Check className="h-3.5 w-3.5 text-grass" strokeWidth={2.5} />
          <span className={clsx(isSkip && "text-muted")}>{display || "—"}</span>
        </div>
      </div>
    );
  }

  // ─── Active question ───────────────────────────────────────────
  return (
    <div className="relative flex flex-col gap-3 border-l-2 border-l-accent pl-4 py-2">
      {/* Kicker line — step tracker + AI tag */}
      <div className="flex items-center gap-2 font-mono text-[10px] font-semibold tracking-wide text-accent">
        <Sparkles className="h-3 w-3" strokeWidth={2} />
        <span>Agent 问</span>
        <span className="text-muted">·</span>
        <span className="text-muted">
          STEP {view.step} / {view.total}
        </span>
        {view.optional ? (
          <>
            <span className="text-muted">·</span>
            <span className="text-muted">可跳过</span>
          </>
        ) : null}
      </div>

      {/* Question body */}
      <div className="font-mono text-[15px] font-bold leading-snug tracking-tight text-ink">
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
                "group inline-flex items-center gap-1.5 rounded-pixel border-2 border-line-2 bg-surface px-3.5 py-1.5",
                "font-mono text-[13px] text-ink",
                "transition-all duration-150",
                "hover:border-ink hover:bg-accent-soft",
                "active:translate-y-[1px]",
                busy && "cursor-not-allowed opacity-40 hover:border-line-2 hover:bg-surface",
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
                "inline-flex items-center gap-1.5 rounded-pixel border-2 border-dashed border-line-2 bg-transparent px-3.5 py-1.5",
                "font-mono text-[11px] font-semibold text-muted",
                "transition-colors duration-150 hover:border-ink hover:text-ink-2",
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
              "flex-1 rounded-pixel border-2 border-line-2 bg-surface px-4 py-2",
              "font-mono text-[14px] text-ink placeholder:text-muted",
              "transition-all duration-150 focus:border-ink focus:shadow-pixel-sm focus:outline-none",
              busy && "cursor-not-allowed opacity-50",
            )}
          />
          <button
            type="submit"
            disabled={busy || !freeText.trim()}
            className={clsx(
              "inline-flex items-center gap-1.5 rounded-pixel border-2 border-ink bg-accent px-4 py-2",
              "font-mono text-[13px] font-semibold text-white shadow-pixel-sm",
              "transition-all duration-150 hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover",
              "active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none",
              "disabled:cursor-not-allowed disabled:opacity-40 disabled:translate-x-0 disabled:translate-y-0",
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
                "inline-flex items-center rounded-pixel border-2 border-dashed border-line-2 bg-transparent px-3 py-2",
                "font-mono text-[11px] font-semibold text-muted",
                "transition-colors duration-150 hover:border-ink hover:text-ink-2",
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
        <div className="flex items-center gap-1.5 font-mono text-[12px] text-muted">
          <Loader2 className="h-3 w-3 animate-spin text-accent" />
          <span>处理中…agent 正在准备下一步</span>
        </div>
      ) : null}
    </div>
  );
}

// UserAnswerBubble renders the user-side bubble for a `user-answer`
// ConversationMessage. Sits below the matching QuestionBubble in the
// thread so the wizard reads as a real dialogue. Visually contrasts
// the agent bubble (left accent bleed) by sitting right-aligned with
// a quiet hairline border — same convention edit-turn user messages
// use elsewhere in the thread.
export function UserAnswerBubble({ text }: { text: string }) {
  const isSkip = text === ANSWER_PLACEHOLDER_SKIP;
  const isBack = text === "（返回上一步）";
  return (
    <div className="flex justify-end">
      <div
        className={clsx(
          "relative max-w-[80%] rounded-pixel border-2 px-4 py-2",
          isSkip || isBack
            ? "border-dashed border-line-2 bg-transparent"
            : "border-line-2 bg-surface-2",
        )}
      >
        <div className="mb-0.5 font-mono text-[10px] font-semibold tracking-wide text-muted">
          你
        </div>
        <div
          className={clsx(
            "font-mono text-[14px] leading-snug",
            isSkip || isBack ? "text-muted" : "text-ink",
          )}
        >
          {text}
        </div>
      </div>
    </div>
  );
}
