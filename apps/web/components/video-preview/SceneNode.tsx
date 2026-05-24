"use client";

import { useState } from "react";
import { RotateCcw, Loader2, AlertTriangle, Check } from "lucide-react";
import clsx from "clsx";

import type { VideoTimelineNode } from "@/lib/api";

// SceneNode is the click-to-regen primitive of the video workspace.
//
// One card represents one DAG node. Hover reveals the regen button;
// clicking it fires the regen handler, which the parent forwards to
// `POST /api/v1/video/runs/{id}/regen { node_keys: [key] }`. The
// upstream planner takes care of transitive descendants, so the user
// never has to think about "if I redo this frame, do I also redo the
// clip downstream?" — they click one node, the rest follows.
//
// Visual contract:
//   - Image kinds (char_sheet / scene_frame / end_frame) → <img>
//   - scene_clip / final_compose → <video controls>
//   - No artifact yet → status badge dominates the card
//
// The kind label and subject (cid/sid) form the badge in the top-left.
// The state pill (top-right) is colour-coded to match the upstream
// state machine. Cost lives in a quiet footnote — it's a power-user
// detail, not the headline.

export type SceneNodeProps = {
  node: VideoTimelineNode;
  /** Called when the user clicks the regen button. */
  onRegen: (nodeKey: string) => void | Promise<void>;
  /** True while a regen request for this specific node is in flight. */
  regenPending?: boolean;
};

export function SceneNode({ node, onRegen, regenPending }: SceneNodeProps) {
  const [hovered, setHovered] = useState(false);

  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      className={clsx(
        "group relative flex flex-col overflow-hidden rounded-lg border bg-white shadow-[0_1px_2px_rgba(0,0,0,0.04)] transition-colors",
        node.state === "failed"
          ? "border-red-200"
          : node.state === "running"
            ? "border-amber-200"
            : node.state === "done"
              ? "border-emerald-100"
              : "border-zinc-100",
      )}
    >
      {/* Preview / thumbnail */}
      <div className="relative aspect-video bg-zinc-50">
        <NodeArtifact node={node} />

        {/* State pill — always shown so the user has glance-able status */}
        <StatePill state={node.state} />

        {/* Regen overlay button — appears on hover when not currently running */}
        {hovered && node.state !== "running" && (
          <button
            type="button"
            onClick={() => onRegen(node.key)}
            disabled={regenPending}
            className={clsx(
              "absolute inset-0 z-10 flex items-center justify-center gap-2",
              "bg-black/45 text-xs font-medium text-white backdrop-blur-[1px]",
              "transition-opacity",
              regenPending ? "cursor-wait" : "cursor-pointer",
            )}
            aria-label={`Regenerate ${node.key} and its downstream nodes`}
          >
            {regenPending ? (
              <Loader2 size={14} className="animate-spin" />
            ) : (
              <RotateCcw size={14} />
            )}
            <span>{regenPending ? "Queued…" : "Regenerate"}</span>
          </button>
        )}
      </div>

      {/* Card footer — kind + subject + cost */}
      <div className="flex items-baseline justify-between gap-2 border-t border-zinc-100 px-3 py-2">
        <div className="min-w-0">
          <div className="truncate text-[12px] font-medium text-zinc-800">
            {node.key}
          </div>
          <div className="truncate font-mono text-[10px] uppercase tracking-[0.16em] text-zinc-400">
            {labelForKind(node.kind)}
          </div>
        </div>
        {node.cost_usd > 0 && (
          <div className="shrink-0 font-mono text-[10px] text-zinc-400">
            ${node.cost_usd.toFixed(2)}
          </div>
        )}
      </div>

      {/* Error row — only visible on failure */}
      {node.error && (
        <div className="flex items-start gap-1.5 border-t border-red-100 bg-red-50 px-3 py-2 text-[11px] text-red-700">
          <AlertTriangle size={12} className="mt-0.5 shrink-0" strokeWidth={2} />
          <span className="break-words">{node.error}</span>
        </div>
      )}
    </div>
  );
}

// ─── Sub-components ───────────────────────────────────────────────────

function NodeArtifact({ node }: { node: VideoTimelineNode }) {
  if (!node.output_url) {
    return (
      <div className="absolute inset-0 flex items-center justify-center text-[11px] text-zinc-400">
        {placeholderForState(node.state)}
      </div>
    );
  }
  // scene_clip / final_compose ship as .mp4; everything else is a still.
  // We sniff on file extension because the upstream Content-Type can
  // race the cache header (the bridge mirrors whatever Opendream sent
  // first), and the URL itself is authoritative.
  const isVideo = /\.(mp4|webm|mov)(\?|$)/i.test(node.output_url);
  if (isVideo) {
    return (
      <video
        src={node.output_url}
        className="absolute inset-0 h-full w-full object-cover"
        controls
        preload="metadata"
        playsInline
      />
    );
  }
  return (
    <img
      src={node.output_url}
      alt={node.key}
      className="absolute inset-0 h-full w-full object-cover"
      loading="lazy"
    />
  );
}

function StatePill({ state }: { state: VideoTimelineNode["state"] }) {
  const styles = STATE_STYLES[state];
  return (
    <div
      className={clsx(
        "absolute right-2 top-2 z-10 inline-flex items-center gap-1 rounded-full px-2 py-0.5",
        "font-mono text-[10px] uppercase tracking-[0.12em]",
        styles.bg,
        styles.fg,
      )}
    >
      {styles.icon}
      <span>{state}</span>
    </div>
  );
}

const STATE_STYLES: Record<
  VideoTimelineNode["state"],
  { bg: string; fg: string; icon: React.ReactNode }
> = {
  pending: {
    bg: "bg-zinc-100/95",
    fg: "text-zinc-500",
    icon: <span className="h-1.5 w-1.5 rounded-full bg-zinc-400" aria-hidden />,
  },
  running: {
    bg: "bg-amber-100/95",
    fg: "text-amber-800",
    icon: <Loader2 size={10} className="animate-spin" />,
  },
  done: {
    bg: "bg-emerald-100/95",
    fg: "text-emerald-800",
    icon: <Check size={10} strokeWidth={2.5} />,
  },
  failed: {
    bg: "bg-red-100/95",
    fg: "text-red-800",
    icon: <AlertTriangle size={10} strokeWidth={2.5} />,
  },
  skipped: {
    bg: "bg-zinc-100/95",
    fg: "text-zinc-400",
    icon: <span className="h-1.5 w-1.5 rounded-full bg-zinc-300" aria-hidden />,
  },
};

function labelForKind(kind: VideoTimelineNode["kind"]): string {
  switch (kind) {
    case "char_sheet":
      return "Character sheet";
    case "vehicle_sheet":
      return "Vehicle sheet";
    case "scene_frame":
      return "Scene frame";
    case "scene_clip":
      return "Scene clip";
    case "end_frame":
      return "End frame";
    case "final_compose":
      return "Final compose";
  }
}

function placeholderForState(state: VideoTimelineNode["state"]): string {
  if (state === "running") return "Generating…";
  if (state === "failed") return "Generation failed";
  if (state === "skipped") return "Skipped";
  return "Awaiting upstream";
}
