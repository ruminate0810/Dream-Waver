"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Copy, Download, Check, FileText, Layers, Film, ChevronLeft, ChevronRight, BookOpen, ScrollText, FileCode2 } from "lucide-react";
import clsx from "clsx";
import { renderToStaticMarkup } from "react-dom/server";
import { AnimatePresence, motion } from "framer-motion";

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
  revisionStamp = null,
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
  /** v22 质检章 — blocks changed by the latest revision (claw.issue
   *  report_revised). null = no revision seen; 0 = 绿章 (review passed
   *  clean); n>0 = 红章 (review changed n blocks). */
  revisionStamp?: number | null;
}) {
  const stream = useAgentEventStream();
  const [markdown, setMarkdown] = useState<string>("");
  const [readMode, setReadMode] = useState<"scroll" | "swipe">("scroll");
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
            {/* v22 质检章 — the review is not a rubber stamp: say what it changed */}
            {revisionStamp !== null && (
              <span
                title={revisionStamp > 0 ? `评审改稿 ${revisionStamp} 处后定稿` : "评审通过,零修改"}
                className="rounded-[4px] border-2 px-1.5 py-[2px] font-mono text-[9px] font-bold leading-none"
                style={
                  revisionStamp > 0
                    ? { borderColor: "#b23a3a", color: "#b23a3a", transform: "rotate(-4deg)" }
                    : { borderColor: "#3ea96a", color: "#3ea96a", transform: "rotate(-4deg)" }
                }
              >
                {revisionStamp > 0 ? `改 ${revisionStamp} 处` : "评审通过"}
              </span>
            )}
            <span
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData("application/x-claw-report", JSON.stringify({ version }));
                e.dataTransfer.effectAllowed = "copy";
              }}
              title="拖到办公室的制片身上 → 做成 PPT"
              className="grid h-6 cursor-grab place-items-center rounded-pixel border-2 border-dashed border-ink/50 bg-surface px-1.5 font-mono text-[9px] font-bold text-ink-2 shadow-pixel-sm hover:text-ink active:cursor-grabbing"
            >
              拖给制片
            </span>
            <IconButton label={readMode === "swipe" ? "切回滚动阅读" : "分页滑读"} onClick={() => setReadMode((m) => (m === "swipe" ? "scroll" : "swipe"))}>
              {readMode === "swipe" ? <ScrollText size={12} strokeWidth={2} /> : <BookOpen size={12} strokeWidth={2} />}
            </IconButton>
            <IconButton label="复制" onClick={onCopy}>
              {copied ? <Check size={12} strokeWidth={2.4} /> : <Copy size={12} strokeWidth={2} />}
            </IconButton>
            <IconButton label="下载网页版 .html" onClick={() => downloadReportHtml(reportTitle(markdown), markdown)}>
              <FileCode2 size={12} strokeWidth={2} />
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
          readMode === "swipe" ? (
            <ReportSwipe markdown={markdown} />
          ) : (
            <article className="claw-md px-5 py-4">
              <ReactMarkdown remarkPlugins={[remarkGfm]} components={MD_COMPONENTS}>
                {markdown}
              </ReactMarkdown>
            </article>
          )
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

// DeckView — when the deck carries a preview_id AND that slide session is
// actually serving page HTML, render a swipeable live preview; otherwise
// fall back to a clean download card (never the raw endpoint error).
const SLIDE_W = 1280;
const SLIDE_H = 720;

// DeckCard — the download fallback, with an optional hint line.
function DeckCard({ deck, hint }: { deck: ClawDeck; hint?: string }) {
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
        {hint ? <p className="mt-1.5 font-mono text-[11px] text-muted">{hint}</p> : null}
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

function DeckView({ deck }: { deck: ClawDeck }) {
  const count = deck.slide_count ?? 0;
  const [page, setPage] = useState(1);
  const [dir, setDir] = useState(1);
  const frameRef = useRef<HTMLDivElement | null>(null);
  const [scale, setScale] = useState(0.5);
  // null = checking, true = serving HTML, false = unavailable → download card
  const [previewOk, setPreviewOk] = useState<boolean | null>(null);

  useEffect(() => {
    if (!deck.preview_id || count === 0) {
      setPreviewOk(false);
      return;
    }
    let cancelled = false;
    setPreviewOk(null);
    (async () => {
      try {
        const r = await fetch(`/api/v1/slides/${deck.preview_id}/page/1.html`);
        const ct = r.headers.get("content-type") ?? "";
        if (!cancelled) setPreviewOk(r.ok && ct.includes("html"));
      } catch {
        if (!cancelled) setPreviewOk(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [deck.preview_id, count]);

  useEffect(() => {
    const el = frameRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setScale(el.clientWidth / SLIDE_W));
    ro.observe(el);
    setScale(el.clientWidth / SLIDE_W);
    return () => ro.disconnect();
  }, [deck.preview_id, previewOk]);

  const go = (next: number) => {
    if (next < 1 || next > count) return;
    setDir(next > page ? 1 : -1);
    setPage(next);
  };

  if (previewOk === false) return <DeckCard deck={deck} hint={deck.preview_id ? "在线预览暂不可用 — 下载 .pptx 查看完整版" : undefined} />;
  if (previewOk === null) {
    return (
      <div className="flex h-full min-h-[220px] items-center justify-center gap-2 font-pixel text-[0.55rem] tracking-wide text-muted">
        <span className="inline-block h-2 w-2 animate-pixpulse rounded-full bg-accent" />
        载入预览…
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col gap-2 p-4">
      <div className="flex items-center justify-between">
        <p className="truncate font-mono text-[12px] font-bold text-ink">{deck.title || "幻灯片 deck"}</p>
        <a
          href={deck.url}
          download
          className="inline-flex flex-none items-center gap-1.5 rounded-pixel border-2 border-ink bg-surface px-2 py-1 font-mono text-[11px] font-semibold text-ink-2 shadow-pixel-sm transition-colors hover:text-ink"
        >
          <Download size={11} strokeWidth={2} />
          .pptx
        </a>
      </div>
      {/* the slide — drag horizontally to flip */}
      <div ref={frameRef} className="relative w-full overflow-hidden rounded-pixel border-2 border-ink bg-white shadow-pixel-sm" style={{ height: SLIDE_H * scale }}>
        <AnimatePresence initial={false} custom={dir} mode="popLayout">
          <motion.div
            key={page}
            custom={dir}
            initial={{ x: dir * 60, opacity: 0 }}
            animate={{ x: 0, opacity: 1 }}
            exit={{ x: -dir * 60, opacity: 0 }}
            transition={{ duration: 0.22, ease: "easeOut" }}
            drag="x"
            dragConstraints={{ left: 0, right: 0 }}
            dragElastic={0.18}
            onDragEnd={(_, info) => {
              if (info.offset.x < -56) go(page + 1);
              else if (info.offset.x > 56) go(page - 1);
            }}
            className="absolute inset-0 cursor-grab active:cursor-grabbing"
          >
            <iframe
              src={`/api/v1/slides/${deck.preview_id}/page/${page}.html`}
              title={`slide ${page}`}
              className="pointer-events-none origin-top-left border-0"
              style={{ width: SLIDE_W, height: SLIDE_H, transform: `scale(${scale})` }}
            />
          </motion.div>
        </AnimatePresence>
      </div>
      {/* pager: arrows + dots */}
      <div className="flex items-center justify-center gap-2">
        <button
          type="button"
          onClick={() => go(page - 1)}
          disabled={page <= 1}
          className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface text-ink-2 shadow-pixel-sm disabled:opacity-30"
          aria-label="上一页"
        >
          <ChevronLeft size={13} strokeWidth={2.4} />
        </button>
        <div className="flex items-center gap-1">
          {Array.from({ length: count }, (_, i) => (
            <button
              key={i}
              type="button"
              onClick={() => go(i + 1)}
              aria-label={`第 ${i + 1} 页`}
              className={clsx(
                "h-[7px] rounded-full transition-all",
                i + 1 === page ? "w-4 bg-accent" : "w-[7px] bg-ink/20 hover:bg-ink/40",
              )}
            />
          ))}
        </div>
        <button
          type="button"
          onClick={() => go(page + 1)}
          disabled={page >= count}
          className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface text-ink-2 shadow-pixel-sm disabled:opacity-30"
          aria-label="下一页"
        >
          <ChevronRight size={13} strokeWidth={2.4} />
        </button>
        <span className="ml-1 font-mono text-[10px] text-muted">
          {page}/{count}
        </span>
      </div>
    </div>
  );
}

// ReportSwipe — 分页滑读: the report split by H2 headings into swipeable
// magazine-style cards. The cover card is everything before the first H2.
function ReportSwipe({ markdown }: { markdown: string }) {
  const sections = useMemo(() => {
    const parts = markdown.split(/\n(?=##\s)/).map((s) => s.trim()).filter(Boolean);
    return parts.length > 0 ? parts : [markdown];
  }, [markdown]);
  const [page, setPage] = useState(0);
  const [dir, setDir] = useState(1);
  const go = (n: number) => {
    if (n < 0 || n >= sections.length) return;
    setDir(n > page ? 1 : -1);
    setPage(n);
  };
  return (
    <div className="flex h-full flex-col">
      <div className="relative min-h-0 flex-1 overflow-hidden">
        <AnimatePresence initial={false} custom={dir} mode="popLayout">
          <motion.article
            key={page}
            custom={dir}
            initial={{ x: dir * 80, opacity: 0 }}
            animate={{ x: 0, opacity: 1 }}
            exit={{ x: -dir * 80, opacity: 0 }}
            transition={{ duration: 0.22, ease: "easeOut" }}
            drag="x"
            dragConstraints={{ left: 0, right: 0 }}
            dragElastic={0.18}
            onDragEnd={(_, info) => {
              if (info.offset.x < -56) go(page + 1);
              else if (info.offset.x > 56) go(page - 1);
            }}
            className="claw-md absolute inset-0 cursor-grab overflow-y-auto px-5 py-4 active:cursor-grabbing"
          >
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={MD_COMPONENTS}>
              {sections[page]}
            </ReactMarkdown>
          </motion.article>
        </AnimatePresence>
      </div>
      <div className="flex flex-none items-center justify-center gap-2 border-t-2 border-line py-1.5">
        <button
          type="button"
          onClick={() => go(page - 1)}
          disabled={page <= 0}
          className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface text-ink-2 shadow-pixel-sm disabled:opacity-30"
          aria-label="上一节"
        >
          <ChevronLeft size={13} strokeWidth={2.4} />
        </button>
        <div className="flex items-center gap-1">
          {sections.map((_, i) => (
            <button
              key={i}
              type="button"
              onClick={() => go(i)}
              aria-label={`第 ${i + 1} 节`}
              className={clsx(
                "h-[7px] rounded-full transition-all",
                i === page ? "w-4 bg-accent" : "w-[7px] bg-ink/20 hover:bg-ink/40",
              )}
            />
          ))}
        </div>
        <button
          type="button"
          onClick={() => go(page + 1)}
          disabled={page >= sections.length - 1}
          className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface text-ink-2 shadow-pixel-sm disabled:opacity-30"
          aria-label="下一节"
        >
          <ChevronRight size={13} strokeWidth={2.4} />
        </button>
        <span className="ml-1 font-mono text-[10px] text-muted">
          {page + 1}/{sections.length}
        </span>
      </div>
    </div>
  );
}

// downloadFile force-saves a (possibly cross-origin OSS) asset: a plain
// <a download> is ignored for cross-origin URLs, so we fetch the blob and
// download that; if CORS blocks the fetch, fall back to opening in a new tab
// so the user can still save it manually.
async function downloadFile(url: string, filename: string) {
  try {
    const r = await fetch(url, { mode: "cors" });
    if (!r.ok) throw new Error(String(r.status));
    const blob = await r.blob();
    const obj = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = obj;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(obj), 1000);
  } catch {
    window.open(url, "_blank", "noopener");
  }
}

// REPORT_CSS — a clean, self-contained document stylesheet for the exported
// .html so it reads like a published article in any browser, offline.
const REPORT_CSS = `
*{box-sizing:border-box}
body{margin:0;background:#f6f3ec;color:#1a1813;font:16px/1.75 -apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif}
main{max-width:760px;margin:0 auto;padding:56px 24px 96px}
h1{font-size:2em;line-height:1.25;margin:0 0 .5em}
h2{font-size:1.4em;margin:1.9em 0 .5em;padding-bottom:.25em;border-bottom:2px solid #e3ddcd}
h3{font-size:1.15em;margin:1.4em 0 .4em}
p{margin:.75em 0}
a{color:#5b46e0;text-decoration:none}a:hover{text-decoration:underline}
ul,ol{padding-left:1.5em}li{margin:.3em 0}
img{max-width:100%;height:auto;border-radius:8px;margin:1.2em 0;box-shadow:0 2px 12px rgba(0,0,0,.1)}
table{border-collapse:collapse;width:100%;margin:1.3em 0;font-size:.95em}
th,td{border:1px solid #d8d0bf;padding:7px 11px;text-align:left}
th{background:#efe9da}
code{background:#ece6d6;padding:2px 5px;border-radius:4px;font-size:.9em}
pre{background:#2b2720;color:#f2ede0;padding:14px 16px;border-radius:10px;overflow:auto}pre code{background:none;padding:0}
blockquote{margin:1em 0;padding:.3em 1.1em;border-left:3px solid #c9bfa6;color:#5a5443}
hr{border:none;border-top:1px solid #ddd5c3;margin:2em 0}
footer{margin-top:64px;padding-top:16px;border-top:1px solid #ddd5c3;color:#8a8170;font-size:.82em}
`;

function escapeHtml(s: string): string {
  return s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c] as string);
}

function reportTitle(md: string): string {
  return (md.match(/^#\s+(.+)$/m)?.[1] || "报告").trim();
}

// downloadReportHtml renders the markdown to clean semantic HTML (no chat-bubble
// classes), wraps it with REPORT_CSS into a self-contained doc, and downloads it.
function downloadReportHtml(title: string, markdown: string) {
  const body = renderToStaticMarkup(
    <ReactMarkdown remarkPlugins={[remarkGfm]}>{markdown}</ReactMarkdown>,
  );
  const doc = `<!DOCTYPE html>
<html lang="zh"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>${escapeHtml(title)}</title>
<style>${REPORT_CSS}</style></head>
<body><main>${body}<footer>由 Dream-Waver · Claw 团队生成</footer></main></body></html>`;
  const blob = new Blob([doc], { type: "text/html;charset=utf-8" });
  const obj = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = obj;
  a.download = `${title.replace(/[\\/:*?"<>|\n\r]+/g, " ").trim().slice(0, 40) || "报告"}.html`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(obj), 1000);
}

// figureFilename derives a clean filename from the figure's caption (drops a
// 海报: prefix, strips illegal chars), falling back to figure-N.
function figureFilename(caption: string | undefined, i: number): string {
  const base = (caption ?? "")
    .replace(/^海报[:：]\s*/, "")
    .replace(/[\\/:*?"<>|\n\r]+/g, " ")
    .trim()
    .slice(0, 40);
  return `${base || `figure-${i + 1}`}.png`;
}

function FiguresGallery({ figures }: { figures: ClawFigure[] }) {
  return (
    <div className="grid grid-cols-1 gap-4 p-4 sm:grid-cols-2">
      {figures.map((f, i) => (
        <figure
          key={i}
          draggable
          onDragStart={(e) => {
            e.dataTransfer.setData("application/x-claw-figure", JSON.stringify({ index: i, caption: f.caption ?? "" }));
            e.dataTransfer.effectAllowed = "copy";
          }}
          title="拖到办公室的设计师(精修)或视频师(成片)身上"
          className="relative cursor-grab overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm active:cursor-grabbing"
        >
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={f.url} alt={f.caption ?? `figure ${i + 1}`} className="block w-full" />
          <button
            type="button"
            onClick={() => void downloadFile(f.url, figureFilename(f.caption, i))}
            title="下载这张图"
            aria-label="下载这张图"
            className="absolute right-2 top-2 grid h-8 w-8 place-items-center rounded-pixel border-2 border-ink bg-surface/95 text-ink-2 shadow-pixel-sm transition-transform hover:-translate-y-0.5 hover:text-ink"
          >
            <Download size={14} strokeWidth={2} />
          </button>
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
              <button
                type="button"
                onClick={() => void downloadFile(v.url, `${(v.caption || `clip-${i + 1}`).replace(/[\\/:*?"<>|\n\r]+/g, " ").trim().slice(0, 40)}.mp4`)}
                className="grid h-5 w-5 place-items-center rounded-pixel border border-ink/40 text-ink-2 hover:text-ink"
                aria-label="下载视频"
              >
                <Download size={11} strokeWidth={2} />
              </button>
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
