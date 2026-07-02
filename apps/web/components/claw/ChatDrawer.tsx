"use client";

import { useEffect, useState } from "react";
import { MessageSquareText, ChevronLeft } from "lucide-react";
import clsx from "clsx";

import type { ClawRun } from "@/lib/api";
import { ClawChat } from "./ClawChat";
import { OFFICE_CONFIG } from "./officeSim";

// ChatDrawer makes the office full-page: the plan checklist + clarification
// card + process chat live in a summonable left drawer that slides OVER the
// office. A slim edge tab toggles it; it force-opens when the run pauses for
// clarification (the user must see the questions); open state persists.
export function ChatDrawer({
  run,
  onPendingEdit,
  registerAsk,
}: {
  run: ClawRun;
  onPendingEdit: () => void;
  /** Receives a dispatcher that sends a message AND slides the drawer open. */
  registerAsk?: (fn: (text: string) => void) => void;
}) {
  const [open, setOpen] = useState(false);
  const [booted, setBooted] = useState(false);

  useEffect(() => {
    try {
      setOpen(localStorage.getItem(OFFICE_CONFIG.storage.chat) === "1");
    } catch {
      /* default closed */
    }
    setBooted(true);
  }, []);

  const toggle = (next: boolean) => {
    setOpen(next);
    try {
      localStorage.setItem(OFFICE_CONFIG.storage.chat, next ? "1" : "0");
    } catch {
      /* no persistence */
    }
  };

  // The clarification gate needs eyes on it — force the drawer open.
  const mustShow = run.status === "awaiting_input" || run.status === "error";
  const shown = open || mustShow;

  return (
    <>
      {/* edge tab */}
      <button
        type="button"
        onClick={() => toggle(!shown)}
        className={clsx(
          "absolute left-0 top-1/2 z-[80] flex -translate-y-1/2 flex-col items-center gap-1 rounded-r-pixel border-2 border-l-0 border-ink bg-surface px-1 py-2.5 shadow-pixel-sm transition-transform hover:translate-x-[1px]",
          shown && "opacity-0 pointer-events-none",
        )}
        aria-label="打开对话"
      >
        <MessageSquareText size={13} strokeWidth={2} className="text-accent" />
        <span className="font-pixel text-[0.5rem] tracking-wide text-ink-2 [writing-mode:vertical-rl]">对话</span>
        {run.status === "running" && <span className="h-[6px] w-[6px] animate-pixpulse rounded-full bg-accent" />}
      </button>

      {/* drawer */}
      <div
        className={clsx(
          "absolute inset-y-2 left-2 z-[85] flex w-[min(420px,92vw)] flex-col overflow-hidden rounded-pixel border-2 border-ink bg-paper shadow-pixel transition-transform duration-300",
          booted ? (shown ? "translate-x-0" : "translate-x-[-110%]") : "translate-x-[-110%]",
        )}
      >
        <div className="flex flex-none items-center justify-between border-b-2 border-ink bg-surface-2 px-3 py-2">
          <span className="font-pixel text-[0.58rem] tracking-wide text-accent">✦ 对话 · 过程</span>
          <button
            type="button"
            onClick={() => toggle(false)}
            disabled={mustShow}
            className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface text-ink-2 shadow-pixel-sm transition-transform hover:-translate-y-0.5 hover:text-ink disabled:opacity-40"
            aria-label="收起对话"
          >
            <ChevronLeft size={12} strokeWidth={2.4} />
          </button>
        </div>
        <div className="flex min-h-0 flex-1 flex-col px-3">
          {/* clarification is now asked conversationally inside ClawChat;
              the plan checklist floats at the office top-right (OfficePlan) */}
          <div className="min-h-0 flex-1">
            <ClawChat
              run={run}
              onPendingEdit={onPendingEdit}
              registerSend={
                registerAsk
                  ? (fn) =>
                      registerAsk((text) => {
                        fn(text);
                        toggle(true); // the user should see their spatial ask land as a turn
                      })
                  : undefined
              }
            />
          </div>
        </div>
      </div>
    </>
  );
}
