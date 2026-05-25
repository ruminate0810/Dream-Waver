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
    <main className="min-h-screen bg-white text-zinc-900">
      <header className="sticky top-0 z-20 border-b border-zinc-100 bg-white/90 backdrop-blur">
        <div className="mx-auto flex max-w-[1480px] items-baseline justify-between px-6 py-3">
          <a
            href="/"
            className="inline-flex items-baseline gap-2 text-xs text-zinc-500 hover:text-zinc-800"
          >
            <ArrowLeft size={12} /> Dream-Waver
          </a>
          <div className="flex items-baseline gap-4">
            <span className="hidden font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-400 md:inline">
              Video · {snapshot?.title ?? "Loading"}
            </span>
            <a
              href="/video/new"
              className="font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500 hover:text-zinc-800"
            >
              + new run
            </a>
          </div>
        </div>
      </header>

      <div className="mx-auto grid max-w-[1480px] grid-cols-1 gap-x-8 px-4 md:px-8 lg:grid-cols-[minmax(320px,30fr)_minmax(0,70fr)]">
        {/* Left — activity log (acts as the "chat" surface). */}
        <section className="border-r border-zinc-100 lg:sticky lg:top-[49px] lg:h-[calc(100vh-49px)]">
          <VideoEventLog timeline={snapshot} />
        </section>

        {/* Right — click-to-regen timeline. */}
        <section className="py-6 lg:pl-6">
          <VideoTimeline runId={runId} onTimeline={setSnapshot} />
        </section>
      </div>
    </main>
  );
}
