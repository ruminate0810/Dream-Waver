"use client";

import { useEffect, useState } from "react";
import { MessageSquareText, ChevronLeft } from "lucide-react";
import clsx from "clsx";

import type { ClawRun, ClawTask } from "@/lib/api";
import { useAgentEventStream, type AgentEvent } from "@/components/chat/transport";
import { ClawChat } from "./ClawChat";
import { TaskPlanCard } from "./TaskPlanCard";
import { ClarificationCard } from "./ClarificationCard";
import { OFFICE_CONFIG } from "./officeSim";

// ChatDrawer makes the office full-page: the plan checklist + clarification
// card + process chat live in a summonable left drawer that slides OVER the
// office. A slim edge tab toggles it; it force-opens when the run pauses for
// clarification (the user must see the questions); open state persists.
export function ChatDrawer({ run, onPendingEdit }: { run: ClawRun; onPendingEdit: () => void }) {
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
          {run.status === "awaiting_input" && (run.clarification_questions?.length ?? 0) > 0 && (
            <ClarificationCard jobId={run.job_id} questions={run.clarification_questions ?? []} onResume={onPendingEdit} />
          )}
          <PlanRail run={run} />
          <div className="min-h-0 flex-1">
            <ClawChat run={run} onPendingEdit={onPendingEdit} />
          </div>
        </div>
      </div>
    </>
  );
}

// PlanRail owns the live plan checklist: seeded from the polled run.plan,
// driven live by claw.plan / claw.task.update events.
function PlanRail({ run }: { run: ClawRun }) {
  const stream = useAgentEventStream();
  const [plan, setPlan] = useState<ClawTask[]>(run.plan ?? []);

  useEffect(() => {
    if (run.plan && run.plan.length > 0) {
      setPlan((cur) => (cur.length === 0 ? run.plan! : cur));
    }
  }, [run.plan]);

  useEffect(() => {
    const handle = (ev: AgentEvent) => {
      if (ev.kind === "claw.plan") {
        const titles = ev.data.task_titles ?? [];
        const roles = ev.data.task_roles ?? [];
        setPlan(titles.map((t, i) => ({ title: t, role: roles[i], status: "pending" as const })));
      } else if (ev.kind === "claw.task.update") {
        const idx = ev.data.task_index ?? 0;
        const status = (ev.data.task_status ?? "pending") as ClawTask["status"];
        setPlan((prev) => {
          if (idx < 1 || idx > prev.length) return prev;
          const next = prev.slice();
          next[idx - 1] = { ...next[idx - 1], status };
          return next;
        });
      }
    };
    return stream.subscribe(handle);
  }, [stream]);

  if (plan.length === 0) return null;
  return (
    <div className="pb-3 pt-2">
      <TaskPlanCard tasks={plan} />
    </div>
  );
}
