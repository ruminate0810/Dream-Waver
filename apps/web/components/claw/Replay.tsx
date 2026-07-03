"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Pause, Play, RotateCcw } from "lucide-react";
import clsx from "clsx";

import type { SessionLogEvent } from "@/lib/api";
import type { AgentEvent, AgentEventStream } from "@/components/chat/transport";

// Replay (v24 时光机) — re-enacts a persisted event journal through the SAME
// stream contract the live office consumes (StaticStreamProvider), so the
// scene grammar can't tell live from replayed. Pacing compresses real gaps
// (capped per step) and scrubbing rebuilds cumulative state by re-mounting
// consumers and fast-feeding the prefix.
//
// Known limit: time-anchored scenes (a meeting's Date.now window) only stage
// while PLAYING past their events; a cold seek rebuilds accumulated state
// (plan, incidents, stats) but not in-flight windows. Fine for review use.

const SPEEDS = [1, 8, 32] as const;
const MAX_STEP_MS = 1500; // cap idle gaps so replays never stall
const MIN_STEP_MS = 30;

export type Replay = {
  stream: AgentEventStream;
  /** bump = consumers must re-mount (seek rebuilt the world) */
  generation: number;
  playing: boolean;
  idx: number; // events delivered so far
  total: number;
  speed: number;
  atEnd: boolean;
  play: () => void;
  pause: () => void;
  seek: (i: number) => void;
  setSpeed: (s: number) => void;
  /** journal indices that carry bad news (red dots on the scrubber) */
  incidentIdx: number[];
};

export function useReplay(events: SessionLogEvent[]): Replay {
  const listeners = useRef<Set<(ev: AgentEvent) => void>>(new Set());
  const stream = useMemo<AgentEventStream>(
    () => ({
      subscribe: (fn: (ev: AgentEvent) => void) => {
        listeners.current.add(fn);
        return () => listeners.current.delete(fn);
      },
      status: "open" as AgentEventStream["status"],
    }),
    [],
  );

  const [generation, setGeneration] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [idx, setIdx] = useState(0);
  const [speed, setSpeed] = useState<number>(8);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const idxRef = useRef(0);
  const speedRef = useRef(speed);
  speedRef.current = speed;

  const emit = useCallback(
    (ev: SessionLogEvent) => {
      const frame = { kind: ev.kind, data: ev.data, ts: ev.at } as unknown as AgentEvent;
      listeners.current.forEach((fn) => fn(frame));
    },
    [],
  );

  const stop = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    setPlaying(false);
  }, []);

  const scheduleNext = useCallback(() => {
    const i = idxRef.current;
    if (i >= events.length) {
      stop();
      return;
    }
    const gap =
      i === 0
        ? 0
        : new Date(events[i].at).getTime() - new Date(events[i - 1].at).getTime();
    const delay = Math.min(Math.max(gap / speedRef.current, MIN_STEP_MS), MAX_STEP_MS);
    timer.current = setTimeout(() => {
      emit(events[i]);
      idxRef.current = i + 1;
      setIdx(i + 1);
      scheduleNext();
    }, delay);
  }, [events, emit, stop]);

  const play = useCallback(() => {
    if (idxRef.current >= events.length) return;
    setPlaying(true);
    scheduleNext();
  }, [events.length, scheduleNext]);

  const seek = useCallback(
    (target: number) => {
      stop();
      const to = Math.max(0, Math.min(target, events.length));
      // Rebuild the world: re-mount consumers, then fast-feed the prefix on
      // the next tick (after the fresh subscribers attach).
      setGeneration((g) => g + 1);
      idxRef.current = to;
      setIdx(to);
      setTimeout(() => {
        for (let i = 0; i < to; i++) emit(events[i]);
      }, 50);
    },
    [events, emit, stop],
  );

  useEffect(() => () => stop(), [stop]); // unmount cleanup

  const incidentIdx = useMemo(
    () =>
      events
        .map((ev, i) => {
          const bad =
            (ev.kind === "tool.end" && ev.data?.error) ||
            ev.kind === "claw.issue" ||
            (ev.kind === "claw.task.update" && ev.data?.task_status === "skipped");
          return bad ? i : -1;
        })
        .filter((i) => i >= 0),
    [events],
  );

  return {
    stream,
    generation,
    playing,
    idx,
    total: events.length,
    speed,
    atEnd: idx >= events.length,
    play,
    pause: stop,
    seek,
    setSpeed,
    incidentIdx,
  };
}

/** The scrubber bar: play/pause, speed, timeline with incident dots. */
export function ReplayBar({ replay }: { replay: Replay }) {
  const pct = replay.total > 0 ? (replay.idx / replay.total) * 100 : 0;
  return (
    <div className="absolute inset-x-3 bottom-3 z-[105] flex items-center gap-2 rounded-pixel border-2 border-ink bg-surface/95 px-3 py-2 shadow-pixel">
      <button
        type="button"
        aria-label={replay.playing ? "暂停" : "播放"}
        onClick={() => (replay.playing ? replay.pause() : replay.play())}
        className="grid h-7 w-7 place-items-center rounded-pixel border-2 border-ink bg-paper text-ink shadow-pixel-sm hover:-translate-y-0.5"
      >
        {replay.playing ? <Pause size={12} strokeWidth={2.4} /> : <Play size={12} strokeWidth={2.4} />}
      </button>
      <button
        type="button"
        aria-label="回到开头"
        onClick={() => replay.seek(0)}
        className="grid h-7 w-7 place-items-center rounded-pixel border-2 border-ink bg-paper text-ink-2 shadow-pixel-sm hover:-translate-y-0.5 hover:text-ink"
      >
        <RotateCcw size={12} strokeWidth={2.2} />
      </button>
      {/* timeline */}
      <div className="relative min-w-0 flex-1">
        <input
          type="range"
          aria-label="回放进度"
          min={0}
          max={replay.total}
          value={replay.idx}
          onChange={(e) => replay.seek(Number(e.target.value))}
          className="w-full accent-[#6a55ff]"
        />
        {/* incident dots — bad news is findable on the timeline too */}
        {replay.incidentIdx.map((i) => (
          <span
            key={i}
            title={`事件 #${i + 1} 有问题`}
            className="pointer-events-none absolute top-1/2 h-[6px] w-[6px] -translate-y-1/2 rounded-full"
            style={{ left: `${(i / Math.max(replay.total, 1)) * 100}%`, background: "#c96f1f" }}
          />
        ))}
      </div>
      <span className="font-mono text-[10px] tabular-nums text-muted">
        {replay.idx}/{replay.total}
      </span>
      {SPEEDS.map((s) => (
        <button
          key={s}
          type="button"
          onClick={() => replay.setSpeed(s)}
          className={clsx(
            "rounded-pixel border-2 border-ink px-1.5 py-0.5 font-mono text-[10px] font-bold shadow-pixel-sm",
            replay.speed === s ? "bg-accent text-white" : "bg-paper text-ink-2 hover:text-ink",
          )}
        >
          {s}×
        </button>
      ))}
    </div>
  );
}
