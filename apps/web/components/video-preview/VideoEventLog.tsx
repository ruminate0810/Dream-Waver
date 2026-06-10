"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2, Check, AlertTriangle, RotateCcw } from "lucide-react";

import type { VideoTimeline } from "@/lib/api";

// VideoEventLog is the left-pane "chat" surface of the video workspace.
//
// Strictly speaking this is *not* a chat — the Opendream pipeline isn't
// conversational and there are no LLM thoughts to stream. But the slides
// workspace establishes a strong two-column rhythm (events left,
// artifact right) and the user expects it; collapsing to a single
// timeline column would feel barren.
//
// What we surface here:
//   - Run lifecycle: created → running → finished/error
//   - Per-node transitions: any time a node flips state we add a row
//   - Errors: bubbled up with the node key for quick mental indexing
//
// The parent passes us the latest timeline snapshot; we diff against
// our own previous snapshot to compute the new "log lines". This is
// cheap (O(nodes) per update) and avoids leaking diff machinery into
// VideoTimeline.

export type VideoEventLogProps = {
  /** Latest timeline snapshot from the SSE stream. */
  timeline: VideoTimeline | null;
};

type LogEntry = {
  id: string;
  ts: string;
  kind:
    | "lifecycle"
    | "node_running"
    | "node_done"
    | "node_failed"
    | "node_regen"
    | "error";
  message: string;
  detail?: string;
};

export function VideoEventLog({ timeline }: VideoEventLogProps) {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  // Refs hold the diff state across renders — they survive without
  // triggering re-renders themselves; the `entries` state above is
  // what actually paints. Splitting it this way avoids the trap of
  // stuffing diff bookkeeping into useState and paying for a render
  // on every node ref update.
  const prevByKeyRef = useRef<Map<string, string>>(new Map()); // nodeKey -> state
  const prevStatusRef = useRef<string | null>(null);
  const scrollerRef = useRef<HTMLDivElement | null>(null);

  // Append entries for every state change since the last snapshot.
  // We batch all transitions from one SSE tick into a single setState
  // so React only re-paints once per upstream message.
  useEffect(() => {
    if (!timeline) return;
    const ts = new Date().toLocaleTimeString();
    const added: LogEntry[] = [];

    // Lifecycle entry on status change.
    if (prevStatusRef.current !== timeline.status) {
      const prev = prevStatusRef.current;
      if (prev === null) {
        added.push({
          id: `lifecycle-init-${ts}`,
          ts,
          kind: "lifecycle",
          message: `Run "${timeline.title}" loaded`,
          detail: `${timeline.nodes.length} nodes · status: ${timeline.status}`,
        });
      } else {
        added.push({
          id: `lifecycle-${prev}->${timeline.status}-${ts}`,
          ts,
          kind: timeline.status === "error" ? "error" : "lifecycle",
          message: `Run is now ${timeline.status}`,
        });
      }
      prevStatusRef.current = timeline.status;
    }

    // Per-node diffs.
    for (const n of timeline.nodes) {
      const prev = prevByKeyRef.current.get(n.key);
      if (prev === n.state) continue;
      // Skip noise on first connect: there's no "transition" from
      // null to any state — that's just the initial snapshot.
      if (prev !== undefined) {
        if (n.state === "running") {
          added.push({
            id: `${n.key}-running-${ts}`,
            ts,
            kind: prev === "done" || prev === "failed" ? "node_regen" : "node_running",
            message: `${n.key} → running`,
          });
        } else if (n.state === "done") {
          added.push({
            id: `${n.key}-done-${ts}`,
            ts,
            kind: "node_done",
            message: `${n.key} → done`,
          });
        } else if (n.state === "failed") {
          added.push({
            id: `${n.key}-failed-${ts}`,
            ts,
            kind: "node_failed",
            message: `${n.key} → failed`,
            detail: n.error,
          });
        }
      }
      prevByKeyRef.current.set(n.key, n.state);
    }

    if (added.length > 0) {
      setEntries((prev) => {
        // Cap at the most recent 500 entries — defensive against
        // runaway SSE chatter (e.g. a flapping node retrying in a
        // loop). The cap is enforced on append rather than render so
        // we don't pay for the slice on every paint.
        const next = [...prev, ...added];
        return next.length > 500 ? next.slice(-500) : next;
      });
    }
  }, [timeline]);

  // Auto-scroll to the bottom after entries land so the newest line
  // is always on screen. Runs AFTER the paint that the setEntries
  // above triggered.
  useEffect(() => {
    const node = scrollerRef.current;
    if (node) node.scrollTop = node.scrollHeight;
  }, [entries.length]);

  return (
    <div className="flex h-full flex-col overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel">
      {/* Window chrome — frames the log as a floating terminal ("run.log"). */}
      <header className="flex flex-none items-center gap-2 border-b-2 border-ink bg-surface-2 px-3 py-2">
        <span className="flex flex-none gap-1.5">
          <i className="h-[10px] w-[10px] rounded-full border border-ink bg-[#ff8a8a]" />
          <i className="h-[10px] w-[10px] rounded-full border border-ink bg-gold" />
          <i className="h-[10px] w-[10px] rounded-full border border-ink bg-grass" />
        </span>
        <span className="font-pixel text-[0.55rem] tracking-wide text-muted">run.log</span>
      </header>
      <div
        ref={scrollerRef}
        className="flex-1 space-y-1 overflow-y-auto px-4 py-3 font-mono text-sm"
      >
        {entries.length === 0 ? (
          <p className="font-mono text-muted">
            Run events will stream here as nodes start, finish, or fail.
          </p>
        ) : (
          entries.map((e) => <Row key={e.id} entry={e} />)
        )}
      </div>
    </div>
  );
}

function Row({ entry }: { entry: LogEntry }) {
  return (
    <div className="flex items-start gap-2 py-0.5">
      <Icon kind={entry.kind} />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="font-mono text-[13px] text-ink">{entry.message}</span>
          <span className="ml-auto shrink-0 font-mono text-[10px] text-muted">
            {entry.ts}
          </span>
        </div>
        {entry.detail && (
          <div className="mt-0.5 truncate font-mono text-[11px] text-ink-2">
            {entry.detail}
          </div>
        )}
      </div>
    </div>
  );
}

function Icon({ kind }: { kind: LogEntry["kind"] }) {
  switch (kind) {
    case "node_running":
      return <Loader2 size={12} className="mt-1 shrink-0 animate-spin text-accent" />;
    case "node_regen":
      return <RotateCcw size={12} className="mt-1 shrink-0 text-accent" />;
    case "node_done":
      return <Check size={12} className="mt-1 shrink-0 text-grass" strokeWidth={2.4} />;
    case "node_failed":
    case "error":
      return <AlertTriangle size={12} className="mt-1 shrink-0 text-[#d4503a]" strokeWidth={2.2} />;
    case "lifecycle":
    default:
      return <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-muted" />;
  }
}
