"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { ArrowLeft, FileQuestion, Plus } from "lucide-react";

import { ApiError, getSlideJob, type SlideJob } from "@/lib/api";
import { Chat } from "@/components/chat/Chat";
import { ChatThread } from "@/components/chat/ChatThread";
import { AgentSessionProvider } from "@/components/chat/transport";
import { ConnectionToast } from "@/components/chat/ConnectionToast";
import { LivePreviewStack } from "@/components/slides-preview/LivePreviewStack";
import { SessionSwitcherPopover } from "@/components/slides/SessionSwitcherPopover";
import { PixelButton, Sprite, StatusChip, type PixelStatus } from "@/components/ui/pixel";
import { forgetDeck, updateDeckTitle } from "@/lib/recentDecks";

// The slides workspace is a two-column terminal:
//   left  — chat-style generation timeline (the running conversation)
//   right — live HTML preview stack (the typeset proof)
//
// Pixel re-skin: warm paper + dot-grid, hard pixel borders, monospace.
// Entrances are CSS-driven (.dw-deck-* in globals.css) so nothing strands
// at opacity:0 under React Strict Mode / a backgrounded tab.

export default function SlideJobPage() {
  const params = useParams<{ id: string }>();
  const search = useSearchParams();
  const jobId = params.id;
  const sessionId = search.get("session") ?? "";
  const uiVariant = search.get("ui") === "log" ? "log" : "thread";
  const [job, setJob] = useState<SlideJob | null>(null);
  const [notFound, setNotFound] = useState(false);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const j = await getSlideJob(jobId);
        if (cancelled) return;
        setJob(j);
        if (j.title) updateDeckTitle(jobId, j.title);
        if (j.status !== "finished" && j.status !== "error") {
          timer = setTimeout(poll, 2000);
        }
      } catch (err) {
        if (cancelled) return;
        if (err instanceof ApiError && (err.status === 404 || err.status === 410)) {
          forgetDeck(jobId);
          setNotFound(true);
          return;
        }
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
    <main className="dot-grid relative min-h-[100dvh] bg-paper text-ink antialiased">
      {/* Publication header — hard hairline so it reads as terminal chrome. */}
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
              ~/slides/{jobId.slice(0, 6)}
            </span>
            <DeckStatusChip status={job?.status} />
            {job ? (
              <SessionSwitcherPopover
                currentJobId={job.job_id}
                currentTitle={job.title || job.input?.topic}
              />
            ) : null}
          </div>
        </div>
      </header>

      {job?.status === "error" && !notFound ? <JobErrorBanner job={job} /> : null}

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
          <div className="dw-deck-loading flex items-center gap-2 px-2 py-20 font-pixel text-[0.55rem] tracking-wide text-muted">
            <span className="inline-block h-2 w-2 animate-pixpulse rounded-full bg-accent" />
            LOADING SESSION…
          </div>
        )}
      </div>
    </main>
  );
}

// DeckStatusChip maps job.status → a pixel StatusChip in the header.
function DeckStatusChip({ status }: { status?: SlideJob["status"] }) {
  const map: Record<string, { s: PixelStatus; label: string }> = {
    running: { s: "working", label: "In Press" },
    awaiting_wizard: { s: "queued", label: "Awaiting" },
    awaiting_clarification: { s: "queued", label: "Awaiting" },
    awaiting_outline_approval: { s: "queued", label: "Review" },
    finished: { s: "done", label: "Set in Type" },
    error: { s: "error", label: "Withdrawn" },
  };
  const m = status ? map[status] : undefined;
  return (
    <StatusChip status={m?.s ?? "idle"} className="hidden md:inline-flex">
      {m?.label ?? "Loading"}
    </StatusChip>
  );
}

// Sticky banner shown below the header when job.status flipped to "error".
function JobErrorBanner({ job }: { job: SlideJob }) {
  const topic = (job.input?.topic || job.title || "").slice(0, 240);
  const escapedTopic = encodeURIComponent(topic);
  return (
    <div className="sticky top-0 z-20 border-b-2 border-ink bg-[#fdece9]/80 backdrop-blur-sm">
      <div className="mx-auto flex max-w-[1480px] flex-wrap items-center gap-3 px-4 py-2.5 md:px-10">
        <span className="font-pixel text-[0.55rem] tracking-wide text-[#a23a2a]">✗ 本次生成失败</span>
        <span className="min-w-0 flex-1 truncate font-mono text-[13px] text-ink">
          {job.error || "agent stopped without producing a deck"}
        </span>
        <Link href={topic ? `/slides/new?topic=${escapedTopic}` : "/slides/new"} className="no-underline">
          <PixelButton variant="primary" className="!py-1.5 text-[0.72rem]">
            {topic ? "新建相似 deck" : "返回首页"}
          </PixelButton>
        </Link>
      </div>
    </div>
  );
}

// Workspace owns the two-column split + the shared event stream. One
// WebSocket (AgentSessionProvider) fans out to chat (left) + preview (right).
function Workspace({
  job,
  sessionId,
  uiVariant,
  onDeckGone,
}: {
  job: SlideJob;
  sessionId: string;
  uiVariant: "log" | "thread";
  onDeckGone: () => void;
}) {
  return (
    <AgentSessionProvider sessionId={sessionId || job.session_id} onDeckGone={onDeckGone}>
      <ConnectionToast />
      <div className="grid grid-cols-1 gap-x-10 lg:grid-cols-[minmax(440px,38fr)_minmax(0,62fr)] xl:gap-x-14">
        {/* ── Left: chat surface ─────────────────────────────────────── */}
        <section className="dw-deck-chat relative lg:border-r-2 lg:border-ink lg:pr-2 xl:pr-4">
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

        {/* ── Right: live HTML preview stack ─────────────────────────── */}
        <section className="dw-deck-preview relative lg:pl-2">
          <div className="lg:sticky lg:top-[57px] lg:max-h-[calc(100dvh-57px)] lg:overflow-y-auto lg:pl-4 lg:pr-2 lg:pt-2 lg:[scrollbar-width:thin]">
            <LivePreviewStack job={job} />
          </div>
        </section>
      </div>
    </AgentSessionProvider>
  );
}

// ChatPanelHeader — thin chrome row above the chat surface with the
// Chat/Log variant toggle.
function ChatPanelHeader({ uiVariant }: { uiVariant: "log" | "thread" }) {
  return (
    <div className="flex items-center justify-between gap-3 border-b-2 border-ink px-4 py-2.5 lg:px-5">
      <span className="font-pixel text-[0.55rem] tracking-wide text-muted">§ CONVERSATION</span>
      <div className="inline-flex overflow-hidden rounded-pixel border-2 border-ink">
        <TabLink href="?" active={uiVariant === "thread"} label="Chat" />
        <TabLink href="?ui=log" active={uiVariant === "log"} label="Log" />
      </div>
    </div>
  );
}

function TabLink({ href, active, label }: { href: string; active: boolean; label: string }) {
  return (
    <a
      href={href}
      className={
        active
          ? "bg-ink px-3 py-1 font-mono text-[11px] font-semibold text-paper"
          : "px-3 py-1 font-mono text-[11px] font-semibold text-ink-2 hover:bg-surface-2"
      }
    >
      {label}
    </a>
  );
}

// DeckNotFound — terminal state when GET /slides/{id} returns 404 (after an
// orchestrator restart with the in-memory store, or a foreign deep-link).
function DeckNotFound({ jobId }: { jobId: string }) {
  return (
    <div className="mx-auto max-w-2xl px-2 py-24">
      <div className="dw-deck-notfound relative rounded-pixel border-2 border-ink bg-surface-2 p-10 text-center shadow-pixel-lg">
        <div className="mx-auto mb-5 grid h-12 w-12 place-items-center rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm">
          <FileQuestion size={22} strokeWidth={1.6} className="text-ink-2" />
        </div>
        <p className="font-pixel text-[0.55rem] tracking-wide text-[#a23a2a]">§ DECK NO LONGER AVAILABLE</p>
        <h3 className="mt-4 font-mono text-[24px] font-extrabold leading-tight tracking-tight text-ink">
          这个 deck 已经不在服务器上了
        </h3>
        <p className="mx-auto mt-3 max-w-md font-mono text-[14px] leading-relaxed text-ink-2">
          可能是后端重启后 in-memory 状态丢失了，或者这是别人生成的 deck
          链接。我们已经把它从你本机的「最近 decks」列表里清理掉。
        </p>
        <p className="mt-3 font-pixel text-[0.5rem] tracking-wide text-muted">JOB · {jobId.slice(0, 8)}…</p>
        <div className="mt-7 flex items-center justify-center gap-3">
          <Link href="/" className="no-underline">
            <PixelButton variant="ghost">
              <ArrowLeft size={13} strokeWidth={2} />
              返回首页
            </PixelButton>
          </Link>
          <Link href="/slides/new" className="no-underline">
            <PixelButton variant="accent">
              <Plus size={13} strokeWidth={2} />
              新建 deck
            </PixelButton>
          </Link>
        </div>
        <div className="pointer-events-none absolute -right-3 -top-3 rotate-6">
          <Sprite name="robot" size={34} />
        </div>
      </div>
    </div>
  );
}
