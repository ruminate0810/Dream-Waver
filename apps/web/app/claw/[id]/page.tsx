"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useSearchParams } from "next/navigation";
import { ArrowLeft } from "lucide-react";

import { getClawRun, getSessionLog, type ClawRun, type SessionLogEvent } from "@/lib/api";
import { AgentSessionProvider, StaticStreamProvider } from "@/components/chat/transport";
import { ClawOffice } from "@/components/claw/ClawOffice";
import { ChecklistView, LogView } from "@/components/claw/AltViews";
import { ChatDrawer } from "@/components/claw/ChatDrawer";
import { ReplayBar, useReplay } from "@/components/claw/Replay";
import { StatusChip, type PixelStatus } from "@/components/ui/pixel";

// v22b 三视图 — office / checklist / log render the SAME stream+run;
// information-equivalent, representation is the only difference.
type ClawView = "office" | "checklist" | "log";
const VIEW_LABEL: Record<ClawView, string> = { office: "办公室", checklist: "清单", log: "日志" };

// /claw/[id] — the FULL-PAGE office. The whole viewport below the header is
// the Sims-style room (ClawOffice); the plan/clarification/chat live in a
// summonable left drawer (ChatDrawer); the work package opens as an OS
// window over the office. We poll GET /claw/{id} every 2s while running (or
// paused for clarification) to seed plan/artifacts and detect status flips.

export default function ClawPage() {
  const params = useParams<{ id: string }>();
  const search = useSearchParams();
  const jobId = params.id;
  const sessionId = search.get("session") ?? "";
  const [run, setRun] = useState<ClawRun | null>(null);
  const [pollTick, setPollTick] = useState(0);
  // 空间可操作化 — the office dispatches follow-up messages (拖图派活/敲桌
  // 追问) through the chat's send(); the drawer registers it here.
  const askRef = useRef<((text: string) => void) | null>(null);
  const [view, setView] = useState<ClawView>("office");
  useEffect(() => {
    try {
      const v = localStorage.getItem("claw-view-v1");
      if (v === "checklist" || v === "log") setView(v);
    } catch {
      /* default office */
    }
  }, []);
  const switchView = (v: ClawView) => {
    setView(v);
    try {
      localStorage.setItem("claw-view-v1", v);
    } catch {
      /* no persistence */
    }
  };

  // v24 时光机 — replay a finished run's persisted journal through the same
  // stream contract; null = live mode.
  const [replayEvents, setReplayEvents] = useState<SessionLogEvent[] | null>(null);
  const enterReplay = async () => {
    if (!run) return;
    try {
      const events = await getSessionLog(sessionId || run.session_id);
      setReplayEvents(events);
    } catch {
      setReplayEvents([]); // endpoint down → empty timeline, exit is one click
    }
  };

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const r = await getClawRun(jobId);
        if (cancelled) return;
        setRun(r);
        if (r.status === "running" || r.status === "awaiting_input") timer = setTimeout(poll, 2000);
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
    <main className="dot-grid relative flex h-[100dvh] flex-col overflow-hidden bg-paper text-ink antialiased">
      <header className="dw-deck-header relative z-20 flex-none border-b-2 border-ink bg-paper/85 backdrop-blur-[2px]">
        <div className="mx-auto flex max-w-[1800px] items-center justify-between px-6 py-3 md:px-8">
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
            <span className="hidden max-w-[420px] truncate font-mono text-[11px] text-ink-2 md:inline">
              {run?.title || `~/claw/${jobId.slice(0, 6)}`}
            </span>
            {/* 三视图切换 — 同一事件流,三种表征 */}
            <div className="flex overflow-hidden rounded-pixel border-2 border-ink shadow-pixel-sm">
              {(["office", "checklist", "log"] as ClawView[]).map((v) => (
                <button
                  key={v}
                  type="button"
                  onClick={() => switchView(v)}
                  className={
                    "px-2 py-1 font-mono text-[10px] font-bold transition-colors " +
                    (view === v ? "bg-accent text-white" : "bg-surface text-ink-2 hover:text-ink")
                  }
                >
                  {VIEW_LABEL[v]}
                </button>
              ))}
            </div>
            {/* v24 时光机 — finished runs can be re-enacted from the journal */}
            {run?.status === "finished" &&
              (replayEvents === null ? (
                <button
                  type="button"
                  onClick={() => void enterReplay()}
                  className="rounded-pixel border-2 border-ink bg-surface px-2 py-1 font-mono text-[10px] font-bold text-ink-2 shadow-pixel-sm hover:text-ink"
                >
                  ⏪ 回放
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => setReplayEvents(null)}
                  className="rounded-pixel border-2 border-ink bg-accent px-2 py-1 font-mono text-[10px] font-bold text-white shadow-pixel-sm"
                >
                  退出回放
                </button>
              ))}
            <ClawStatusChip status={run?.status} />
          </div>
        </div>
      </header>

      <div className="relative z-10 min-h-0 flex-1 p-2 md:p-3">
        {run && replayEvents !== null ? (
          <ReplayStage run={run} events={replayEvents} />
        ) : run ? (
          <AgentSessionProvider sessionId={sessionId || run.session_id}>
            <div className="relative h-full">
              {view === "office" ? (
                <ClawOffice run={run} onAsk={(text) => askRef.current?.(text)} />
              ) : view === "checklist" ? (
                <ChecklistView run={run} />
              ) : (
                <LogView run={run} />
              )}
              <ChatDrawer
                run={run}
                onPendingEdit={onPendingEdit}
                registerAsk={(fn) => {
                  askRef.current = fn;
                }}
              />
            </div>
          </AgentSessionProvider>
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

// ReplayStage (v24) — the office re-enacted from the persisted journal. The
// run copy starts "running" with an empty plan so the events themselves
// rebuild the world; the office can't tell this from a live stream.
function ReplayStage({ run, events }: { run: ClawRun; events: SessionLogEvent[] }) {
  const replay = useReplay(events);
  const replayRun: ClawRun = {
    ...run,
    status: "running",
    plan: [],
    artifact_version: 0,
    figures: [],
    videos: [],
    deck: undefined,
  };
  // auto-start once on entry
  const started = useRef(false);
  useEffect(() => {
    if (!started.current && events.length > 0) {
      started.current = true;
      replay.play();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [events.length]);

  return (
    <StaticStreamProvider stream={replay.stream}>
      <div className="relative h-full">
        <ClawOffice key={replay.generation} run={replayRun} />
        <ReplayBar replay={replay} />
        {events.length === 0 && (
          <div className="absolute left-1/2 top-1/3 -translate-x-1/2 rounded-pixel border-2 border-ink bg-surface px-4 py-3 font-mono text-[12px] text-ink-2 shadow-pixel">
            这单没有留下事件日志(匿名运行或无持久化后端)。
          </div>
        )}
      </div>
    </StaticStreamProvider>
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
