"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Copy, Download, Check, FileText, Layers, Film } from "lucide-react";
import clsx from "clsx";

import { StatusChip } from "@/components/ui/pixel";
import { fetchClawArtifact, clawArtifactURL, type ClawFigure, type ClawDeck, type ClawVideo } from "@/lib/api";
import {
  useAgentEventStream,
  type AgentEvent,
} from "@/components/chat/transport";

export type WorkTab = "report" | "figures" | "video" | "deck";

// ArtifactBody renders the Claw work package (report / figures / deck tabs)
// FRAMELESS — it lives inside the office's PixelWindow (the OS-window
// metaphor), which provides the chrome. The report body NEVER rides the WS —
// on mount we GET /claw/{id}/artifact, and we re-fetch only when a
// claw.artifact.updated event reports a HIGHER version than we currently
// hold (a monotonic guard so a replayed/stale event can't clobber a newer
// fetch). react-markdown + remark-gfm renders headings / tables / links;
// the components map dresses them in the pixel type scale.

export function ArtifactBody({
  jobId,
  initialVersion = 0,
  figures = [],
  videos = [],
  deck = null,
  tab,
  onTabChange,
}: {
  jobId: string;
  /** Version known at page load (from getClawRun); seeds the guard so we
   *  don't re-fetch for an event that's older than what we already have. */
  initialVersion?: number;
  /** Work-package figures (from getClawRun polling). */
  figures?: ClawFigure[];
  /** Work-package videos / clips (from getClawRun polling). */
  videos?: ClawVideo[];
  /** Work-package slide deck (from getClawRun polling). */
  deck?: ClawDeck | null;
  /** Controlled active tab (the office dock drives this). */
  tab: WorkTab;
  onTabChange: (t: WorkTab) => void;
}) {
  const stream = useAgentEventStream();
  const [markdown, setMarkdown] = useState<string>("");
  const [version, setVersion] = useState<number>(0);
  const [loading, setLoading] = useState<boolean>(true);
  const [copied, setCopied] = useState(false);
  // Ref mirror of version so the event handler reads the latest without
  // re-subscribing every render.
  const versionRef = useRef<number>(0);

  const load = useCallback(async () => {
    try {
      const res = await fetchClawArtifact(jobId);
      if (res) {
        setMarkdown(res.markdown);
        setVersion(res.version);
        versionRef.current = res.version;
      }
    } catch {
      /* best-effort — a transient fetch error just leaves the prior body */
    } finally {
      setLoading(false);
    }
  }, [jobId]);

  // Initial fetch (only when the page says a report already exists).
  useEffect(() => {
    if (initialVersion > 0) {
      load();
    } else {
      setLoading(false);
    }
  }, [initialVersion, load]);

  // Re-fetch on a newer version notification.
  useEffect(() => {
    const handle = (ev: AgentEvent) => {
      if (ev.kind !== "claw.artifact.updated") return;
      const v = ev.data.artifact_version ?? 0;
      if (v > versionRef.current) {
        versionRef.current = v; // claim it immediately so concurrent events don't double-fetch
        load();
      }
    };
    return stream.subscribe(handle);
  }, [stream, load]);

  const onCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(markdown);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard blocked — no-op */
    }
  }, [markdown]);

  const hasReport = version > 0 && markdown.trim().length > 0;
  // Active tab with availability guards (a tab can't be active if empty).
  const active: WorkTab =
    tab === "figures" && figures.length > 0
      ? "figures"
      : tab === "video" && videos.length > 0
        ? "video"
        : tab === "deck" && deck
          ? "deck"
          : "report";

  return (
    <div className="flex h-full flex-col bg-paper/60">
      {/* work-package tabs + report actions */}
      <div className="flex flex-none items-center gap-1 border-b-2 border-ink bg-surface-2 px-2 py-1.5">
        <TabButton active={active === "report"} onClick={() => onTabChange("report")}>
          报告{hasReport ? ` · v${version}` : ""}
        </TabButton>
        <TabButton active={active === "figures"} onClick={() => onTabChange("figures")} disabled={figures.length === 0}>
          配图{figures.length > 0 ? ` · ${figures.length}` : ""}
        </TabButton>
        <TabButton active={active === "video"} onClick={() => onTabChange("video")} disabled={videos.length === 0}>
          视频{videos.length > 0 ? ` · ${videos.length}` : ""}
        </TabButton>
        <TabButton active={active === "deck"} onClick={() => onTabChange("deck")} disabled={!deck}>
          Deck
        </TabButton>
        {active === "report" && hasReport && (
          <span className="ml-auto flex items-center gap-1.5">
            <IconButton label="复制" onClick={onCopy}>
              {copied ? <Check size={12} strokeWidth={2.4} /> : <Copy size={12} strokeWidth={2} />}
            </IconButton>
            <a
              href={clawArtifactURL(jobId)}
              download={`report-${jobId.slice(0, 6)}.md`}
              className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface text-ink-2 shadow-pixel-sm transition-transform hover:-translate-y-0.5 hover:text-ink"
              aria-label="下载 .md"
            >
              <Download size={12} strokeWidth={2} />
            </a>
          </span>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {active === "deck" && deck ? (
          <DeckView deck={deck} />
        ) : active === "video" ? (
          <VideoGallery videos={videos} />
        ) : active === "figures" ? (
          <FiguresGallery figures={figures} />
        ) : hasReport ? (
          <article className="claw-md px-5 py-4">
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={MD_COMPONENTS}>
              {markdown}
            </ReactMarkdown>
          </article>
        ) : (
          <EmptyState loading={loading} />
        )}
      </div>
    </div>
  );
}

function TabButton({
  active,
  disabled,
  onClick,
  children,
}: {
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={clsx(
        "rounded-pixel border-2 px-2.5 py-1 font-mono text-[11px] font-semibold transition-colors",
        active
          ? "border-ink bg-accent text-white shadow-pixel-sm"
          : disabled
            ? "cursor-not-allowed border-line-2 bg-surface text-line-2"
            : "border-line-2 bg-surface text-ink-2 hover:border-ink hover:text-ink",
      )}
    >
      {children}
    </button>
  );
}

function DeckView({ deck }: { deck: ClawDeck }) {
  return (
    <div className="flex h-full min-h-[220px] flex-col items-center justify-center gap-4 p-8 text-center">
      <span className="grid h-14 w-14 place-items-center rounded-pixel border-2 border-ink bg-accent-soft text-accent shadow-pixel-sm">
        <Layers size={24} strokeWidth={1.6} />
      </span>
      <div>
        <p className="font-mono text-[15px] font-bold text-ink">{deck.title || "幻灯片 deck"}</p>
        {deck.slide_count ? (
          <p className="mt-1 font-mono text-[12px] text-muted">{deck.slide_count} 页 · PowerPoint (.pptx)</p>
        ) : null}
      </div>
      <a
        href={deck.url}
        download
        className="inline-flex items-center gap-2 rounded-pixel border-2 border-ink bg-accent px-4 py-2.5 font-mono text-[13px] font-semibold text-white shadow-pixel-sm transition-transform hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none"
      >
        <Download size={14} strokeWidth={2} />
        下载 .pptx
      </a>
    </div>
  );
}

function FiguresGallery({ figures }: { figures: ClawFigure[] }) {
  return (
    <div className="grid grid-cols-1 gap-4 p-4 sm:grid-cols-2">
      {figures.map((f, i) => (
        <figure key={i} className="overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={f.url} alt={f.caption ?? `figure ${i + 1}`} className="block w-full" />
          {f.caption && (
            <figcaption className="border-t-2 border-line px-2.5 py-1.5 font-mono text-[11px] text-ink-2">
              {f.caption}
            </figcaption>
          )}
        </figure>
      ))}
    </div>
  );
}

function VideoGallery({ videos }: { videos: ClawVideo[] }) {
  return (
    <div className="grid grid-cols-1 gap-4 p-4">
      {videos.map((v, i) => (
        <figure key={i} className="overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm">
          <video
            src={v.url}
            poster={v.poster}
            controls
            loop
            playsInline
            className="block w-full bg-ink"
          />
          <figcaption className="flex items-center justify-between gap-2 border-t-2 border-line px-2.5 py-1.5">
            <span className="truncate font-mono text-[11px] text-ink-2">{v.caption || `视频 ${i + 1}`}</span>
            <span className="flex flex-none items-center gap-1.5 font-mono text-[10px] text-muted">
              {v.resolution && (
                <span className="rounded-[3px] border border-line-2 bg-surface-2 px-1 py-[1px]">{v.resolution}</span>
              )}
              {v.duration ? <span>{v.duration}s</span> : null}
              <a
                href={v.url}
                download={`clip-${i + 1}.mp4`}
                className="grid h-5 w-5 place-items-center rounded-pixel border border-ink/40 text-ink-2 hover:text-ink"
                aria-label="下载视频"
              >
                <Download size={11} strokeWidth={2} />
              </a>
            </span>
          </figcaption>
        </figure>
      ))}
      {videos.length === 0 && (
        <div className="flex min-h-[160px] flex-col items-center justify-center gap-3 text-center">
          <span className="grid h-12 w-12 place-items-center rounded-pixel border-2 border-line-2 bg-surface-2 text-line-2">
            <Film size={20} strokeWidth={1.6} />
          </span>
          <p className="font-mono text-[12px] text-muted">还没有视频 — 在对话里让视频师把配图做成短片</p>
        </div>
      )}
    </div>
  );
}

function EmptyState({ loading }: { loading: boolean }) {
  return (
    <div className="flex h-full min-h-[220px] flex-col items-center justify-center gap-3 px-6 py-12 text-center">
      <span className="grid h-12 w-12 place-items-center rounded-pixel border-2 border-line-2 bg-surface-2 text-line-2">
        <FileText size={20} strokeWidth={1.6} />
      </span>
      {loading ? (
        <StatusChip status="working">撰写中</StatusChip>
      ) : (
        <p className="font-mono text-[12px] text-muted">
          报告尚未生成 — 工作完成后会在这里实时呈现
        </p>
      )}
    </div>
  );
}

function IconButton({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface text-ink-2 shadow-pixel-sm transition-transform hover:-translate-y-0.5 hover:text-ink"
    >
      {children}
    </button>
  );
}

// MD_COMPONENTS dresses react-markdown's output in the pixel type scale.
// Kept inline (not a CSS file) so the styling travels with the component.
const MD_COMPONENTS = {
  h1: (p: React.HTMLAttributes<HTMLHeadingElement>) => (
    <h1 className="mb-3 mt-1 font-mono text-[22px] font-extrabold leading-tight tracking-tight text-ink" {...p} />
  ),
  h2: (p: React.HTMLAttributes<HTMLHeadingElement>) => (
    <h2 className="mb-2 mt-6 border-b-2 border-line pb-1 font-mono text-[17px] font-bold text-ink" {...p} />
  ),
  h3: (p: React.HTMLAttributes<HTMLHeadingElement>) => (
    <h3 className="mb-1.5 mt-4 font-mono text-[14px] font-bold text-ink" {...p} />
  ),
  p: (p: React.HTMLAttributes<HTMLParagraphElement>) => (
    <p className="mb-3 font-mono text-[13.5px] leading-relaxed text-ink-2" {...p} />
  ),
  ul: (p: React.HTMLAttributes<HTMLUListElement>) => (
    <ul className="mb-3 ml-5 list-disc space-y-1 font-mono text-[13.5px] text-ink-2 marker:text-accent" {...p} />
  ),
  ol: (p: React.HTMLAttributes<HTMLOListElement>) => (
    <ol className="mb-3 ml-5 list-decimal space-y-1 font-mono text-[13.5px] text-ink-2 marker:text-accent" {...p} />
  ),
  li: (p: React.LiHTMLAttributes<HTMLLIElement>) => <li className="leading-relaxed" {...p} />,
  a: (p: React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a className="font-semibold text-accent underline decoration-accent/40 underline-offset-2 hover:decoration-accent" target="_blank" rel="noreferrer" {...p} />
  ),
  strong: (p: React.HTMLAttributes<HTMLElement>) => <strong className="font-bold text-ink" {...p} />,
  blockquote: (p: React.HTMLAttributes<HTMLQuoteElement>) => (
    <blockquote className="mb-3 border-l-4 border-accent bg-accent-soft/50 px-3 py-1.5 font-mono text-[13px] italic text-ink-2" {...p} />
  ),
  code: (p: React.HTMLAttributes<HTMLElement>) => (
    <code className="rounded-[3px] border border-line-2 bg-surface-2 px-1 py-0.5 font-mono text-[12px] text-ink" {...p} />
  ),
  table: (p: React.TableHTMLAttributes<HTMLTableElement>) => (
    <div className="mb-4 overflow-x-auto">
      <table className="w-full border-collapse border-2 border-ink font-mono text-[12.5px]" {...p} />
    </div>
  ),
  th: (p: React.ThHTMLAttributes<HTMLTableCellElement>) => (
    <th className="border-2 border-ink bg-surface-2 px-2.5 py-1.5 text-left font-bold text-ink" {...p} />
  ),
  td: (p: React.TdHTMLAttributes<HTMLTableCellElement>) => (
    <td className="border-2 border-line px-2.5 py-1.5 text-ink-2" {...p} />
  ),
  hr: (p: React.HTMLAttributes<HTMLHRElement>) => <hr className="my-5 border-t-2 border-line" {...p} />,
};
