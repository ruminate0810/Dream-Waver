"use client";

import { useState } from "react";
import { useParams } from "next/navigation";
import { ArrowLeft } from "lucide-react";

import { VideoTimeline } from "@/components/video-preview/VideoTimeline";
import { VideoEventLog } from "@/components/video-preview/VideoEventLog";
import type { VideoTimeline as VideoTimelineT } from "@/lib/api";

// Video workspace — same two-column rhythm as /slides/[id] (events on
// the left, artifact on the right) but the artifact here is a DAG
// timeline rather than a slide stack, and the "chat" is a derived
// activity log (Opendream's pipeline isn't conversational).
//
// Pixel re-skin: warm paper + dot-grid, hard pixel borders, monospace.
// The event log is framed as a floating terminal window ("run.log"),
// so the old hairline column divider is gone — the window chrome
// delimits the pane by itself.
//
// State flow:
//   VideoTimeline subscribes to the SSE stream and fires `onTimeline`
//   on each snapshot. We hold the latest snapshot at the page level
//   and pass it to VideoEventLog, which diffs against its previous
//   snapshot to produce log lines. One subscription, two consumers —
//   matches the slides workspace's AgentSessionProvider pattern.

export default function VideoRunPage() {
  const params = useParams<{ id: string }>();
  const runId = params.id;
  const [snapshot, setSnapshot] = useState<VideoTimelineT | null>(null);

  return (
    <main className="dot-grid min-h-screen bg-paper text-ink antialiased">
      {/* Header is 50px tall: h-6 back square (24) + py-3 (24) + border-b-2
          (2). The sticky offsets below must match. */}
      <header className="sticky top-0 z-20 border-b-2 border-ink bg-paper/85 backdrop-blur-[2px]">
        <div className="mx-auto flex max-w-[1480px] items-center justify-between px-6 py-3">
          <a
            href="/"
            className="group inline-flex items-center gap-2 font-mono text-[11px] font-semibold tracking-wide text-ink-2 transition-colors hover:text-ink"
          >
            <span className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm transition-transform group-hover:-translate-x-0.5">
              <ArrowLeft size={11} strokeWidth={2} />
            </span>
            Dream-Waver
          </a>
          <div className="flex items-center gap-4">
            {/* Run title may contain CJK — keep it in JetBrains Mono
                (Silkscreen is ASCII-only). */}
            <span className="hidden font-mono text-[10px] font-semibold uppercase tracking-wide text-muted md:inline">
              Video · {snapshot?.title ?? "Loading"}
            </span>
            <a
              href="/video/new"
              className="font-pixel text-[0.55rem] uppercase tracking-wide text-ink-2 transition-colors hover:text-ink"
            >
              + new run
            </a>
          </div>
        </div>
      </header>

      <div className="mx-auto grid max-w-[1480px] grid-cols-1 gap-x-8 px-4 md:px-8 lg:grid-cols-[minmax(320px,30fr)_minmax(0,70fr)]">
        {/* Left — activity log (acts as the "chat" surface), rendered as
            a terminal window pinned below the header on desktop. */}
        <section className="pt-6 lg:sticky lg:top-[50px] lg:h-[calc(100vh-50px)] lg:py-6">
          <VideoEventLog timeline={snapshot} />
        </section>

        {/* Right — click-to-regen timeline. */}
        <section className="py-6">
          <VideoTimeline runId={runId} onTimeline={setSnapshot} />
        </section>
      </div>
    </main>
  );
}
