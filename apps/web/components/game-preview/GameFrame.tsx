"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2, RefreshCw, ExternalLink, Maximize2, Eye } from "lucide-react";
import clsx from "clsx";

import { gameRevisionPlayURL, type GameJob } from "@/lib/api";
import { RevisionStrip } from "./RevisionStrip";
import { GameSource } from "./GameSource";

type View = "preview" | "source";

// GameFrame is the right-hand preview pane. It hosts an iframe that points
// at /api/v1/games/{id}/play (same-origin via Next.js rewrites) and bumps
// the src's cache-buster every time the job's status flips back to
// "finished" — that's how we get the iframe to reload after a follow-up
// edit lands without a full page navigation.
//
// While the backend is generating (status==="running") we keep the prior
// frame visible if we have one, and overlay a translucent "Composing" badge
// so the user knows another iteration is in flight.
//
// The RevisionStrip below the header lets the user time-travel to an
// earlier revision (read-only preview) and optionally restore from there.

export function GameFrame({ job }: { job: GameJob }) {
  const [version, setVersion] = useState(0);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [view, setView] = useState<View>("preview");
  // viewingRevision === undefined means "head" — show whatever job.play_url
  // currently points at. A defined idx pins the iframe to a historical
  // revision; user toggles back to head by clicking the head pill again.
  const [viewingRevision, setViewingRevision] = useState<number | undefined>(undefined);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  // Track the previous finishedAt so we only bump the cache key on a real
  // re-render. Useful when the polling loop keeps re-fetching the same
  // finished job — we don't want to keep reloading the iframe.
  const lastFinishedRef = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (job.status !== "finished") return;
    if (!job.finished_at) return;
    if (job.finished_at === lastFinishedRef.current) return;
    lastFinishedRef.current = job.finished_at;
    setVersion((v) => v + 1);
    setHasLoaded(false);
    // A new generation lands → snap back to head so the user sees the
    // newly produced artifact rather than staying parked on an older one.
    setViewingRevision(undefined);
  }, [job.status, job.finished_at]);

  const playable = job.status === "finished" && !!job.play_url;
  const src = playable
    ? viewingRevision !== undefined
      ? `${gameRevisionPlayURL(job.job_id, viewingRevision)}?v=${version}`
      : `${job.play_url}?v=${version}`
    : undefined;

  const focusGame = () => {
    // Click into the iframe so keyboard events flow to the game. Without
    // this the first arrow-key press scrolls the parent page instead of
    // moving the snake. With sandbox="allow-scripts" (null origin) we
    // can't touch contentWindow cross-origin — but focusing the element
    // is enough; the browser forwards subsequent keystrokes to the frame.
    iframeRef.current?.focus();
  };

  const reload = () => {
    setVersion((v) => v + 1);
    setHasLoaded(false);
  };

  const handleRestored = () => {
    // After restore, the head moves to the restored idx. Snap back to
    // head (clear local selection) and force the iframe to refetch.
    setViewingRevision(undefined);
    setVersion((v) => v + 1);
    setHasLoaded(false);
  };

  return (
    <div className="flex h-full flex-col">
      <FrameHeader
        job={job}
        view={view}
        setView={setView}
        onReload={reload}
        onFullscreen={() => {
          if (job.play_url) window.open(job.play_url, "_blank");
        }}
      />
      {job.status === "finished" ? (
        <RevisionStrip
          jobId={job.job_id}
          refreshKey={job.finished_at}
          selectedIdx={viewingRevision}
          onSelect={(idx) => {
            setViewingRevision(idx);
            setVersion((v) => v + 1);
            setHasLoaded(false);
          }}
          onRestored={handleRestored}
          compact
        />
      ) : null}
      {view === "source" && job.status === "finished" ? (
        <GameSource
          jobId={job.job_id}
          revisionIdx={viewingRevision}
          title={job.title || "game"}
        />
      ) : (
        <div className="relative flex-1 overflow-hidden border-2 border-ink bg-[#0f1115] shadow-pixel">
          {src ? (
            <iframe
              ref={iframeRef}
              key={src /* hard remount on version bump — cleanest reset */}
              src={src}
              title={job.title || "Game preview"}
              onLoad={() => {
                setHasLoaded(true);
                focusGame();
              }}
              onClick={focusGame}
              // sandbox=allow-scripts gives the iframe a unique null origin
              // — requestAnimationFrame / keyboard / canvas all work, but
              // the game cannot reach window.parent, read parent cookies,
              // or top-navigate the tab. The artifact comes from an LLM so
              // we treat it as untrusted content.
              sandbox="allow-scripts"
              className="h-full w-full bg-white"
            />
          ) : (
            <PlaceholderCanvas status={job.status} error={job.error} />
          )}

          {/* Read-only banner — only shows when previewing a historical
              revision. Click "Restore" in the strip to fork edits from it. */}
          {viewingRevision !== undefined ? (
            <div className="pointer-events-none absolute left-3 top-3 flex items-center gap-2 rounded-pixel border-2 border-ink bg-accent px-3 py-1.5 font-mono text-[10px] font-semibold uppercase tracking-wide text-white shadow-pixel-sm">
              <Eye size={11} strokeWidth={2} />
              <span>Viewing v{viewingRevision} · read-only</span>
            </div>
          ) : null}

          {/* Running overlay — semi-transparent so the user still sees the
              previous game and gets a sense of continuity. */}
          {job.status === "running" && hasLoaded ? (
            <div className="absolute right-3 top-3 flex items-center gap-2 rounded-pixel border-2 border-ink bg-ink px-3 py-1.5 font-mono text-[10px] font-semibold uppercase tracking-wide text-white shadow-pixel-sm">
              <Loader2 size={11} strokeWidth={2} className="animate-spin text-accent" />
              <span>Composing</span>
            </div>
          ) : null}
        </div>
      )}
      <FrameFooter job={job} />
    </div>
  );
}

function FrameHeader({
  job,
  view,
  setView,
  onReload,
  onFullscreen,
}: {
  job: GameJob;
  view: View;
  setView: (v: View) => void;
  onReload: () => void;
  onFullscreen: () => void;
}) {
  const ready = job.status === "finished" && !!job.play_url;
  return (
    <div className="flex items-center justify-between border-x-2 border-t-2 border-ink bg-surface-2 px-3 py-2">
      <div className="flex items-center gap-3 truncate">
        <span className="flex flex-none gap-1.5">
          <i className="h-[10px] w-[10px] rounded-full border border-ink bg-[#ff8a8a]" />
          <i className="h-[10px] w-[10px] rounded-full border border-ink bg-gold" />
          <i className="h-[10px] w-[10px] rounded-full border border-ink bg-grass" />
        </span>
        <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
          Live
        </span>
        <span className="truncate font-mono text-[13px] font-bold tracking-tight text-ink">
          {job.title || "Untitled game"}
        </span>
      </div>
      <div className="flex items-center gap-2">
        {ready ? (
          <div className="flex overflow-hidden rounded-pixel border-2 border-ink">
            {(["preview", "source"] as View[]).map((v) => (
              <button
                key={v}
                type="button"
                onClick={() => setView(v)}
                className={clsx(
                  "px-2.5 py-1 font-mono text-[10px] font-semibold uppercase tracking-wide transition-colors",
                  view === v
                    ? "bg-ink text-paper"
                    : "text-ink-2 hover:bg-ink/5 hover:text-ink",
                )}
              >
                {v}
              </button>
            ))}
          </div>
        ) : null}
        <IconButton
          onClick={onReload}
          disabled={!ready || view !== "preview"}
          title="Restart game"
        >
          <RefreshCw size={13} strokeWidth={1.8} />
        </IconButton>
        <IconButton onClick={onFullscreen} disabled={!ready} title="Open in new tab">
          <ExternalLink size={13} strokeWidth={1.8} />
        </IconButton>
      </div>
    </div>
  );
}

function IconButton({
  children,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      {...rest}
      className={clsx(
        "rounded-pixel border-2 p-1.5 transition-colors",
        rest.disabled
          ? "cursor-not-allowed border-line-2 text-muted"
          : "border-line-2 text-ink-2 hover:border-ink hover:bg-ink/5 hover:text-ink",
      )}
    >
      {children}
    </button>
  );
}

function FrameFooter({ job }: { job: GameJob }) {
  // Footer surfaces byte size + a one-shot fullscreen affordance. Stays
  // hairline thin so the iframe owns the visual real estate.
  if (job.status !== "finished") {
    return <div className="border-x-2 border-b-2 border-ink px-3 py-2" />;
  }
  return (
    <div className="flex items-center justify-between border-x-2 border-b-2 border-ink bg-surface-2 px-3 py-2 font-mono text-[10px] font-semibold uppercase tracking-wide text-muted">
      <span>{(job.bytes ?? 0).toLocaleString()} bytes</span>
      <a
        href={job.play_url}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1 hover:text-ink"
      >
        <Maximize2 size={10} strokeWidth={1.8} />
        Fullscreen
      </a>
    </div>
  );
}

function PlaceholderCanvas({ status, error }: { status: GameJob["status"]; error?: string }) {
  return (
    <div className="absolute inset-0 flex flex-col items-center justify-center gap-4 bg-[#0f1115] text-white/60">
      {status === "error" ? (
        <>
          <span className="font-mono text-[10px] font-semibold uppercase tracking-wide text-[#ff8a8a]">
            Generation failed
          </span>
          <pre className="max-w-[80%] whitespace-pre-wrap text-center font-mono text-[11px] text-white/50">
            {error || "Unknown error"}
          </pre>
        </>
      ) : (
        <>
          <Loader2 size={28} strokeWidth={1.6} className="animate-spin text-accent" />
          <span className="font-mono text-[10px] font-semibold uppercase tracking-wide">
            Writing your game…
          </span>
        </>
      )}
    </div>
  );
}
