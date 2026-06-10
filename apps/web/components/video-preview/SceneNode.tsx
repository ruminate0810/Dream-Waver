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
        "group relative flex flex-col overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm transition-transform hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-pixel",
        node.state === "running" && "bg-accent-soft",
      )}
    >
      {/* Preview / thumbnail */}
      <div className="relative aspect-video border-b-2 border-ink bg-surface-2">
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
              "bg-ink/55 font-mono text-xs font-semibold text-white backdrop-blur-[1px]",
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
      <div className="flex items-baseline justify-between gap-2 px-3 py-2">
        <div className="min-w-0">
          <div className="truncate font-mono text-[12px] font-semibold text-ink">
            {node.key}
          </div>
          <div className="truncate font-mono text-[10px] uppercase tracking-[0.16em] text-muted">
            {labelForKind(node.kind)}
          </div>
        </div>
        {node.cost_usd > 0 && (
          <div className="shrink-0 font-mono text-[10px] text-muted">
            ${node.cost_usd.toFixed(2)}
          </div>
        )}
      </div>

      {/* Error row — only visible on failure */}
      {node.error && (
        <div className="flex items-start gap-1.5 border-t-2 border-ink bg-[#fdece9] px-3 py-2 font-mono text-[11px] text-[#a23a2a]">
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
      <div className="absolute inset-0 flex items-center justify-center font-mono text-[11px] text-muted">
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
        "absolute right-2 top-2 z-10 inline-flex items-center gap-1 rounded-pixel border-[1.5px] border-ink px-2 py-0.5",
        "font-mono text-[10px] font-semibold uppercase tracking-[0.12em]",
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
    bg: "bg-[#fff7e8]",
    fg: "text-[#9a6b15]",
    icon: <span className="h-1.5 w-1.5 rounded-full bg-gold" aria-hidden />,
  },
  running: {
    bg: "bg-accent",
    fg: "text-white",
    icon: <Loader2 size={10} className="animate-spin" />,
  },
  done: {
    bg: "bg-[#eaf7ef]",
    fg: "text-[#1f7a4d]",
    icon: <Check size={10} strokeWidth={2.5} />,
  },
  failed: {
    bg: "bg-[#fdece9]",
    fg: "text-[#a23a2a]",
    icon: <AlertTriangle size={10} strokeWidth={2.5} />,
  },
  skipped: {
    bg: "bg-surface-2",
    fg: "text-muted",
    icon: <span className="h-1.5 w-1.5 rounded-full bg-muted" aria-hidden />,
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
