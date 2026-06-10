"use client";

import { useEffect, useState } from "react";

// Shared tool-call progress affordances, used by both the default chat
// thread (ChatThread.ToolRow) and the ToolStrip cards (slides edit traces /
// opening phase / games chat) so every running tool reads the same way.

// RunningClock — live elapsed seconds since the row first rendered as
// running (≈ tool.start). Makes a slow tool read as "progressing", not
// frozen. Pass `className` to fit the host's type scale.
export function RunningClock({ className }: { className?: string }) {
  const [secs, setSecs] = useState(0);
  useEffect(() => {
    const start = Date.now();
    const id = setInterval(() => setSecs(Math.floor((Date.now() - start) / 1000)), 500);
    return () => clearInterval(id);
  }, []);
  return (
    <span className={className ?? "font-pixel text-[0.5rem] tabular-nums text-accent"} aria-hidden>
      {secs}s
    </span>
  );
}

// ToolProgressBar — an indeterminate accent segment sweeping across a thin
// ink-topped track. Honest "in-flight" feedback when there's no real %.
export function ToolProgressBar({ label }: { label?: string }) {
  return (
    <div
      role="progressbar"
      aria-label={label}
      className="relative h-[3px] w-full overflow-hidden border-t-2 border-ink bg-line"
    >
      <span className="dw-progress-bar" />
    </div>
  );
}

// fmtDuration renders a finished tool call's durationMs compactly:
// "312ms" below a second, "4.7s" above.
export function fmtDuration(ms: number): string {
  return ms < 1000 ? `${Math.round(ms)}ms` : `${(ms / 1000).toFixed(1)}s`;
}
