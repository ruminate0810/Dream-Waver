"use client";

import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "next/navigation";
import { ArrowLeft } from "lucide-react";

import { getClawRun, type ClawRun, type ClawTask } from "@/lib/api";
import {
  AgentSessionProvider,
  useAgentEventStream,
  type AgentEvent,
} from "@/components/chat/transport";
import { ClawChat } from "@/components/claw/ClawChat";
import { ClarificationCard } from "@/components/claw/ClarificationCard";
import { TaskPlanCard } from "@/components/claw/TaskPlanCard";
import { ClawOffice } from "@/components/claw/ClawOffice";
import { StatusChip, type PixelStatus } from "@/components/ui/pixel";

// /claw/[id] is the Claw worker workspace. Left column = process timeline
// (plan checklist + chat); right column = WorkerDesk (live worker stations)
// + the markdown report. All four surfaces share one WebSocket via
// <AgentSessionProvider>, mirroring /code/[id] and /slides/[id].
//
// We poll GET /claw/{id} every 2s while running to detect the status flip
// and seed plan / artifact version on cold load; once finished we stop, and
// a follow-up edit flips status back to running + resumes polling.

export default function ClawPage() {
  const params = useParams<{ id: string }>();
  const search = useSearchParams();
  const jobId = params.id;
  const sessionId = search.get("session") ?? "";
  const [run, setRun] = useState<ClawRun | null>(null);
  const [pollTick, setPollTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const r = await getClawRun(jobId);
        if (cancelled) return;
        setRun(r);
        if (r.status === "running" || r.status === "awaiting_input")
          timer = setTimeout(poll, 2000);
      } catch {
        if (!cancelled) timer = setTimeout(poll, 5000);
      }
    }
    poll();
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [jobId, pollTick]);

  const onPendingEdit = () => {
    setRun((prev) => (prev ? { ...prev, status: "running" } : prev));
    setPollTick((n) => n + 1);
  };

  return (
    <main className="dot-grid relative min-h-[100dvh] bg-paper text-ink antialiased">
      <header className="dw-deck-header relative z-20 border-b-2 border-ink bg-paper/85 backdrop-blur-[2px]">
        <div className="mx-auto flex max-w-[1480px] items-center justify-between px-6 py-3.5 md:px-10">
          <a
            href="/"
            className="group inline-flex items-center gap-2 font-mono text-[11px] font-semibold tracking-wide text-ink-2 transition-colors hover:text-ink"
          >
            <span className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm transition-transform group-hover:-translate-x-0.5">
              <ArrowLeft size={11} strokeWidth={2} />
            </span>
            <span>DREAM-WAVER / INDEX</span>
          </a>
          <div className="flex items-center gap-4">
            <span className="hidden font-pixel text-[0.55rem] tracking-wide text-muted md:inline">
              ~/claw/{jobId.slice(0, 6)}
            </span>
            <ClawStatusChip status={run?.status} />
          </div>
        </div>
      </header>

      <div className="relative z-10 mx-auto max-w-[1480px] px-4 md:px-10">
        {run ? (
          <Workspace run={run} sessionId={sessionId} onPendingEdit={onPendingEdit} />
        ) : (
          <div className="dw-deck-loading flex items-center gap-2 px-2 py-20 font-pixel text-[0.55rem] tracking-wide text-muted">
            <span className="inline-block h-2 w-2 animate-pixpulse rounded-full bg-accent" />
            Loading session…
          </div>
        )}
      </div>
    </main>
  );
}

function ClawStatusChip({ status }: { status?: ClawRun["status"] }) {
  const map: Record<string, { s: PixelStatus; label: string }> = {
    running: { s: "working", label: "工作中" },
    awaiting_input: { s: "queued", label: "待补充" },
    finished: { s: "done", label: "已完成" },
    error: { s: "error", label: "出错" },
  };
  const m = status ? map[status] : undefined;
  return (
    <StatusChip status={m?.s ?? "idle"} className="hidden md:inline-flex">
      {m?.label ?? "Loading"}
    </StatusChip>
  );
}

function Workspace({
  run,
  sessionId,
  onPendingEdit,
}: {
  run: ClawRun;
  sessionId: string;
  onPendingEdit: () => void;
}) {
  return (
    <AgentSessionProvider sessionId={sessionId || run.session_id}>
      <div className="grid grid-cols-1 gap-x-10 py-4 lg:grid-cols-[minmax(360px,38fr)_minmax(0,62fr)] xl:gap-x-12">
        {/* Left: plan checklist + process chat */}
        <section className="dw-deck-chat relative lg:border-r-2 lg:border-ink lg:pr-2 xl:pr-4">
          <div className="lg:sticky lg:top-[57px] lg:flex lg:h-[calc(100dvh-73px)] lg:flex-col">
            {run.status === "awaiting_input" && (run.clarification_questions?.length ?? 0) > 0 && (
              <ClarificationCard
                jobId={run.job_id}
                questions={run.clarification_questions ?? []}
                onResume={onPendingEdit}
              />
            )}
            <PlanRail run={run} />
            <div className="min-h-0 flex-1">
              <ClawChat run={run} onPendingEdit={onPendingEdit} />
            </div>
          </div>
        </section>

        {/* Right: the office — workers live in it, the work package opens as
            a window over it */}
        <section className="dw-deck-preview relative lg:pl-2">
          <div className="lg:sticky lg:top-[57px] lg:h-[calc(100dvh-73px)] lg:py-4 lg:pl-4">
            <div className="h-[460px] lg:h-full">
              <ClawOffice run={run} />
            </div>
          </div>
        </section>
      </div>
    </AgentSessionProvider>
  );
}

// PlanRail owns the live plan checklist. It seeds from the polled run.plan
// (authoritative on cold load) and folds claw.plan / claw.task.update events
// so the checkboxes move in lockstep with the real tool calls.
function PlanRail({ run }: { run: ClawRun }) {
  const stream = useAgentEventStream();
  const [plan, setPlan] = useState<ClawTask[]>(run.plan ?? []);

  // Adopt a polled plan when we don't have one yet (e.g. cold load of a
  // finished run with no replay) — live events otherwise drive it.
  useEffect(() => {
    if (run.plan && run.plan.length > 0) {
      setPlan((cur) => (cur.length === 0 ? run.plan! : cur));
    }
  }, [run.plan]);

  useEffect(() => {
    const handle = (ev: AgentEvent) => {
      if (ev.kind === "claw.plan") {
        const titles = ev.data.task_titles ?? [];
        setPlan(titles.map((t) => ({ title: t, status: "pending" as const })));
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
