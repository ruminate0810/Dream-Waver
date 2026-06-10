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
// Visual language: ink-framed pixel chip on the violet-soft accent
// wash + pixel-face kicker + mono zh phrase + spinning Loader — same
// chrome as the StatusChip "working" state, so the surface reads as
// one continuous pixel voice.

export function BusyHint({ kind }: { kind: "preparing" | "editing" }) {
  const kicker = kind === "editing" ? "Editing" : "Preparing";
  const zh = kind === "editing" ? "正在编辑当前页面…" : "正在准备工具…";

  return (
    <div
      role="status"
      aria-live="polite"
      className="animate-phase-in mt-5 flex items-center gap-3 rounded-pixel border-2 border-ink bg-accent-soft px-4 py-2.5 shadow-pixel-sm"
    >
      <Loader2
        size={13}
        strokeWidth={1.9}
        className="shrink-0 animate-spin text-accent"
      />
      <span className="font-pixel text-[0.55rem] tracking-wide text-accent">
        {kicker}
      </span>
      <span className="font-mono text-[13px] leading-snug text-ink-2">
        {zh}
      </span>
    </div>
  );
}
