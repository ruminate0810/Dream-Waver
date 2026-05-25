"use client";

import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";

import { useAgentEventStream } from "./transport";

// ConnectionToast — surfaces the WebSocket connection status as a
// transient bottom-center toast so the user knows the agent stream
// is alive (and finds out fast when it's not).
//
// Design discipline: stays HIDDEN during normal operation. The
// transport hook already handles reconnect with exponential backoff;
// this is purely an awareness layer for the user.
//
// Grace period: status flickers to "connecting" briefly on initial
// mount and on every reconnect. A 600ms delay before showing
// prevents nuisance flashes during healthy network conditions, while
// still surfacing real outages within a second.

const SHOW_DELAY_MS = 600;

export function ConnectionToast() {
  const { status } = useAgentEventStream();
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (status === "open") {
      setVisible(false);
      return;
    }
    // status is "connecting" or "closed" — schedule the toast unless
    // we recover within the grace window.
    const t = setTimeout(() => setVisible(true), SHOW_DELAY_MS);
    return () => clearTimeout(t);
  }, [status]);

  if (!visible) return null;

  return (
    <div
      role="status"
      aria-live="polite"
      className="pointer-events-none fixed bottom-6 left-1/2 z-50 -translate-x-1/2 transform"
      style={{
        // Tiny fade-in transition. Pure CSS so SSR / hydrate is clean.
        animation: "dw-conn-fade 200ms ease-out forwards",
      }}
    >
      <style jsx>{`
        @keyframes dw-conn-fade {
          from { opacity: 0; transform: translate(-50%, 6px); }
          to   { opacity: 1; transform: translate(-50%, 0); }
        }
      `}</style>
      <div className="pointer-events-auto inline-flex items-center gap-2.5 border border-[color:var(--ink)]/15 bg-[#FBF9F2] px-4 py-2.5 font-mono-jb text-[11px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] shadow-[0_18px_32px_-20px_rgba(50,40,32,0.32)]">
        <Loader2 size={12} strokeWidth={1.8} className="animate-spin text-[color:var(--vermillion)]" />
        <span>实时通道重连中…</span>
      </div>
    </div>
  );
}
