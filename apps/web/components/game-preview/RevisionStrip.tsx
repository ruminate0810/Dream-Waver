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
        "flex items-center gap-2 overflow-x-auto border-x-2 border-ink bg-surface-2 px-3",
        compact ? "py-1.5" : "py-2",
      )}
    >
      <span className="shrink-0 font-pixel text-[0.55rem] tracking-wide text-muted">
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
              "group inline-flex shrink-0 items-center gap-1.5 rounded-pixel border-2 px-2.5 py-1 transition-colors",
              isActive
                ? "border-ink bg-accent-soft text-ink shadow-pixel-sm"
                : "border-line-2 text-ink-2 hover:border-ink hover:text-ink",
            )}
          >
            <span className="font-mono text-[10px] font-semibold uppercase tracking-wide">
              v{r.idx}
            </span>
            {isHead ? (
              <span className="font-pixel text-[0.5rem] uppercase tracking-wide opacity-70">
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
            "ml-auto inline-flex shrink-0 items-center gap-1.5 rounded-pixel border-2 border-ink bg-accent px-2.5 py-1 text-white shadow-pixel-sm transition-transform",
            restoring !== null
              ? "cursor-not-allowed opacity-50"
              : "hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none",
          )}
          title={`Fork future edits from v${selectedIdx}`}
        >
          {restoring === selectedIdx ? (
            <Loader2 size={11} strokeWidth={2} className="animate-spin" />
          ) : (
            <RotateCcw size={11} strokeWidth={2} />
          )}
          <span className="font-mono text-[10px] font-semibold uppercase tracking-wide">
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
