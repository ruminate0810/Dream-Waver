"use client";

import { Loader2 } from "lucide-react";

// BusyHint fills the visual gap between a user action — a chat message
// or a gate submission ("保存并继续") — and the first real backend
// progress event. Without it the active turn looks frozen for the
// 1-3s the resume goroutine + first agent LLM round-trip takes.
//
// Reducer side: session.ts sets Turn.busyHint on `user_message` and
// `gate_submitted` actions, and clears it the moment any progress
// event lands (tool.start / llm.thought / llm.token / slides.outline
// / slides.compose.start / wizard.step / agent.finish / agent.error).
// So this component just renders if busyHint is set; it doesn't own
// any timing logic.
//
// Visual language: vermillion left rule + monospace caps kicker +
// italic serif zh phrase + spinning Loader — same chrome as
// EditTurnTrace's userMessage blockquote and the existing
// "Drafting each page…" italics in SlidesOpening, so the surface
// reads as one continuous editorial voice.

export function BusyHint({ kind }: { kind: "preparing" | "editing" }) {
  const kicker = kind === "editing" ? "Editing" : "Preparing";
  const zh = kind === "editing" ? "正在编辑当前页面…" : "正在准备工具…";

  return (
    <div
      role="status"
      aria-live="polite"
      className="animate-phase-in mt-5 flex items-center gap-3 border-l-[2px] border-[color:var(--vermillion)] bg-[color:var(--vermillion)]/[0.05] py-2.5 pl-3.5 pr-5"
    >
      <Loader2
        size={13}
        strokeWidth={1.9}
        className="shrink-0 animate-spin text-[color:var(--vermillion)]"
      />
      <span className="font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--vermillion)]">
        {kicker}
      </span>
      <span className="font-display text-[13px] italic leading-snug text-[color:var(--ink)]/85">
        {zh}
      </span>
    </div>
  );
}
