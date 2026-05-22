"use client";

import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "next/navigation";
import { ArrowLeft } from "lucide-react";

import { getGameJob, type GameJob } from "@/lib/api";
import { AgentSessionProvider } from "@/components/chat/transport";
import { GameChat } from "@/components/game-preview/GameChat";
import { GameFrame } from "@/components/game-preview/GameFrame";

// /code/[id] is the game workspace — left column = chat with the agent,
// right column = live iframe preview of the generated game. The two
// columns share a single WebSocket via <AgentSessionProvider>, mirroring
// how /slides/[id] is wired.
//
// We poll GET /games/{id} every 2s while running so we can detect the
// status flip to "finished" and surface the play_url + bytes. Once
// finished we stop polling; follow-up edits will flip status back to
// "running" via the POST messages endpoint and resume polling.

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
    <main className="relative min-h-[100dvh] bg-[color:var(--paper)] text-[color:var(--ink)] antialiased">
      <div
        aria-hidden
        className="pointer-events-none fixed inset-0 z-0 opacity-[0.05] mix-blend-multiply"
        style={{
          backgroundImage:
            "url(\"data:image/svg+xml;utf8,%3Csvg viewBox='0 0 240 240' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix values='0 0 0 0 0.1 0 0 0 0 0.07 0 0 0 0 0.06 0 0 0 0 0.06 0 0 0 0.8 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
          backgroundSize: "240px 240px",
        }}
      />

      <header className="relative z-20 border-b border-[color:var(--rule)] bg-[color:var(--paper)]/85 backdrop-blur-[2px]">
        <div className="mx-auto flex max-w-[1480px] items-baseline justify-between px-6 py-4 md:px-10">
          <a
            href="/"
            className="group inline-flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
          >
            <ArrowLeft
              size={11}
              strokeWidth={1.8}
              className="translate-y-[1px] transition-transform group-hover:-translate-x-0.5"
            />
            <span>Dream-Waver / Index</span>
          </a>
          <div className="flex items-baseline gap-6">
            <span className="hidden font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)] md:inline">
              Vol. 01 · Game Studio
            </span>
            <span className="hidden font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] md:inline">
              {job?.status === "running"
                ? "Composing"
                : job?.status === "finished"
                  ? "Playable"
                  : job?.status === "error"
                    ? "Halted"
                    : "Loading"}
            </span>
          </div>
        </div>
      </header>

      <div className="relative z-10 mx-auto max-w-[1480px] px-4 md:px-10">
        {job ? (
          <Workspace job={job} sessionId={sessionId} onPendingEdit={onPendingEdit} />
        ) : (
          <div className="px-2 py-20">
            <p className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
              Loading session…
            </p>
          </div>
        )}
      </div>
    </main>
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
        <section className="relative lg:border-r lg:border-[color:var(--rule)] lg:pr-2 xl:pr-4">
          <div className="lg:sticky lg:top-[57px] lg:h-[calc(100dvh-57px)]">
            <GameChat job={job} onPendingEdit={onPendingEdit} />
          </div>
        </section>

        {/* Right: live iframe preview */}
        <section className="relative lg:pl-2">
          <div className="lg:sticky lg:top-[57px] lg:h-[calc(100dvh-57px)] lg:pl-4 lg:pr-2 lg:pt-4">
            <GameFrame job={job} />
          </div>
        </section>
      </div>
    </AgentSessionProvider>
  );
}
