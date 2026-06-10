"use client";

import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "next/navigation";
import { ArrowLeft } from "lucide-react";

import { getGameJob, type GameJob } from "@/lib/api";
import { AgentSessionProvider } from "@/components/chat/transport";
import { GameChat } from "@/components/game-preview/GameChat";
import { GameFrame } from "@/components/game-preview/GameFrame";
import { StatusChip, type PixelStatus } from "@/components/ui/pixel";

// /code/[id] is the game workspace — left column = chat with the agent,
// right column = live iframe preview of the generated game. The two
// columns share a single WebSocket via <AgentSessionProvider>, mirroring
// how /slides/[id] is wired.
//
// We poll GET /games/{id} every 2s while running so we can detect the
// status flip to "finished" and surface the play_url + bytes. Once
// finished we stop polling; follow-up edits will flip status back to
// "running" via the POST messages endpoint and resume polling.
//
// Pixel re-skin: warm paper + dot-grid, hard pixel borders, monospace.
// Entrances reuse the CSS-driven .dw-deck-* classes from globals.css so
// nothing strands at opacity:0 under React Strict Mode.

export default function GamePage() {
  const params = useParams<{ id: string }>();
  const search = useSearchParams();
  const jobId = params.id;
  const sessionId = search.get("session") ?? "";
  const [job, setJob] = useState<GameJob | null>(null);
  // `pollTick` is bumped whenever we need to restart the polling loop —
  // the initial mount, plus every time a follow-up edit gets POSTed
  // (since the loop self-terminates once status flips to "finished").
  const [pollTick, setPollTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const j = await getGameJob(jobId);
        if (cancelled) return;
        setJob(j);
        if (j.status === "running") {
          timer = setTimeout(poll, 2000);
        }
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

  // Called from GameChat after POST /messages succeeds. We flip the local
  // job's status to "running" immediately (so the UI feels responsive)
  // and bump pollTick to restart the polling loop.
  const onPendingEdit = () => {
    setJob((prev) => (prev ? { ...prev, status: "running" } : prev));
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
              ~/code/{jobId.slice(0, 6)}
            </span>
            <GameStatusChip status={job?.status} />
          </div>
        </div>
      </header>

      <div className="relative z-10 mx-auto max-w-[1480px] px-4 md:px-10">
        {job ? (
          <Workspace job={job} sessionId={sessionId} onPendingEdit={onPendingEdit} />
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

// GameStatusChip maps job.status → a pixel StatusChip in the header.
// Labels keep the prior header copy (Composing / Playable / Halted).
function GameStatusChip({ status }: { status?: GameJob["status"] }) {
  const map: Record<string, { s: PixelStatus; label: string }> = {
    running: { s: "working", label: "Composing" },
    finished: { s: "done", label: "Playable" },
    error: { s: "error", label: "Halted" },
  };
  const m = status ? map[status] : undefined;
  return (
    <StatusChip status={m?.s ?? "idle"} className="hidden md:inline-flex">
      {m?.label ?? "Loading"}
    </StatusChip>
  );
}

function Workspace({
  job,
  sessionId,
  onPendingEdit,
}: {
  job: GameJob;
  sessionId: string;
  onPendingEdit: () => void;
}) {
  return (
    <AgentSessionProvider sessionId={sessionId || job.session_id}>
      <div className="grid grid-cols-1 gap-x-10 lg:grid-cols-[minmax(360px,32fr)_minmax(0,68fr)] xl:gap-x-14">
        {/* Left: chat surface */}
        <section className="dw-deck-chat relative lg:border-r-2 lg:border-ink lg:pr-2 xl:pr-4">
          <div className="lg:sticky lg:top-[57px] lg:h-[calc(100dvh-57px)]">
            <GameChat job={job} onPendingEdit={onPendingEdit} />
          </div>
        </section>

        {/* Right: live iframe preview */}
        <section className="dw-deck-preview relative lg:pl-2">
          <div className="lg:sticky lg:top-[57px] lg:h-[calc(100dvh-57px)] lg:pb-6 lg:pl-4 lg:pr-3 lg:pt-4">
            <GameFrame job={job} />
          </div>
        </section>
      </div>
    </AgentSessionProvider>
  );
}
