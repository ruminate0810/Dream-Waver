"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Film } from "lucide-react";

import {
  getVideoTimeline,
  regenVideoNodes,
  videoEventsURL,
  type VideoTimeline as VideoTimelineT,
  type VideoTimelineNode,
} from "@/lib/api";

import { SceneNode } from "./SceneNode";

// VideoTimeline owns the right-pane of the video workspace.
//
// Lifecycle:
//   1. Initial fetch via GET /api/v1/video/runs/{id} so the user sees
//      the full DAG immediately (even if the SSE handshake is slow).
//   2. Open an EventSource on /api/v1/video/runs/{id}/events; each
//      `timeline` message carries the full TimelineResponse — we
//      overwrite local state, no diffing needed.
//   3. Click a SceneNode → regenVideoNodes({ node_keys: [key] }) →
//      the SSE stream emits the new "running" state on the next tick
//      and the card updates without any manual re-fetch.
//
// Why no diff machinery? — server emits one event per state change,
// and a full snapshot is small (~1 KB per node). Sending the whole
// thing keeps the wire format trivial and means a client that connects
// mid-run sees the canonical state immediately.
//
// The grouping logic ("group by kind, sort by subject") matches how
// users reason about the DAG: I'm reviewing the char sheets, then the
// scene frames, then the clips. Mixed ordering hides the pipeline shape.

const KIND_ORDER: VideoTimelineNode["kind"][] = [
  "char_sheet",
  "vehicle_sheet",
  "scene_frame",
  "scene_clip",
  "end_frame",
  "final_compose",
];

const KIND_LABEL: Record<VideoTimelineNode["kind"], string> = {
  char_sheet: "Character sheets",
  vehicle_sheet: "Vehicle sheets",
  scene_frame: "Scene frames (first frames)",
  scene_clip: "Scene clips (image → video)",
  end_frame: "End frames",
  final_compose: "Final compose",
};

export type VideoTimelineProps = {
  runId: string;
  /** Called whenever a timeline event arrives, so the parent's chat
   *  surface (left pane) can render a derived activity log. Optional. */
  onTimeline?: (snapshot: VideoTimelineT) => void;
};

export function VideoTimeline({ runId, onTimeline }: VideoTimelineProps) {
  const [timeline, setTimeline] = useState<VideoTimelineT | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [regenPending, setRegenPending] = useState<Set<string>>(new Set());

  const apply = useCallback(
    (snapshot: VideoTimelineT) => {
      setTimeline(snapshot);
      onTimeline?.(snapshot);
      // Clear regen-pending flags for nodes that have moved past pending.
      setRegenPending((prev) => {
        if (prev.size === 0) return prev;
        const next = new Set(prev);
        for (const n of snapshot.nodes) {
          if (n.state !== "pending" && next.has(n.key)) {
            next.delete(n.key);
          }
        }
        return next.size === prev.size ? prev : next;
      });
    },
    [onTimeline],
  );

  // Initial fetch — gives the user something to look at before SSE
  // catches up.
  useEffect(() => {
    let cancelled = false;
    getVideoTimeline(runId)
      .then((tl) => {
        if (!cancelled) apply(tl);
      })
      .catch((e) => {
        if (!cancelled) setError(String(e?.message ?? e));
      });
    return () => {
      cancelled = true;
    };
  }, [runId, apply]);

  // SSE subscription. We deliberately use the browser's built-in
  // EventSource (no polyfill, no library) so reconnection / backoff
  // are handled by the platform.
  useEffect(() => {
    const es = new EventSource(videoEventsURL(runId));
    es.addEventListener("timeline", (e) => {
      try {
        const data = JSON.parse((e as MessageEvent).data) as VideoTimelineT;
        apply(data);
      } catch {
        // Drop a malformed frame — next snapshot will overwrite anyway.
      }
    });
    es.addEventListener("error", (e) => {
      // EventSource fires `error` on disconnect; it auto-reconnects.
      // The `error` SSE *event* from Opendream is a different thing —
      // surface it to the user.
      const data = (e as MessageEvent).data;
      if (data) {
        try {
          const parsed = JSON.parse(data) as { message?: string };
          if (parsed?.message) setError(parsed.message);
        } catch {
          /* ignore */
        }
      }
    });
    es.addEventListener("done", () => {
      es.close();
    });
    return () => {
      es.close();
    };
  }, [runId, apply]);

  const handleRegen = useCallback(
    async (nodeKey: string) => {
      setRegenPending((prev) => new Set(prev).add(nodeKey));
      try {
        await regenVideoNodes(runId, { node_keys: [nodeKey] });
        // No setState here — the SSE stream will deliver the new
        // running/pending states. We don't optimistically flip the
        // node to "running" because dependencies can put the actual
        // start a beat later; lying to the user is worse than the
        // ~500 ms latency.
      } catch (e) {
        setError(String((e as Error)?.message ?? e));
        setRegenPending((prev) => {
          const next = new Set(prev);
          next.delete(nodeKey);
          return next;
        });
      }
    },
    [runId],
  );

  const grouped = useMemo(() => groupByKind(timeline?.nodes ?? []), [timeline]);
  const finalNode = useMemo(
    () => timeline?.nodes.find((n) => n.kind === "final_compose") ?? null,
    [timeline],
  );

  if (error && !timeline) {
    return (
      <div className="rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-700">
        Couldn't load video run: {error}
      </div>
    );
  }
  if (!timeline) {
    return (
      <div className="px-4 py-8 text-sm text-zinc-400">Loading timeline…</div>
    );
  }

  return (
    <div className="space-y-8 pb-12">
      <TimelineHeader timeline={timeline} finalNode={finalNode} />

      {error && (
        <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
          {error}
        </div>
      )}

      {KIND_ORDER.map((kind) => {
        const nodes = grouped.get(kind);
        if (!nodes || nodes.length === 0) return null;
        return (
          <section key={kind}>
            <h3 className="mb-3 font-mono text-[10px] uppercase tracking-[0.22em] text-zinc-500">
              {KIND_LABEL[kind]} · {nodes.length}
            </h3>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {nodes.map((n) => (
                <SceneNode
                  key={n.key}
                  node={n}
                  onRegen={handleRegen}
                  regenPending={regenPending.has(n.key)}
                />
              ))}
            </div>
          </section>
        );
      })}
    </div>
  );
}

// ─── Header — title + status + final clip player ─────────────────────

function TimelineHeader({
  timeline,
  finalNode,
}: {
  timeline: VideoTimelineT;
  finalNode: VideoTimelineNode | null;
}) {
  return (
    <header className="flex flex-col gap-3">
      <div className="flex items-baseline justify-between gap-4">
        <div>
          <h2 className="text-lg font-medium text-zinc-900">{timeline.title}</h2>
          <p className="mt-0.5 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-400">
            Run {timeline.run_id} · {timeline.nodes.length} nodes
          </p>
        </div>
        <StatusBadge status={timeline.status} />
      </div>

      {/* Final clip — pinned at the top so the user always sees what's
          shippable. When the final hasn't rendered yet we still reserve
          the slot so the layout doesn't shift on completion. */}
      {finalNode?.output_url ? (
        <video
          src={finalNode.output_url}
          controls
          className="aspect-video w-full rounded-lg border border-zinc-200 bg-black"
          preload="metadata"
        />
      ) : (
        <div className="flex aspect-video w-full items-center justify-center rounded-lg border border-dashed border-zinc-200 bg-zinc-50 text-sm text-zinc-400">
          <Film size={16} className="mr-2" strokeWidth={1.6} />
          Final cut renders here once every clip is ready.
        </div>
      )}
    </header>
  );
}

function StatusBadge({ status }: { status: VideoTimelineT["status"] }) {
  const tone =
    status === "finished"
      ? "border-emerald-200 bg-emerald-50 text-emerald-800"
      : status === "error"
        ? "border-red-200 bg-red-50 text-red-800"
        : status === "running"
          ? "border-amber-200 bg-amber-50 text-amber-800"
          : "border-zinc-200 bg-zinc-50 text-zinc-600";
  return (
    <span
      className={
        "inline-flex items-center rounded-full border px-2.5 py-0.5 font-mono text-[10px] uppercase tracking-[0.18em] " +
        tone
      }
    >
      {status}
    </span>
  );
}

// ─── Helpers ──────────────────────────────────────────────────────────

function groupByKind(
  nodes: VideoTimelineNode[],
): Map<VideoTimelineNode["kind"], VideoTimelineNode[]> {
  const out = new Map<VideoTimelineNode["kind"], VideoTimelineNode[]>();
  for (const n of nodes) {
    const arr = out.get(n.kind) ?? [];
    arr.push(n);
    out.set(n.kind, arr);
  }
  // Stable per-kind sort by subject when numeric, falling back to
  // key lex order — handles SceneFrame[2] before SceneFrame[10].
  for (const [k, arr] of out.entries()) {
    arr.sort((a, b) => compareKeys(a.key, b.key));
    out.set(k, arr);
  }
  return out;
}

function compareKeys(a: string, b: string): number {
  // Natural sort on the [number] suffix so SceneFrame[10] follows
  // SceneFrame[9], not SceneFrame[1].
  const ai = extractIndex(a);
  const bi = extractIndex(b);
  if (ai !== null && bi !== null && ai !== bi) return ai - bi;
  return a.localeCompare(b);
}

function extractIndex(key: string): number | null {
  const m = /\[(\d+)\]/.exec(key);
  if (!m) return null;
  const n = parseInt(m[1], 10);
  return Number.isFinite(n) ? n : null;
}
