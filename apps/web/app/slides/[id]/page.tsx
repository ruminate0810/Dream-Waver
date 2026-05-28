"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { gsap } from "gsap";
import { useGSAP } from "@gsap/react";
import { ArrowLeft, FileQuestion, Plus } from "lucide-react";

import { ApiError, getSlideJob, type SlideJob } from "@/lib/api";
import { Chat } from "@/components/chat/Chat";
import { ChatThread } from "@/components/chat/ChatThread";
import { AgentSessionProvider } from "@/components/chat/transport";
import { ConnectionToast } from "@/components/chat/ConnectionToast";
import { LivePreviewStack } from "@/components/slides-preview/LivePreviewStack";
import { DeckPhaseTimeline } from "@/components/slides/DeckPhaseTimeline";
import { forgetDeck, updateDeckTitle } from "@/lib/recentDecks";
import { DUR, EASE, PREFERS_FULL_MOTION } from "@/lib/motion";

gsap.registerPlugin(useGSAP);

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
  // Default UI is the ChatGPT/Claude-style conversation thread — the
  // whole point of Dream-Waver is the agent dialogue. ?ui=log switches
  // to the editorial composition log if the user explicitly asks for it.
  const uiVariant = search.get("ui") === "log" ? "log" : "thread";
  const [job, setJob] = useState<SlideJob | null>(null);
  // Sprint O.4 polish — distinguish "deck no longer exists on server"
  // (terminal: 404, typical after in-memory store restart) from
  // "transient network blip" (re-poll). Without this, the page sits
  // on "Loading session…" forever and the iframes / WS keep firing
  // failed requests in the background.
  const [notFound, setNotFound] = useState(false);
  const mainRef = useRef<HTMLElement | null>(null);

  // Sprint Y2 — header settles in on first paint; the loading-state
  // caption fades. Workspace + DeckNotFound run their own entrances
  // on mount (deferred until poll resolves).
  useGSAP(
    () => {
      const mm = gsap.matchMedia();
      mm.add(PREFERS_FULL_MOTION, () => {
        gsap.from(".dw-deck-header", {
          y: -8,
          opacity: 0,
          duration: DUR.secondary,
          ease: EASE.entrance,
        });
        gsap.from(".dw-deck-loading", {
          opacity: 0,
          duration: DUR.micro,
          delay: 0.3,
        });
      });
      mm.add("(prefers-reduced-motion: reduce)", () => {
        gsap.from(".dw-deck-header, .dw-deck-loading", {
          opacity: 0,
          duration: 0.2,
        });
      });
    },
    { scope: mainRef },
  );

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const j = await getSlideJob(jobId);
        if (cancelled) return;
        setJob(j);
        // Sprint I0.5 — backfill the title into the recent-decks list
        // the moment the planner gives it to us. Idempotent.
        if (j.title) updateDeckTitle(jobId, j.title);
        // Keep polling until the job hits a terminal state. The bug
        // before this was `status === "running"` only — which stops
        // the loop the moment the backend pauses at any HILT gate
        // (awaiting_wizard / awaiting_clarification /
        // awaiting_outline_approval). The user then clicks "保存并
        // 继续", the backend flips status back to "running", but the
        // FE never knows because polling died. If the WebSocket is
        // also dropped (multi-minute Hub timeout), the page is fully
        // stuck on the spinner. Keep polling for all non-terminal
        // statuses; the awaiting_* states tick at 2s like running.
        if (j.status !== "finished" && j.status !== "error") {
          timer = setTimeout(poll, 2000);
        }
      } catch (err) {
        if (cancelled) return;
        // 404 / 410 are terminal — the backend doesn't know this job
        // (404), or the in-memory session that drove it was wiped
        // (410, typically after an orchestrator restart). Stop
        // polling, flip to the dead-link view, and prune the entry
        // from the user's local recent-decks list so the homepage
        // doesn't keep offering it.
        if (err instanceof ApiError && (err.status === 404 || err.status === 410)) {
          forgetDeck(jobId);
          setNotFound(true);
          return;
        }
        // Other errors (network blip, 500) — keep re-polling.
        timer = setTimeout(poll, 5000);
      }
    }
    poll();
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [jobId]);

  return (
    <main
      ref={mainRef}
      className="relative min-h-[100dvh] bg-[color:var(--paper)] text-[color:var(--ink)] antialiased"
    >
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
      <header className="dw-deck-header relative z-20 border-b border-[color:var(--rule)] bg-[color:var(--paper)]/85 backdrop-blur-[2px]">
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

      {/* Sprint U2.3 — page-level sticky banner for job.status=error.
          Without it, the only error indicator was the small Withdrawn
          caption in the header + a buried errorMsg deep in the chat
          thread. Now the user gets one-tap "新建相似 deck" to escape. */}
      {job?.status === "error" && !notFound ? (
        <JobErrorBanner job={job} />
      ) : null}

      {/* Body */}
      <div className="relative z-10 mx-auto max-w-[1480px] px-4 md:px-10">
        {notFound ? (
          <DeckNotFound jobId={jobId} />
        ) : job ? (
          <Workspace
            job={job}
            sessionId={sessionId}
            uiVariant={uiVariant}
            onDeckGone={() => {
              forgetDeck(jobId);
              setNotFound(true);
            }}
          />
        ) : (
          <div className="dw-deck-loading px-2 py-20">
            <p className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
              Loading session…
            </p>
          </div>
        )}
      </div>
    </main>
  );
}

// Sprint U2.3 — sticky banner shown below the header when job.status
// flipped to "error". Persists across scroll so the user always sees
// the failure + an escape route to /slides/new with the original
// topic prefilled.
function JobErrorBanner({ job }: { job: SlideJob }) {
  const topic = (job.input?.topic || job.title || "").slice(0, 240);
  const escapedTopic = encodeURIComponent(topic);
  return (
    <div className="sticky top-0 z-20 border-b border-[color:var(--vermillion)]/40 bg-[color:var(--vermillion)]/[0.06] backdrop-blur-sm">
      <div className="mx-auto flex max-w-[1480px] flex-wrap items-baseline gap-3 px-4 py-2.5 md:px-10">
        <span className="font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--vermillion)]">
          ✗ 本次生成失败
        </span>
        <span className="min-w-0 flex-1 truncate font-display text-[14px] italic text-[color:var(--ink)]">
          {job.error || "agent stopped without producing a deck"}
        </span>
        {topic ? (
          <a
            href={`/slides/new?topic=${escapedTopic}`}
            className="border border-[color:var(--ink)] bg-[color:var(--ink)] px-3 py-1 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--paper)] no-underline transition-colors hover:bg-[color:var(--vermillion)]"
          >
            新建相似 deck
          </a>
        ) : (
          <a
            href="/slides/new"
            className="border border-[color:var(--ink)] bg-[color:var(--ink)] px-3 py-1 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--paper)] no-underline transition-colors hover:bg-[color:var(--vermillion)]"
          >
            返回首页
          </a>
        )}
      </div>
    </div>
  );
}

// Workspace owns the two-column split + the shared event stream.
// <AgentSessionProvider> opens the single WebSocket; both the chat
// (left) and LivePreviewStack (right) subscribe to it. One connection,
// fan-out delivery, no duplication.
//
// uiVariant picks the left-column renderer:
//   "thread" → <ChatThread>  ChatGPT/Claude-style stacked thread (default)
//   "log"    → <Chat>        editorial composition log (escape hatch)
//
// Layout note: the thread surface needs a fixed-height column so the
// composer can stick to the bottom. We give the left section
// `lg:h-[calc(100dvh-57px)]` (header is 57px) and let ChatThread own
// its internal flex layout. The log surface still wants to scroll the
// whole article, so we branch the wrapper accordingly.
function Workspace({
  job,
  sessionId,
  uiVariant,
  onDeckGone,
}: {
  job: SlideJob;
  sessionId: string;
  uiVariant: "log" | "thread";
  /**
   * Called when a resume / edit POST returns 410 or 404 — same
   * terminal-state signal the polling effect uses to flip into
   * DeckNotFound. Threading it through the provider lets the chat
   * dispatchers in session.ts trigger it without prop-drilling.
   */
  onDeckGone: () => void;
}) {
  const wsRef = useRef<HTMLDivElement | null>(null);

  // Sprint Y2 — two-column entrance. Chat slides in from the left
  // edge, preview from the right, so the eye reads the spread opening
  // up rather than two unrelated panels popping. Compound timeline
  // keeps the columns locked to one heartbeat.
  useGSAP(
    () => {
      const mm = gsap.matchMedia();
      mm.add(PREFERS_FULL_MOTION, () => {
        const tl = gsap.timeline({
          defaults: { ease: EASE.entrance, duration: DUR.entrance },
        });
        tl.from(".dw-deck-chat", { x: -20, opacity: 0 })
          .from(".dw-deck-preview", { x: 20, opacity: 0 }, "-=0.5");
      });
      mm.add("(prefers-reduced-motion: reduce)", () => {
        gsap.from(".dw-deck-chat, .dw-deck-preview", {
          opacity: 0,
          duration: 0.2,
        });
      });
    },
    { scope: wsRef },
  );

  return (
    <AgentSessionProvider
      sessionId={sessionId || job.session_id}
      onDeckGone={onDeckGone}
    >
      {/* Sprint I0.6 — surfaces WebSocket disconnection during a session
          so the user knows the live event stream is reconnecting instead
          of silently watching a frozen UI. Auto-dismisses on reconnect. */}
      <ConnectionToast />
      <div
        ref={wsRef}
        className="grid grid-cols-1 gap-x-6 lg:grid-cols-[200px_minmax(420px,36fr)_minmax(0,64fr)] xl:gap-x-10"
      >
        {/* ── Far-left: deck phase timeline (Sprint AF.1) ───────────────
            Vertical 5-step indicator so the user always knows which
            phase the deck is in (提问 / 大纲 / 撰写 / 渲染 / 完成).
            Hidden on mobile — ChatThread's ComposeStrip carries the
            same signal in narrower contexts. */}
        <aside className="dw-deck-timeline hidden lg:block lg:border-r lg:border-[color:var(--rule)]">
          <div className="lg:sticky lg:top-[57px] lg:h-[calc(100dvh-57px)] lg:overflow-y-auto lg:[scrollbar-width:thin]">
            <DeckPhaseTimeline job={job} />
          </div>
        </aside>

        {/* ── Left: chat surface (variant-selected) ─────────────────────
            Sprint U follow-up — the UiToggle used to float over the
            chat content via `lg:absolute`, which read as a floating
            badge disconnected from anything. It now lives in a thin
            sticky header strip above the chat panel: clearly part of
            the chrome, ruled off from the messages below. */}
        <section className="dw-deck-chat relative lg:border-r lg:border-[color:var(--rule)] lg:pr-2 xl:pr-4">
          {uiVariant === "thread" ? (
            <div className="lg:sticky lg:top-[57px] lg:flex lg:h-[calc(100dvh-57px)] lg:flex-col">
              <ChatPanelHeader uiVariant={uiVariant} />
              <div className="min-h-0 flex-1">
                <ChatThread job={job} sessionId={sessionId} />
              </div>
            </div>
          ) : (
            <div className="lg:sticky lg:top-[57px] lg:flex lg:max-h-[calc(100dvh-57px)] lg:flex-col">
              <ChatPanelHeader uiVariant={uiVariant} />
              <div className="min-h-0 flex-1 lg:overflow-y-auto lg:pb-10 lg:[scrollbar-width:thin]">
                <Chat job={job} sessionId={sessionId} compact />
              </div>
            </div>
          )}
        </section>

        {/* ── Right: live HTML preview stack ──────────────────────────── */}
        <section className="dw-deck-preview relative lg:pl-2">
          <div className="lg:sticky lg:top-[57px] lg:max-h-[calc(100dvh-57px)] lg:overflow-y-auto lg:pl-4 lg:pr-2 lg:pt-2 lg:[scrollbar-width:thin]">
            <LivePreviewStack job={job} />
          </div>
        </section>
      </div>
    </AgentSessionProvider>
  );
}

// ChatPanelHeader — the thin chrome row above the chat surface. Sprint
// U follow-up — replaces the old floating UiToggle (which was
// `lg:absolute` and read as a disconnected badge sitting on top of
// chat content). Now the variant toggle lives in a proper editorial
// header: kicker on the left, tab toggle on the right, hairline rule
// underneath separating chrome from messages.
function ChatPanelHeader({ uiVariant }: { uiVariant: "log" | "thread" }) {
  return (
    <div className="flex items-baseline justify-between gap-3 border-b border-[color:var(--rule)] px-4 py-3 lg:px-5">
      <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
        § Conversation
      </span>
      <div className="inline-flex border border-[color:var(--rule)]">
        <TabLink href="?" active={uiVariant === "thread"} label="Chat" />
        <TabLink href="?ui=log" active={uiVariant === "log"} label="Log" />
      </div>
    </div>
  );
}

function TabLink({
  href,
  active,
  label,
}: {
  href: string;
  active: boolean;
  label: string;
}) {
  return (
    <a
      href={href}
      className={
        active
          ? "bg-[color:var(--ink)] px-3 py-1.5 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--paper)]"
          : "px-3 py-1.5 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] hover:bg-[color:var(--paper)]/80"
      }
    >
      {label}
    </a>
  );
}

// DeckNotFound — terminal state shown when GET /slides/{id} returns
// 404. Typically happens after an orchestrator restart with the
// in-memory store (data lost on restart) or if the user opened a
// deep-link to a deck someone else generated. The polling effect has
// already pruned this deck from localStorage by the time we render.
function DeckNotFound({ jobId }: { jobId: string }) {
  const wrapRef = useRef<HTMLDivElement | null>(null);

  // Sprint Y2 — terminal-state card scales in (subtle 0.96 → 1) so the
  // "your deck is gone" message has a beat to land, rather than
  // appearing as a hard cut. feedback ease feels right here — the
  // page is *settling on bad news*, not announcing it.
  useGSAP(
    () => {
      const mm = gsap.matchMedia();
      mm.add(PREFERS_FULL_MOTION, () => {
        gsap.from(".dw-deck-notfound", {
          scale: 0.96,
          opacity: 0,
          duration: DUR.secondary,
          ease: EASE.feedback,
        });
      });
      mm.add("(prefers-reduced-motion: reduce)", () => {
        gsap.from(".dw-deck-notfound", { opacity: 0, duration: 0.2 });
      });
    },
    { scope: wrapRef },
  );

  return (
    <div ref={wrapRef} className="mx-auto max-w-2xl px-2 py-24">
      <div
        className="dw-deck-notfound relative border border-[color:var(--ink)]/15 bg-[#FBF9F2] p-10 text-center"
        style={{
          boxShadow:
            "0 2px 0 rgba(26,22,20,0.04), 0 18px 32px -20px rgba(50,40,32,0.32)",
        }}
      >
        <div className="mx-auto mb-5 flex h-12 w-12 items-center justify-center border border-[color:var(--ink)]/20 bg-white">
          <FileQuestion
            size={22}
            strokeWidth={1.4}
            className="text-[color:var(--ink-soft)]"
          />
        </div>
        <p className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--vermillion)]">
          § Deck No Longer Available
        </p>
        <h3 className="mt-4 font-display text-[26px] leading-tight text-[color:var(--ink)]">
          这个 deck 已经不在服务器上了
        </h3>
        <p className="mx-auto mt-3 max-w-md font-display text-[15px] italic leading-relaxed text-[color:var(--ink-soft)]">
          可能是后端重启后 in-memory 状态丢失了，或者这是别人生成的
          deck 链接。我们已经把它从你本机的「最近 decks」列表里清理掉。
        </p>
        <p className="mt-2 font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
          Job ID · {jobId.slice(0, 8)}…
        </p>
        <div className="mt-7 flex items-center justify-center gap-3">
          <Link
            href="/"
            className="inline-flex items-center gap-2 border border-[color:var(--ink)]/25 px-4 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:border-[color:var(--ink)] hover:text-[color:var(--ink)]"
          >
            <ArrowLeft size={11} strokeWidth={1.8} />
            返回首页
          </Link>
          <Link
            href="/slides/new"
            className="group inline-flex items-center gap-2 bg-[color:var(--ink)] px-4 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--paper)] transition-colors hover:bg-[color:var(--vermillion)]"
          >
            <Plus size={11} strokeWidth={1.8} />
            新建 deck
          </Link>
        </div>
      </div>
    </div>
  );
}
