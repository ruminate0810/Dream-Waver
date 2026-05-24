"use client";

import { useEffect, useState } from "react";
import { RotateCcw, Loader2 } from "lucide-react";
import clsx from "clsx";

import {
  listGameRevisions,
  restoreGameRevision,
  type GameRevision,
} from "@/lib/api";

// RevisionStrip renders the session's immutable history as a horizontal
// row of pills, one per saved generation. The pill labelled with the
// highest idx is "head" — what edits land on by default.
//
// Click a non-head pill → switch GameFrame's iframe to that revision's
// /play URL (read-only preview). Click "Restore" on a non-head pill →
// POST /restore; the page lifts onRestored() to re-fetch revisions and
// snap the preview back to that point so the next edit forks from there.

type Props = {
  jobId: string;
  /** Bumped by the parent each time job.finished_at changes so we refresh. */
  refreshKey: unknown;
  /** Which revision the iframe is currently displaying (head if undefined). */
  selectedIdx: number | undefined;
  /** Fired when the user picks a revision to preview. */
  onSelect: (idx: number | undefined) => void;
  /** Fired after a successful restore lands. */
  onRestored: (idx: number) => void;
  /** Render in a compact mode when stacked above the iframe. */
  compact?: boolean;
};

export function RevisionStrip({
  jobId,
  refreshKey,
  selectedIdx,
  onSelect,
  onRestored,
  compact,
}: Props) {
  const [revs, setRevs] = useState<GameRevision[]>([]);
  const [loading, setLoading] = useState(false);
  const [restoring, setRestoring] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listGameRevisions(jobId)
      .then((rs) => {
        if (!cancelled) setRevs(rs);
      })
      .catch(() => {
        /* swallow — the parent already surfaces job errors */
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [jobId, refreshKey]);

  if (revs.length === 0 && !loading) return null;

  const headIdx = revs.length - 1;
  const viewing = selectedIdx ?? headIdx;

  const handleRestore = async (idx: number) => {
    if (restoring !== null) return;
    setRestoring(idx);
    try {
      await restoreGameRevision(jobId, idx);
      onRestored(idx);
    } catch {
      // Errors come back through the events stream; nothing to do here.
    } finally {
      setRestoring(null);
    }
  };

  return (
    <div
      className={clsx(
        "flex items-center gap-2 overflow-x-auto border-x border-[color:var(--rule)] bg-white/60 px-3 backdrop-blur-[1px]",
        compact ? "py-1.5" : "py-2",
      )}
    >
      <span className="shrink-0 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
        Versions
      </span>
      {revs.map((r) => {
        const isHead = r.idx === headIdx;
        const isActive = r.idx === viewing;
        return (
          <button
            key={r.idx}
            type="button"
            title={`${r.title || "Untitled"} — ${r.description || "no description"} (${r.bytes.toLocaleString()} bytes, ${formatTime(r.at)})`}
            onClick={() => onSelect(isHead ? undefined : r.idx)}
            className={clsx(
              "group inline-flex shrink-0 items-center gap-1.5 border px-2.5 py-1 transition-colors",
              isActive
                ? "border-[color:var(--ink)] bg-[color:var(--ink)] text-[color:var(--paper)]"
                : "border-[color:var(--rule)] text-[color:var(--ink-soft)] hover:border-[color:var(--ink)]/40 hover:text-[color:var(--ink)]",
            )}
          >
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em]">
              v{r.idx}
            </span>
            {isHead ? (
              <span className="font-mono-jb text-[9px] uppercase tracking-[0.22em] opacity-70">
                head
              </span>
            ) : null}
          </button>
        );
      })}
      {selectedIdx !== undefined && selectedIdx !== headIdx ? (
        <button
          type="button"
          onClick={() => handleRestore(selectedIdx)}
          disabled={restoring !== null}
          className={clsx(
            "ml-auto inline-flex shrink-0 items-center gap-1.5 border border-[color:var(--vermillion)]/60 px-2.5 py-1 text-[color:var(--vermillion)] transition-colors",
            restoring !== null
              ? "cursor-not-allowed opacity-50"
              : "hover:bg-[color:var(--vermillion)] hover:text-[color:var(--paper)]",
          )}
          title={`Fork future edits from v${selectedIdx}`}
        >
          {restoring === selectedIdx ? (
            <Loader2 size={11} strokeWidth={2} className="animate-spin" />
          ) : (
            <RotateCcw size={11} strokeWidth={2} />
          )}
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em]">
            Restore v{selectedIdx}
          </span>
        </button>
      ) : null}
    </div>
  );
}

function formatTime(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  } catch {
    return iso;
  }
}
