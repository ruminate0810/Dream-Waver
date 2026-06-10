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
// Visual language matches the pixel pages: ink-framed surface field with
// a hard pixel shadow, mono caption, violet accent send button.

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
    <section className="mt-20 border-t border-line-2 pt-10">
      <p className="font-mono text-[10px] font-semibold uppercase tracking-wide text-ink-2">
        Follow-up · 继续修改
      </p>

      <form
        onSubmit={send}
        className="mt-4 flex items-end gap-3 rounded-pixel border-2 border-ink bg-surface px-4 py-3 shadow-pixel transition-shadow focus-within:shadow-pixel-lg"
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
          className="flex-1 resize-none bg-transparent font-mono text-[15px] leading-relaxed text-ink placeholder:text-muted focus:outline-none disabled:opacity-50"
        />

        <button
          type="submit"
          disabled={busy || !text.trim()}
          aria-label="Send follow-up"
          className={clsx(
            "inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-pixel border-2 transition-transform",
            busy || !text.trim()
              ? "cursor-not-allowed border-line-2 bg-surface-2 text-line-2"
              : "border-ink bg-accent text-white shadow-pixel-sm hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none",
          )}
        >
          {busy ? (
            <Loader2 size={16} strokeWidth={1.8} className="animate-spin text-accent" />
          ) : (
            <ArrowUpRight size={18} strokeWidth={1.8} />
          )}
        </button>
      </form>

      <p className="mt-3 font-mono text-[10px] tracking-wide text-muted">
        ⌘ / Ctrl + Enter to send
      </p>
    </section>
  );
}
