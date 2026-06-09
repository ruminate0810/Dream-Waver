"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Download, Eye, FileDown, FileWarning, Pencil, Presentation } from "lucide-react";
import clsx from "clsx";

import { exportPdfURL, postSlideMessage, presentURL, type SlideJob } from "@/lib/api";
import { useAgentEventStream } from "@/components/chat/transport";
import { SlideFrame, type EditRequest } from "./SlideFrame";
import { EditPopover, type EditSubmit, type EditTarget } from "./EditPopover";
import { SlideToolbar } from "./SlideToolbar";

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
  // Only mount iframes when the backend will actually have slide HTML
  // to serve. Sprint U follow-up + Sprint Z.next — previously this was
  //   `status === "finished" || (status === "running" && count > 0)`
  // which still let us mount 12 iframes the moment Phase 3 (content +
  // render) started — but those page/N.html endpoints don't exist yet
  // until each slide ACTUALLY renders. The result was a 12 × 404
  // storm in the network panel + iframe error fallbacks blinking up.
  //
  // New gate: track the highest rendered slide index via slides.content
  // events (renderedMax below). Only mount frames 1..renderedMax during
  // the content phase; full slideCount when status=finished. The
  // unrendered tail uses the SkeletonStack chrome from below so the
  // user still sees the eventual deck shape.
  const ready =
    job.status === "finished" ||
    (job.status === "running" && slideCount > 0);
  // renderedMax — the highest 1-based slide index that has fired a
  // slides.content event. Updates as Phase 3 progresses, page-by-
  // page. Starts at slideCount when finished (in case we landed on
  // an already-done deck and missed the events).
  const [renderedMax, setRenderedMax] = useState<number>(
    job.status === "finished" ? slideCount : 0,
  );
  // When the deck flips to finished (via polling fallback OR
  // agent.finish event) trust that ALL slides are now servable.
  useEffect(() => {
    if (job.status === "finished") {
      setRenderedMax((m) => Math.max(m, slideCount));
    }
  }, [job.status, slideCount]);
  // Effective count = how many iframes we actually mount this render.
  // Capped at renderedMax during content phase to avoid 404 storms.
  const effectiveCount =
    job.status === "finished" ? slideCount : Math.min(slideCount, renderedMax);

  // Per-slide version counter. Each bump force-remounts that one iframe,
  // re-fetching the live HTML. We start everyone at 1 so the URL has a
  // version segment from the very first paint.
  const [versions, setVersions] = useState<number[]>(() =>
    Array.from({ length: Math.max(slideCount, 0) }, () => 1),
  );
  // activeSet — every 1-based index currently showing the vermillion
  // "just revised" pulse. Plural (not single index) so batch updates
  // can light multiple frames at once as the wave rolls through.
  const [activeSet, setActiveSet] = useState<Set<number>>(() => new Set());
  // focusTicks — bumping focusTicks[i] tells SlideFrame i to scrollIntoView.
  // We only bump for SINGLE-slide updates (not batch refreshes for theme/brand).
  const [focusTicks, setFocusTicks] = useState<Record<number, number>>({});
  // clearTicks / successTicks — bump → SlideFrame posts the matching
  // message to its iframe contentWindow to clear or success-flash the
  // .__dw-active element (the one the user clicked to edit).
  const [clearTicks, setClearTicks] = useState<Record<number, number>>({});
  const [successTicks, setSuccessTicks] = useState<Record<number, number>>({});

  // Rolling window of recent slides.updated arrivals. Used to detect
  // batch refreshes (change_theme / apply_brand fire 5 events in
  // milliseconds) vs single edits (edit_slide_text fires one event).
  const recentRef = useRef<{ at: number; idx: number }[]>([]);
  // Pending submission target — set when EditPopover is open and the
  // user has submitted; cleared when the matching slides.updated event
  // arrives (or after a 6s timeout). This is the thing that makes the
  // popover stay open during the agent round-trip.
  const pendingRef = useRef<{ slideIndex: number; postedAt: number } | null>(null);

  // Grow / shrink the versions array if the deck changes shape between
  // edits (delete_slide drops one, add_slide grows it). Keep existing
  // versions stable to avoid spurious reloads.
  const prevCountRef = useRef<number>(slideCount);
  useEffect(() => {
    const prev = prevCountRef.current;
    if (slideCount !== prev) {
      prevCountRef.current = slideCount;
      // A structural op that changed the deck size (add/delete/duplicate)
      // landed — release the toolbars.
      setMgmtBusy(false);
      // The deck grew → scroll-focus the new last slide so the user
      // immediately sees the addition. The deck shrunk → scroll-focus
      // whichever slide now sits at the deleted position so the user
      // sees what filled the gap. Both bump focusTicks for ONE slide.
      if (slideCount > prev && slideCount > 0) {
        setFocusTicks((t) => ({ ...t, [slideCount]: (t[slideCount] ?? 0) + 1 }));
      } else if (slideCount < prev && slideCount > 0) {
        // The deleted slide was somewhere in 1..prev; we don't know
        // which exactly without inspecting events. Best heuristic:
        // scroll to the index that previously held the deleted item.
        // Without that info, scroll to the LAST surviving slide.
        setFocusTicks((t) => ({ ...t, [slideCount]: (t[slideCount] ?? 0) + 1 }));
      }
    }
    setVersions((prevVersions) => {
      if (prevVersions.length === slideCount) return prevVersions;
      const next = prevVersions.slice(0, slideCount);
      while (next.length < slideCount) next.push(1);
      return next;
    });
  }, [slideCount]);

  // Subscribe to the shared event stream. We process slides.updated
  // through a small dispatcher that classifies the event as single
  // vs batch, then bumps versions + sets active highlight + optionally
  // bumps focusTicks.
  const stream = useAgentEventStream();
  // Sprint Z.next — listen for slides.content events (per-slide
  // render completion during Phase 3) and bump renderedMax. This
  // is what gates the iframe mount loop below — we only mount up
  // to the slide index that's actually been rendered, instead of
  // optimistically mounting all 12 and watching them 404.
  useEffect(() => {
    return stream.subscribe((ev) => {
      if (ev.kind !== "slides.content") return;
      const idx = ev.data.slide_index;
      if (typeof idx === "number" && idx > 0) {
        setRenderedMax((m) => Math.max(m, idx));
      }
    });
  }, [stream]);
  useEffect(() => {
    return stream.subscribe((ev) => {
      if (ev.kind !== "slides.updated") return;
      const idx = ev.data.slide_index;
      if (typeof idx !== "number") return;
      // slides.updated also implies the slide is now servable —
      // belt + braces in case slides.content arrived first but
      // somehow missed by the listener (HMR / stream replay).
      if (idx > 0) {
        setRenderedMax((m) => Math.max(m, idx));
      }
      // A structural op (reorder keeps slide_count, so the count effect
      // below won't fire) just landed — release the per-slide toolbars.
      setMgmtBusy(false);

      const now = Date.now();
      // Slide window: keep only events within the last 250ms — the
      // event budget for a batch refresh (chromedp emits each
      // slide.updated as soon as that chromedp pass completes; for
      // bulk updates these arrive within 50-150ms of each other).
      recentRef.current = recentRef.current.filter((e) => now - e.at < 250);
      recentRef.current.push({ at: now, idx });
      const batchSize = recentRef.current.length;
      const isBatch = batchSize > 2;

      // Bump version for this slide. For batch, we stagger via a
      // setTimeout so the wave reads as a sequence of pulses rather
      // than a single simultaneous flash. The window above resets per
      // event, so 5 quick events naturally space themselves to ~80ms
      // intervals on the screen even though they arrive ~simultaneously.
      const delay = isBatch ? (batchSize - 1) * 80 : 0;
      setTimeout(() => {
        setVersions((prev) => {
          const next = prev.slice();
          const i = idx - 1;
          if (i >= 0 && i < next.length) next[i] = (next[i] ?? 1) + 1;
          return next;
        });
        setActiveSet((prev) => {
          const next = new Set(prev);
          next.add(idx);
          return next;
        });
        // Clear active after pulse animation finishes.
        setTimeout(() => {
          setActiveSet((prev) => {
            const next = new Set(prev);
            next.delete(idx);
            return next;
          });
        }, 1500);
      }, delay);

      // Scroll only for single-slide edits. Batch refreshes
      // (change_theme / apply_brand) shouldn't yank the viewport.
      if (!isBatch) {
        setFocusTicks((t) => ({ ...t, [idx]: (t[idx] ?? 0) + 1 }));
      }

      // If a popover submission is waiting on this slide's update,
      // flash the clicked element green via dw-edit-success then
      // close the popover.
      const pending = pendingRef.current;
      if (pending && pending.slideIndex === idx) {
        setSuccessTicks((t) => ({ ...t, [idx]: (t[idx] ?? 0) + 1 }));
        pendingRef.current = null;
        // Delay popover close so the user sees the success state.
        setTimeout(() => {
          setBusy(false);
          setTarget(null);
          setAnchor(null);
        }, 750);
      }
    });
  }, [stream]);

  // ────────── Edit popover state ──────────
  const [target, setTarget] = useState<EditTarget | null>(null);
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const [busy, setBusy] = useState(false);
  // Sprint I0.3 — surface the last edit error so the popover can render
  // it inline + the user can click submit to retry. Cleared on submit
  // start and on close.
  const [editError, setEditError] = useState<string | null>(null);
  // Sprint AG.1b — structural slide-management (add/duplicate/reorder/delete)
  // posts a deterministic instruction naming the exact tool; the existing
  // slides.updated / slide_count machinery then re-renders the deck. One global
  // flag disables every per-slide toolbar while an op is in flight — we never
  // want two concurrent structural edits racing on the deck shape.
  const [mgmtBusy, setMgmtBusy] = useState(false);

  const handleEditOpen = useCallback((req: EditRequest, anchorRect: DOMRect) => {
    setTarget({
      slideIndex: req.slideIndex,
      text: req.text,
      role: req.role,
      style: req.style ?? null,
    });
    setAnchor(anchorRect);
  }, []);
  const handleEditClose = useCallback(() => {
    if (busy) return; // don't close while the agent is mid-edit
    // Cleanly release the iframe's __dw-active highlight on the
    // clicked element — without this, the orange dashed outline
    // persists until the next click anywhere in that iframe.
    const idx = target?.slideIndex;
    if (idx) setClearTicks((t) => ({ ...t, [idx]: (t[idx] ?? 0) + 1 }));
    setTarget(null);
    setAnchor(null);
    setEditError(null);
  }, [busy, target]);

  const handleEditSubmit = useCallback(
    async (s: EditSubmit) => {
      const message = buildEditInstruction(s);
      try {
        setBusy(true);
        setEditError(null); // clear any prior failure on a fresh attempt
        // Mark this slide as pending — the slides.updated handler
        // above will detect the match, flash the clicked element
        // green via dw-edit-success, and only THEN close the popover.
        pendingRef.current = { slideIndex: s.slideIndex, postedAt: Date.now() };
        await postSlideMessage(job.job_id, message);
        // Failsafe: if no slides.updated arrives within 12s (LLM stuck,
        // ws drop, etc.) force-close so the user isn't trapped.
        setTimeout(() => {
          const p = pendingRef.current;
          if (p && p.slideIndex === s.slideIndex) {
            pendingRef.current = null;
            setBusy(false);
            // Don't close — let the user see what happened (still
            // editing) but flip busy off so they can retry / cancel.
            setEditError("agent 12s 没回响。可以再点「重试」试试。");
          }
        }, 12_000);
      } catch (e) {
        // POST itself failed — back out the optimistic busy state and
        // surface the error inside the popover so the user can retry
        // without re-typing (Sprint I0.3).
        pendingRef.current = null;
        setBusy(false);
        setEditError(e instanceof Error ? e.message : "提交失败，请重试");
      }
    },
    [job.job_id],
  );

  // runMgmt posts a structural-edit instruction and arms the global busy
  // flag; the slides.updated / slide_count effects above clear it once the
  // deck reflects the change. The 10s failsafe covers a dropped event.
  const runMgmt = useCallback(
    async (instruction: string) => {
      try {
        setMgmtBusy(true);
        await postSlideMessage(job.job_id, instruction);
      } catch {
        setMgmtBusy(false);
        return;
      }
      setTimeout(() => setMgmtBusy(false), 10_000);
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

  // Sprint Z.next — during content phase show "rendered/total" so
  // the user knows the deck is still typesetting rather than thinking
  // every slide is broken because effectiveCount < slideCount.
  const composing = effectiveCount < slideCount && job.status === "running";
  // The structural toolbar only makes sense on a finished deck — restructuring
  // mid-generation would race the render pipeline.
  const finished = job.status === "finished";
  const subtitleText = composing
    ? `${effectiveCount}/${slideCount} slides · composing…`
    : `${slideCount} slide${slideCount === 1 ? "" : "s"}`;

  return (
    <section>
      <div className="sticky top-0 z-10 -mx-2 bg-paper/85 px-2 pb-3 pt-1 backdrop-blur-sm">
        <PaneHeader
          subtitle={subtitleText}
          jobId={job.job_id}
          downloadHref={job.download_url ?? undefined}
        />
        <HintRow />
      </div>

      <ol className="space-y-12">
        {Array.from({ length: effectiveCount }).map((_, i) => {
          const oneBased = i + 1;
          const isActive = activeSet.has(oneBased);
          // Sprint Z.4 — staggered phase-in on mount. The first 6
          // frames cascade in (40ms apart, total ~200ms) so the
          // initial deck reveal reads as a printing-press sequence
          // rather than a wall slamming into place. Late-arriving
          // frames (slide_count growing from N → N+1 mid-edit) get
          // 0 delay so they animate alone, immediately.
          const staggerDelay = `${Math.min(i, 5) * 40}ms`;
          return (
            <li
              key={oneBased}
              className="group/frame animate-phase-in"
              style={{ animationDelay: staggerDelay }}
            >
              <NumberMarker oneBased={oneBased} active={isActive} />
              <div className="relative">
                {finished ? (
                  <SlideToolbar
                    oneBased={oneBased}
                    total={slideCount}
                    busy={mgmtBusy}
                    onAdd={() => runMgmt(addAfterInstruction(oneBased))}
                    onDuplicate={() => runMgmt(duplicateInstruction(oneBased))}
                    onMoveUp={() => runMgmt(moveInstruction(oneBased, oneBased - 1))}
                    onMoveDown={() => runMgmt(moveInstruction(oneBased, oneBased + 1))}
                    onDelete={() => runMgmt(deleteInstruction(oneBased))}
                  />
                ) : null}
                <SlideFrame
                  jobId={job.job_id}
                  index={oneBased}
                  version={versions[i] ?? 1}
                  active={isActive}
                  focusTick={focusTicks[oneBased]}
                  clearActiveTick={clearTicks[oneBased]}
                  successTick={successTicks[oneBased]}
                  onEdit={handleEditOpen}
                  numberLabel={`P · ${String(oneBased).padStart(2, "0")}`}
                />
              </div>
            </li>
          );
        })}
      </ol>

      <EditPopover
        target={target}
        anchor={anchor}
        busy={busy}
        error={editError}
        onSubmit={handleEditSubmit}
        onClose={handleEditClose}
      />
    </section>
  );
}

// ─── Header / numbering / hint ────────────────────────────────────────

// Shared style for the secondary deck actions (演示 / PDF). Quiet mono caps in
// the pixel theme's palette so they sit beside the primary .pptx button without
// competing with it. font-mono (not font-pixel) keeps the CJK "演示" legible.
const DECK_ACTION_CLS =
  "group inline-flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-[0.2em] text-muted transition-colors hover:text-ink";

function PaneHeader({
  subtitle,
  jobId,
  downloadHref,
}: {
  subtitle: string;
  jobId?: string;
  downloadHref?: string;
}) {
  // The action cluster only appears once the deck is servable. `downloadHref`
  // is set when status=finished, so its presence (plus a jobId) is our gate —
  // 演示 + PDF endpoints are only meaningful on a finished deck too.
  const showActions = Boolean(downloadHref && jobId);
  return (
    <div className="mb-3 flex items-baseline justify-between border-b border-line pb-2">
      <div className="flex items-baseline gap-3">
        <Eye size={11} strokeWidth={1.6} className="translate-y-[1px] text-ink-2" />
        <span className="font-pixel text-[0.55rem] tracking-wide text-ink">
          Live Composite
        </span>
        <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
          {subtitle}
        </span>
      </div>
      {showActions ? (
        <div className="flex items-center gap-4">
          <a
            href={presentURL(jobId!)}
            target="_blank"
            rel="noopener noreferrer"
            className={DECK_ACTION_CLS}
            title="全屏演示这套幻灯片（新标签页打开）"
          >
            <Presentation
              size={11}
              strokeWidth={1.6}
              className="translate-y-[1px] transition-transform group-hover:-translate-y-[1px]"
            />
            演示
          </a>
          <a href={exportPdfURL(jobId!)} className={DECK_ACTION_CLS} title="导出整套 PDF">
            <FileDown
              size={11}
              strokeWidth={1.6}
              className="translate-y-[1px] transition-transform group-hover:-translate-y-[1px]"
            />
            PDF
          </a>
          <a
            href={downloadHref}
            title="下载可编辑 .pptx"
            className="group inline-flex items-center gap-2 rounded-pixel border-2 border-ink bg-surface px-2.5 py-1 font-mono text-[11px] font-semibold text-ink shadow-pixel-sm transition-transform duration-100 hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none"
          >
            <Download size={11} strokeWidth={1.8} className="translate-y-[1px]" />
            .pptx
          </a>
        </div>
      ) : null}
    </div>
  );
}

function HintRow() {
  return (
    <p className="flex items-baseline gap-2 font-mono text-[10px] font-semibold tracking-wide text-muted">
      <Pencil size={10} strokeWidth={1.8} className="translate-y-[1px]" />
      Click any text to revise — Esc closes the editor
    </p>
  );
}

function NumberMarker({ oneBased, active }: { oneBased: number; active: boolean }) {
  return (
    <div className="mb-2 flex items-baseline gap-3">
      <span
        className={clsx(
          "font-pixel text-[0.55rem] tracking-wide tabular-nums transition-colors",
          active ? "text-accent" : "text-muted",
        )}
      >
        {String(oneBased).padStart(2, "0")}
      </span>
      <span
        className={clsx(
          "h-px flex-1 transition-colors",
          active ? "bg-accent/40" : "bg-line",
        )}
      />
      {active ? (
        <span className="font-pixel text-[0.5rem] tracking-wide text-accent">
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
              <span className="font-pixel text-[0.55rem] tracking-wide tabular-nums text-muted">
                {String(i + 1).padStart(2, "0")}
              </span>
              <span className="h-px flex-1 bg-line" />
            </div>
            <div className="rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm">
              <div className="flex items-center gap-2 border-b border-line bg-surface-2 px-3 py-1.5">
                <span aria-hidden className="flex flex-none gap-1.5">
                  <i className="h-[10px] w-[10px] rounded-full border border-ink bg-[#ff8a8a]" />
                  <i className="h-[10px] w-[10px] rounded-full border border-ink bg-gold" />
                  <i className="h-[10px] w-[10px] rounded-full border border-ink bg-grass" />
                </span>
                <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
                  P {String(i + 1).padStart(2, "0")}
                </span>
              </div>
              <div
                className="relative w-full overflow-hidden bg-surface"
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
                  <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
                    Awaiting press
                  </span>
                </div>
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
    <div className="flex flex-col items-start gap-3 rounded-pixel border-2 border-ink bg-surface p-8 shadow-pixel">
      <FileWarning size={16} strokeWidth={1.8} className="text-muted" />
      <p className="font-mono text-[18px] font-bold tracking-tight leading-snug text-ink-2">
        No composition was returned this turn.
      </p>
      <p className="font-pixel text-[0.55rem] tracking-wide text-muted">
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
  if (s.mode === "style") {
    // Deterministic per-element restyle (Sprint AG.1c) → style_svg_element.
    const parts: string[] = [];
    if (s.fill) parts.push(`fill 设为 "${s.fill}"`);
    if (typeof s.fontSize === "number") parts.push(`font_size 设为 ${s.fontSize}`);
    if (s.fontWeight) parts.push(`font_weight 设为 "${s.fontWeight}"`);
    return `请使用 style_svg_element 工具修改第 ${s.slideIndex} 页：定位文本 "${s.matchText}"（第 ${s.occurrence} 处出现），把 ${parts.join("，")}。只调用 style_svg_element，不要调用 edit_svg_slide 或其他工具。`;
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

// ─── Structural-edit instruction synthesis (Sprint AG.1b) ─────────────
//
// Each toolbar action posts a deterministic instruction that names the exact
// tool + its 1-based positions, so the agent executes the structural edit
// without any LLM reasoning about which tool to pick. Param vocabulary matches
// each tool's schema (add_slide: position/instruction; delete_slide: slide_index;
// reorder_slide: from_position/to_position; duplicate_slide: slide_index).

function addAfterInstruction(oneBased: number): string {
  return `请使用 add_slide 工具在第 ${oneBased} 页之后新增一页（position=${oneBased + 1}，instruction="延续本页主题，自然承接展开"）。只调用 add_slide，不要调用其他工具。`;
}

function duplicateInstruction(oneBased: number): string {
  return `请使用 duplicate_slide 工具复制第 ${oneBased} 页（slide_index=${oneBased}）。只调用 duplicate_slide，不要调用其他工具。`;
}

function moveInstruction(from: number, to: number): string {
  return `请使用 reorder_slide 工具把第 ${from} 页移动到第 ${to} 页的位置（from_position=${from}, to_position=${to}）。只调用 reorder_slide，不要调用其他工具。`;
}

function deleteInstruction(oneBased: number): string {
  return `请使用 delete_slide 工具删除第 ${oneBased} 页（slide_index=${oneBased}）。只调用 delete_slide，不要调用其他工具。`;
}
