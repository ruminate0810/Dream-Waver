"use client";

import { useState, type FormEvent, type KeyboardEvent } from "react";
import { ArrowUpRight, Loader2 } from "lucide-react";
import clsx from "clsx";

// FollowupInput renders below the "Issue" section after the deck is
// rendered. The user types an edit instruction (Chinese OK), the parent
// component fires postSlideMessage, and the same chat WebSocket the
// initial generation used pushes a fresh set of step.start /
// llm.thought / tool.start / tool.end events for the next turn.
//
// Visual language matches the editorial pages: paper-coloured field with
// a hairline border, mono caps caption, vermillion accent on hover.

export function FollowupInput({
  busy,
  onSubmit,
}: {
  busy: boolean;
  onSubmit: (text: string) => void | Promise<void>;
}) {
  const [text, setText] = useState("");

  const send = (e?: FormEvent | KeyboardEvent) => {
    e?.preventDefault();
    const t = text.trim();
    if (!t || busy) return;
    onSubmit(t);
    setText("");
  };

  return (
    <section className="mt-20 border-t border-[color:var(--rule)] pt-10">
      <p className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
        Follow-up · 继续修改
      </p>

      <form
        onSubmit={send}
        className="mt-4 flex items-end gap-3 border-b border-[color:var(--ink)]/15 pb-3 transition-colors focus-within:border-[color:var(--vermillion)]/70"
      >
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) send(e);
          }}
          rows={1}
          disabled={busy}
          placeholder='例: "把第 3 页改得更激进、加点数据"  ·  "删掉第 5 页"'
          className="flex-1 resize-none bg-transparent font-display text-[20px] leading-snug text-[color:var(--ink)] placeholder:font-display placeholder:text-[18px] placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
        />

        <button
          type="submit"
          disabled={busy || !text.trim()}
          aria-label="Send follow-up"
          className={clsx(
            "inline-flex h-10 w-10 shrink-0 items-center justify-center transition-colors",
            busy || !text.trim()
              ? "cursor-not-allowed text-[color:var(--ink-faint)]"
              : "text-[color:var(--ink)] hover:bg-[color:var(--ink)] hover:text-[color:var(--paper)]",
          )}
        >
          {busy ? (
            <Loader2 size={16} strokeWidth={1.8} className="animate-spin" />
          ) : (
            <ArrowUpRight size={18} strokeWidth={1.8} />
          )}
        </button>
      </form>

      <p className="mt-3 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
        ⌘ / Ctrl + Enter to send
      </p>
    </section>
  );
}
