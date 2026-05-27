"use client";

import { useCallback, useEffect, useRef, useState } from "react";
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
  // Only mount iframes when the backend will actually have slide HTML
  // to serve. Sprint U follow-up — previously this was
  //   `status !== "running" || slideCount > 0`
  // which evaluated TRUE during awaiting_outline_approval (status is
  // not "running") and made 8 iframes try to GET /page/N.html before
  // Phase 3 had built any deck. Backend then returned the JSON
  // envelope `{"error":"deck not ready","ok":false}` and the iframes
  // displayed it as raw text. Mount conditions now:
  //   - finished           → all pages exist
  //   - running + count>0  → Phase 3 in progress; pages are arriving
  // Everything else (awaiting_wizard / awaiting_clarification /
  // awaiting_outline_approval / error) defers mount; the gate cards
  // on the left column are the only thing the user should see.
  const ready =
    job.status === "finished" ||
    (job.status === "running" && slideCount > 0);

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
  useEffect(() => {
    return stream.subscribe((ev) => {
      if (ev.kind !== "slides.updated") return;
      const idx = ev.data.slide_index;
      if (typeof idx !== "number") return;

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

  const handleEditOpen = useCallback((req: EditRequest, anchorRect: DOMRect) => {
    setTarget({ slideIndex: req.slideIndex, text: req.text, role: req.role });
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
          const isActive = activeSet.has(oneBased);
          return (
            <li key={oneBased} className="group/frame">
              <NumberMarker oneBased={oneBased} active={isActive} />
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
