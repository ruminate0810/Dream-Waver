"use client";

import { useCallback, useEffect, useState } from "react";
import { Download, Eye, FileWarning, Pencil } from "lucide-react";
import clsx from "clsx";

import { postSlideMessage, type SlideJob } from "@/lib/api";
import { useAgentEventStream } from "@/components/chat/transport";
import { SlideFrame, type EditRequest } from "./SlideFrame";
import { EditPopover, type EditSubmit, type EditTarget } from "./EditPopover";

// LivePreviewStack is the right-hand pane of the two-column slide editor.
// It renders one live SlideFrame per slide, listens for slides.updated
// events on its own dedicated WebSocket (no coupling to <Chat>), and
// opens the EditPopover when a click bubbles out of any iframe.
//
// Header strip + scroll column composition mirrors a printed-magazine
// gallery section. Number plates sit *outside* each frame in the left
// gutter — a deliberate avoidance of the "card with header bar" tell
// that gives away an AI dashboard.

export function LivePreviewStack({ job }: { job: SlideJob }) {
  const slideCount = job.slide_count ?? 0;
  const ready = job.status !== "running" || slideCount > 0;

  // Per-slide version counter. Each bump force-remounts that one iframe,
  // re-fetching the live HTML. We start everyone at 1 so the URL has a
  // version segment from the very first paint.
  const [versions, setVersions] = useState<number[]>(() =>
    Array.from({ length: Math.max(slideCount, 0) }, () => 1),
  );
  // The 1-based index of the slide that flashed most recently — used
  // to tint that frame's chip in vermillion for ~1.4s post-update.
  const [activeIdx, setActiveIdx] = useState<number | null>(null);

  // Grow / shrink the versions array if the deck changes shape between
  // edits (delete_slide drops one, add_slide grows it). Keep existing
  // versions stable to avoid spurious reloads.
  useEffect(() => {
    setVersions((prev) => {
      if (prev.length === slideCount) return prev;
      const next = prev.slice(0, slideCount);
      while (next.length < slideCount) next.push(1);
      return next;
    });
  }, [slideCount]);

  // Subscribe to the shared event stream from <AgentSessionProvider>.
  // The provider owns the single WebSocket + reconnect logic; we just
  // listen for slides.updated and bump exactly one iframe's version.
  const stream = useAgentEventStream();
  useEffect(() => {
    return stream.subscribe((ev) => {
      if (ev.kind !== "slides.updated") return;
      const oneBased = ev.data.slide_index;
      if (typeof oneBased !== "number") return;
      setVersions((prev) => {
        const next = prev.slice();
        const i = oneBased - 1;
        if (i >= 0 && i < next.length) next[i] = (next[i] ?? 1) + 1;
        return next;
      });
      setActiveIdx(oneBased);
    });
  }, [stream]);

  // Clear the vermillion "just updated" highlight after a moment so the
  // strip doesn't permanently scream for attention.
  useEffect(() => {
    if (activeIdx === null) return;
    const id = setTimeout(() => setActiveIdx(null), 1400);
    return () => clearTimeout(id);
  }, [activeIdx]);

  // ────────── Edit popover state ──────────
  const [target, setTarget] = useState<EditTarget | null>(null);
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const [busy, setBusy] = useState(false);

  const handleEditOpen = useCallback((req: EditRequest, anchorRect: DOMRect) => {
    setTarget({ slideIndex: req.slideIndex, text: req.text, role: req.role });
    setAnchor(anchorRect);
  }, []);
  const handleEditClose = useCallback(() => {
    if (busy) return; // don't close while the agent is mid-edit
    setTarget(null);
    setAnchor(null);
  }, [busy]);

  const handleEditSubmit = useCallback(
    async (s: EditSubmit) => {
      const message = buildEditInstruction(s);
      try {
        setBusy(true);
        await postSlideMessage(job.job_id, message);
        // Optimistically close — the WS will push slides.updated when
        // the new render lands.
        setBusy(false);
        setTarget(null);
        setAnchor(null);
      } catch {
        // Keep the popover open; flip busy off so the user can retry.
        // A proper inline error chip is a follow-up.
        setBusy(false);
      }
    },
    [job.job_id],
  );

  // ────────── Render branches ──────────

  if (!ready) {
    return (
      <section>
        <PaneHeader subtitle="Composing first revision…" />
        <SkeletonStack />
      </section>
    );
  }

  if (slideCount === 0) {
    return (
      <section>
        <PaneHeader subtitle="No slides yet" />
        <EmptyState />
      </section>
    );
  }

  return (
    <section>
      <div className="sticky top-0 z-10 -mx-2 bg-[color:var(--paper)]/85 px-2 pb-3 pt-1 backdrop-blur-sm">
        <PaneHeader
          subtitle={`${slideCount} slide${slideCount === 1 ? "" : "s"}`}
          downloadHref={job.download_url ?? undefined}
        />
        <HintRow />
      </div>

      <ol className="space-y-12">
        {Array.from({ length: slideCount }).map((_, i) => {
          const oneBased = i + 1;
          return (
            <li key={oneBased} className="group/frame">
              <NumberMarker oneBased={oneBased} active={activeIdx === oneBased} />
              <SlideFrame
                jobId={job.job_id}
                index={oneBased}
                version={versions[i] ?? 1}
                active={activeIdx === oneBased}
                onEdit={handleEditOpen}
                numberLabel={`P · ${String(oneBased).padStart(2, "0")}`}
              />
            </li>
          );
        })}
      </ol>

      <EditPopover
        target={target}
        anchor={anchor}
        busy={busy}
        onSubmit={handleEditSubmit}
        onClose={handleEditClose}
      />
    </section>
  );
}

// ─── Header / numbering / hint ────────────────────────────────────────

function PaneHeader({
  subtitle,
  downloadHref,
}: {
  subtitle: string;
  downloadHref?: string;
}) {
  return (
    <div className="mb-3 flex items-baseline justify-between border-b border-[color:var(--rule)] pb-2">
      <div className="flex items-baseline gap-3">
        <Eye size={11} strokeWidth={1.6} className="translate-y-[1px] text-[color:var(--ink-soft)]" />
        <span className="font-mono-jb text-[10px] uppercase tracking-[0.28em] text-[color:var(--ink)]">
          Live Composite
        </span>
        <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
          {subtitle}
        </span>
      </div>
      {downloadHref ? (
        <a
          href={downloadHref}
          className="group inline-flex items-center gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
        >
          <Download
            size={11}
            strokeWidth={1.6}
            className="translate-y-[1px] transition-transform group-hover:-translate-y-[1px]"
          />
          .pptx
        </a>
      ) : null}
    </div>
  );
}

function HintRow() {
  return (
    <p className="flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
      <Pencil size={10} strokeWidth={1.6} className="translate-y-[1px]" />
      Click any text to revise — Esc closes the editor
    </p>
  );
}

function NumberMarker({ oneBased, active }: { oneBased: number; active: boolean }) {
  return (
    <div className="mb-2 flex items-baseline gap-3">
      <span
        className={clsx(
          "font-mono-jb text-[10px] uppercase tracking-[0.28em] tabular-nums transition-colors",
          active ? "text-[color:var(--vermillion)]" : "text-[color:var(--ink-faint)]",
        )}
      >
        {String(oneBased).padStart(2, "0")}
      </span>
      <span
        className={clsx(
          "h-px flex-1 transition-colors",
          active ? "bg-[color:var(--vermillion)]/40" : "bg-[color:var(--rule)]",
        )}
      />
      {active ? (
        <span className="font-mono-jb text-[9px] uppercase tracking-[0.26em] text-[color:var(--vermillion)]">
          Just revised
        </span>
      ) : null}
    </div>
  );
}

// ─── Skeleton / empty ────────────────────────────────────────────────

function SkeletonStack() {
  // Hardcoded 4 placeholders feels honest — "we're warming the press" —
  // rather than guessing the true slide count too early.
  return (
    <>
      <ol className="space-y-12">
        {[0, 1, 2, 3].map((i) => (
          <li
            key={i}
            className="opacity-0 animate-[phase-in_700ms_ease-out_forwards]"
            style={{ animationDelay: `${i * 110}ms` }}
          >
            <div className="mb-2 flex items-baseline gap-3">
              <span className="font-mono-jb text-[10px] uppercase tracking-[0.28em] tabular-nums text-[color:var(--ink-faint)]">
                {String(i + 1).padStart(2, "0")}
              </span>
              <span className="h-px flex-1 bg-[color:var(--rule)]" />
            </div>
            <div
              className="relative w-full overflow-hidden border border-[color:var(--rule)] bg-white"
              style={{ aspectRatio: "16 / 9" }}
            >
              <div
                aria-hidden
                className="absolute inset-0 bg-gradient-to-r from-transparent via-[color:var(--ink)]/[0.05] to-transparent"
                style={{
                  backgroundSize: "200% 100%",
                  animation: "dw-shimmer 2.4s ease-in-out infinite",
                }}
              />
              <div className="absolute inset-0 flex items-center justify-center">
                <span className="font-mono-jb text-[10px] uppercase tracking-[0.28em] text-[color:var(--ink-faint)]">
                  Awaiting press
                </span>
              </div>
            </div>
          </li>
        ))}
      </ol>
      <style jsx>{`
        @keyframes dw-shimmer {
          0% {
            background-position: 200% 0;
          }
          100% {
            background-position: -200% 0;
          }
        }
      `}</style>
    </>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-start gap-3 border border-dashed border-[color:var(--rule)] bg-[color:var(--paper)]/40 p-8">
      <FileWarning size={16} strokeWidth={1.6} className="text-[color:var(--ink-faint)]" />
      <p className="font-display text-[18px] italic leading-snug text-[color:var(--ink-soft)]">
        No composition was returned this turn.
      </p>
      <p className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
        Try a fresh prompt — restart from index
      </p>
    </div>
  );
}

// ─── Instruction synthesis ────────────────────────────────────────────

// buildEditInstruction turns the popover submission into a natural-
// language Chinese instruction that the agent picks up via /messages.
// "直接改" produces a deterministic instruction that names the field +
// the new text verbatim, so the agent picks edit_slide_text and skips
// the worker LLM. "让 AI 重写" passes the user's instruction through.
function buildEditInstruction(s: EditSubmit): string {
  if (s.mode === "direct") {
    const fieldZh = roleToFieldZh(s.role);
    // Quote the strings with non-Chinese delimiters so the LLM never
    // confuses payload boundaries with the Chinese 「」 characters that
    // might appear inside the body itself.
    return `请使用 edit_slide_text 工具修改第 ${s.slideIndex} 页的 ${fieldZh} 字段：把内容里包含 "${s.oldText}" 的文本改为 "${s.newText}"。不要调用 regenerate_slide。`;
  }
  return `请把第 ${s.slideIndex} 页按照下面这条指示重写（使用 regenerate_slide 工具）：${s.instruction}`;
}

function roleToFieldZh(role: string): string {
  switch (role) {
    case "title":
      return "标题(title)";
    case "subtitle":
      return "副标题(subtitle)";
    case "bullet":
      return "要点(bullets)";
    case "quote":
      return "引言(quote)";
    case "metric":
      return "数据(metric)";
    case "footer":
      return "页脚(footer)";
    default:
      return "正文文本(body)";
  }
}
