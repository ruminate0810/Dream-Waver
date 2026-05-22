"use client";

import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "next/navigation";
import { ArrowLeft } from "lucide-react";

import { getSlideJob, type SlideJob } from "@/lib/api";
import { Chat } from "@/components/chat/Chat";
import { LivePreviewStack } from "@/components/slides-preview/LivePreviewStack";

// The slides workspace is a two-column manuscript:
//   left  — chat-style generation timeline (the editor's running notes)
//   right — live HTML preview stack (the typeset proof)
//
// On lg+ screens the two columns scroll independently inside a single
// 100dvh frame; the publication header stays pinned along the top. On
// smaller viewports we collapse to one column with the preview first
// (it's the artifact) followed by the chat (the metadata) — but the
// app's primary surface is desktop and the responsive case is a
// graceful fallback rather than a designed-for state.

export default function SlideJobPage() {
  const params = useParams<{ id: string }>();
  const search = useSearchParams();
  const jobId = params.id;
  const sessionId = search.get("session") ?? "";
  const [job, setJob] = useState<SlideJob | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const j = await getSlideJob(jobId);
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
  }, [jobId]);

  return (
    <main className="relative min-h-[100dvh] bg-[color:var(--paper)] text-[color:var(--ink)] antialiased">
      {/* Paper grain — fixed, pointer-events-none so it never re-paints
          during scroll. Mix-blend-multiply keeps the paper warm rather
          than washing it grey. */}
      <div
        aria-hidden
        className="pointer-events-none fixed inset-0 z-0 opacity-[0.05] mix-blend-multiply"
        style={{
          backgroundImage:
            "url(\"data:image/svg+xml;utf8,%3Csvg viewBox='0 0 240 240' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix values='0 0 0 0 0.1 0 0 0 0 0.07 0 0 0 0 0.06 0 0 0 0 0.06 0 0 0 0.8 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
          backgroundSize: "240px 240px",
        }}
      />

      {/* Publication header — kept hairline thin so it doesn't fight the
          two columns underneath. */}
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
              Vol. 01 · Composition Log
            </span>
            <span className="hidden font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] md:inline">
              {job?.status === "running"
                ? "In Press"
                : job?.status === "finished"
                  ? "Set in Type"
                  : job?.status === "error"
                    ? "Withdrawn"
                    : "Loading"}
            </span>
          </div>
        </div>
      </header>

      {/* Body */}
      <div className="relative z-10 mx-auto max-w-[1480px] px-4 md:px-10">
        {job ? (
          <Workspace job={job} sessionId={sessionId} />
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

// Workspace owns the two-column split itself. Kept as a leaf component
// so the suspense/loading states above stay clean, and so the layout
// math doesn't bleed into the data-fetching code.
function Workspace({ job, sessionId }: { job: SlideJob; sessionId: string }) {
  return (
    // CSS grid gives us reliable structure — explicit fr units rather
    // than flexbox math. Left column is narrower (the editor's notes);
    // right column gets the proof. The hairline centre divider is a
    // single rule, not a card border on either side.
    <div className="grid grid-cols-1 gap-x-10 lg:grid-cols-[minmax(440px,38fr)_minmax(0,62fr)] xl:gap-x-14">
      {/* ── Left: chat / generation timeline ─────────────────────────── */}
      <section className="relative lg:border-r lg:border-[color:var(--rule)] lg:pr-10 xl:pr-14">
        <div className="lg:sticky lg:top-[57px] lg:max-h-[calc(100dvh-57px)] lg:overflow-y-auto lg:pb-10 lg:pt-2 lg:[scrollbar-width:thin]">
          <Chat job={job} sessionId={sessionId} compact />
        </div>
      </section>

      {/* ── Right: live HTML preview stack ───────────────────────────── */}
      <section className="relative lg:pl-2">
        <div className="lg:sticky lg:top-[57px] lg:max-h-[calc(100dvh-57px)] lg:overflow-y-auto lg:pl-4 lg:pr-2 lg:pt-2 lg:[scrollbar-width:thin]">
          <LivePreviewStack job={job} />
        </div>
      </section>
    </div>
  );
}
