"use client";

import clsx from "clsx";
import { Check, Minus } from "lucide-react";

import { WindowCard } from "@/components/ui/pixel";
import type { ClawTask } from "@/lib/api";

// TaskPlanCard renders the Claw agent's sub-task checklist. State is fully
// prop-driven — the page owns the plan and folds claw.plan /
// claw.task.update events into it, so the checkbox state is driven solely
// by real tool events (never inferred). A square pixel checkbox per row:
// pending = empty, doing = violet pulse, done = grass check, skipped =
// struck through.

export function TaskPlanCard({ tasks }: { tasks: ClawTask[] }) {
  if (!tasks.length) return null;
  const done = tasks.filter((t) => t.status === "done" || t.status === "skipped").length;

  return (
    <WindowCard
      title="✦ CLAW PLAN"
      right={
        <span className="font-pixel text-[0.55rem] tracking-wide text-muted tabular-nums">
          {done}/{tasks.length}
        </span>
      }
      bodyClassName="p-0"
    >
      <ol className="divide-y-2 divide-line">
        {tasks.map((t, i) => (
          <li key={i} className="flex items-start gap-3 px-4 py-2.5">
            <Checkbox status={t.status} />
            <span
              className={clsx(
                "font-mono text-[13px] leading-snug",
                t.status === "done" && "text-ink",
                t.status === "doing" && "text-ink font-semibold",
                t.status === "pending" && "text-ink-2",
                t.status === "skipped" && "text-muted line-through",
              )}
            >
              {t.title}
            </span>
          </li>
        ))}
      </ol>
    </WindowCard>
  );
}

function Checkbox({ status }: { status: ClawTask["status"] }) {
  return (
    <span
      className={clsx(
        "mt-0.5 flex h-[18px] w-[18px] flex-none items-center justify-center rounded-[3px] border-2 border-ink",
        status === "done" && "bg-grass text-white",
        status === "doing" && "bg-accent text-white animate-pixpulse",
        status === "pending" && "bg-surface",
        status === "skipped" && "bg-surface-2 text-muted",
      )}
    >
      {status === "done" && <Check size={11} strokeWidth={3} />}
      {status === "skipped" && <Minus size={11} strokeWidth={3} />}
      {status === "doing" && <span className="h-[6px] w-[6px] rounded-full bg-white" />}
    </span>
  );
}
