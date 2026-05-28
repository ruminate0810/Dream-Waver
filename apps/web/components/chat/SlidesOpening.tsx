"use client";

import { ArrowDownToLine, ArrowUpRight, RotateCcw } from "lucide-react";

import type { SlideJob } from "@/lib/api";
import { BusyHint } from "./BusyHint";
import { Phase, PressTicker, type SectionStatus } from "./Bubble";
import { PreviewGrid } from "../slides-preview/PreviewGrid";
import { ThoughtCollapse } from "./ThoughtCollapse";
import { ToolStrip } from "./ToolStrip";
import { ErrorCallout } from "../ui/ErrorCallout";

import type { Turn } from "./session";
import {
  derivePhaseStates,
  SLIDES_PHASES,
  type PhaseKey,
  type PhaseState,
} from "./skills/slides";

// SlidesOpening renders Turn 0 — the initial-generation chapter — using
// the slides skill's editorial 4-phase chrome (composition / prose /
// press / issue). Every other turn (follow-up edits) renders through
// <EditTurnTrace>, which is uniform regardless of which tool ran.
//
// This component is intentionally read-only on the Turn — it takes the
// already-folded session state and the SlideJob for download/preview
// links, and produces the magazine-spread visual.

export function SlidesOpening({
  turn,
  job,
  previewVersion,
  compact = false,
}: {
  turn: Turn | undefined;
  job: SlideJob;
  previewVersion: number;
  compact?: boolean;
}) {
  const phases = derivePhaseStates(turn);
  // Pick the first phase still pending — that's where the busyHint
  // belongs. Without this, both `prose` and `press` would each
  // render the chip during the post-outline-approval gap.
  const firstPendingKey =
    turn?.busyHint
      ? phases.find((p) => p.status === "pending" && p.key !== "issue")?.key
      : undefined;

  return (
    <>
      {phases.map((phase) => {
        // The issue phase only renders once it's actually done — before
        // then it's an empty placeholder that would add visual noise.
        if (phase.key === "issue" && phase.status !== "done") return null;

        return (
          <Phase
            key={phase.key}
            index={phase.kicker}
            status={toSectionStatus(phase.status)}
            kicker={phase.kicker}
            zhTitle={phase.zhTitle}
            enSubtitle={phase.enSubtitle}
            emphasis={phase.key === "issue"}
          >
            <PhaseBody
              phase={phase}
              turn={turn}
              job={job}
              previewVersion={previewVersion}
              compact={compact}
              showBusy={phase.key === firstPendingKey}
            />
            <ToolStrip calls={phase.toolCalls} />
            <ThoughtCollapse thoughts={phase.thoughts} />
          </Phase>
        );
      })}
    </>
  );
}

// ─── Per-phase body content ───────────────────────────────────────────

function PhaseBody({
  phase,
  turn,
  job,
  previewVersion,
  compact,
  showBusy,
}: {
  phase: PhaseState;
  turn: Turn | undefined;
  job: SlideJob;
  previewVersion: number;
  compact: boolean;
  showBusy: boolean;
}) {
  if (phase.status === "error") {
    const msg = turn?.errorMsg ?? job.error ?? "Unknown failure.";
    const topic = (job.input?.topic || job.title || "").slice(0, 240);
    // Sprint AF.3 — replaced the inline error banner with shared
    // <ErrorCallout>. Initial-gen failures can't replay the same job
    // (deck rows aren't restartable from a Phase-3 crash mid-job);
    // best we can do is hand the topic back to /slides/new prefilled.
    return (
      <div className="mt-2 mb-1">
        <ErrorCallout
          message={msg}
          action={
            topic
              ? {
                  label: "新建相似 deck",
                  href: `/slides/new?topic=${encodeURIComponent(topic)}`,
                }
              : undefined
          }
        />
      </div>
    );
  }

  return renderForPhase(phase.key, phase.status, turn, job, previewVersion, compact, showBusy);
}

function renderForPhase(
  key: PhaseKey,
  status: PhaseState["status"],
  turn: Turn | undefined,
  job: SlideJob,
  previewVersion: number,
  compact: boolean,
  showBusy: boolean,
) {
  switch (key) {
    case "composition":
      if (status === "running" || status === "pending") {
        return (
          <p className="font-display italic">
            Analysing topic, deciding chapters &amp; rhythm…
          </p>
        );
      }
      return (
        <div className="space-y-3">
          {turn?.outlineTitle && (
            <p>
              <span className="font-display italic text-[color:var(--ink)]">
                &ldquo;{turn.outlineTitle}&rdquo;
              </span>
            </p>
          )}
          {turn?.outlineSlideCount ? (
            <p className="font-mono-jb text-[11px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
              {turn.outlineSlideCount} chapters set
            </p>
          ) : null}
        </div>
      );

    case "prose":
      if (status === "pending") {
        // Phase is still pending (no write_content tool.start yet) but
        // a gate was just submitted — surface the busy chip so the
        // user immediately sees "正在准备" instead of a frozen strip
        // for the 1-3s before Phase 3 actually fires. Only the first
        // pending phase shows it (outer loop computes `showBusy`).
        if (showBusy && turn?.busyHint) {
          return <BusyHint kind={turn.busyHint.kind} />;
        }
        return null;
      }
      if (status === "running") {
        return (
          <p className="font-display italic">
            Drafting each page from the outline…
          </p>
        );
      }
      return (
        <p className="font-mono-jb text-[11px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
          Manuscript ready
        </p>
      );

    case "press":
      if (status === "pending") {
        if (showBusy && turn?.busyHint) {
          return <BusyHint kind={turn.busyHint.kind} />;
        }
        return null;
      }
      return (
        <PressTicker
          now={turn?.slidesRendered ?? 0}
          total={turn?.slidesTotal || job.slide_count || 1}
        />
      );

    case "issue":
      return (
        <div className="mt-2">
          {/* In compact mode the right-pane <LivePreviewStack> shows the
              live editable deck; the PNG thumbnail grid would just
              duplicate that surface. */}
          {!compact && job.preview_urls?.length ? (
            <PreviewGrid urls={job.preview_urls} cacheBust={previewVersion} />
          ) : null}
          <Colophon job={job} />
        </div>
      );
  }
}

// ─── Colophon ────────────────────────────────────────────────────────

function Colophon({ job }: { job: SlideJob }) {
  return (
    <footer className="mt-10 flex flex-wrap items-center gap-x-3 gap-y-3 border-t border-[color:var(--rule)] pt-6">
      {job.download_url && (
        <a
          href={job.download_url}
          className="group inline-flex items-center gap-2.5 bg-[color:var(--ink)] px-5 py-3 text-[color:var(--paper)] transition-colors hover:bg-[color:var(--vermillion)]"
        >
          <span className="font-mono-jb text-[10px] font-medium uppercase tracking-[0.22em]">
            Download .pptx
          </span>
          <ArrowDownToLine
            size={14}
            strokeWidth={1.6}
            className="transition-transform group-hover:translate-y-0.5"
          />
        </a>
      )}
      <a
        href="/slides/new"
        className="group inline-flex items-center gap-2 border border-[color:var(--rule)] px-5 py-3 text-[color:var(--ink)] transition-colors hover:border-[color:var(--ink)]/30 hover:bg-white/60"
      >
        <RotateCcw size={13} strokeWidth={1.6} />
        <span className="font-mono-jb text-[10px] font-medium uppercase tracking-[0.22em]">
          New Edition
        </span>
      </a>
      <a
        href="/"
        className="ml-auto inline-flex items-center gap-1.5 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)] transition-colors hover:text-[color:var(--ink)]"
      >
        <span>Index</span>
        <ArrowUpRight size={11} strokeWidth={1.6} />
      </a>
    </footer>
  );
}

// ─── Helpers ──────────────────────────────────────────────────────────

function toSectionStatus(s: PhaseState["status"]): SectionStatus {
  switch (s) {
    case "pending":
      return "pending";
    case "running":
      return "running";
    case "done":
      return "done";
    case "error":
      return "error";
  }
}

// Silence unused-export warning by re-exporting SLIDES_PHASES — Chat or
// other call sites might use it for sticky-nav / breadcrumb purposes.
export { SLIDES_PHASES };
