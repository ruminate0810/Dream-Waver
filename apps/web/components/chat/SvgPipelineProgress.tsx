"use client";

import clsx from "clsx";
import { Check, Sparkles } from "lucide-react";

import type { Turn } from "./session";

// SvgPipelineProgress — stage tracker for the SVG (RunSVG) deterministic
// pipeline, which emits no tool.start/end events (so the tool-call progress
// bars never appear). It surfaces the three phases the pipeline runs —
// 规划大纲 → 逐页绘制 → 精修渲染 — with an accurate N/M count for the
// parallel authoring phase. Shown only while the SVG run is in flight.
const STAGES = ["规划大纲", "逐页绘制", "精修渲染"] as const;

export function SvgPipelineProgress({ turn, busy }: { turn: Turn; busy: boolean }) {
  const total = turn.outlineSlideCount ?? 0;
  const rendered = total > 0 ? Math.min((turn.svgRendered ?? []).length, total) : 0;

  const done = !busy;
  // 0 = planning outline, 1 = authoring slides, 2 = refine/render.
  let active: 0 | 1 | 2;
  if (done) active = 2;
  else if (total === 0) active = 0;
  else if (rendered < total) active = 1;
  else active = 2;

  const pct = total > 0 ? Math.round((rendered / total) * 100) : 0;
  const label = done
    ? "完成"
    : active === 1
      ? `逐页绘制 · ${rendered}/${total}`
      : STAGES[active];

  return (
    <div className="overflow-hidden rounded-pixel border-2 border-ink bg-surface/60 shadow-pixel-sm">
      <div className="flex items-center gap-2 border-b-2 border-ink bg-surface-2 px-3 py-2">
        <Sparkles size={12} strokeWidth={2} className="text-accent" />
        <span className="font-pixel text-[0.55rem] tracking-wide text-accent">SVG 出片</span>
        <span className="font-mono text-[11px] text-muted">· {label}</span>
        {done ? <Check size={13} strokeWidth={2.6} className="ml-auto text-grass" /> : null}
      </div>

      <div className="px-3 py-3">
        {/* Stage tracker */}
        <ol className="mb-3 flex items-center">
          {STAGES.map((s, i) => {
            const isDone = done || i < active;
            const isActive = !done && i === active;
            return (
              <li
                key={s}
                className={clsx("flex items-center gap-2", i < STAGES.length - 1 && "flex-1")}
              >
                <span
                  className={clsx(
                    "grid h-5 w-5 flex-none place-items-center rounded-full border-2 border-ink text-[9px] font-bold tabular-nums",
                    isDone
                      ? "bg-grass text-white"
                      : isActive
                        ? "bg-accent text-white"
                        : "bg-surface text-muted",
                  )}
                >
                  {isDone ? <Check size={11} strokeWidth={3} /> : i + 1}
                </span>
                <span
                  className={clsx(
                    "whitespace-nowrap font-mono text-[11px] font-semibold",
                    isActive ? "text-ink" : isDone ? "text-ink-2" : "text-muted",
                  )}
                >
                  {s}
                </span>
                {i < STAGES.length - 1 ? (
                  <span className={clsx("mx-1 h-[2px] flex-1", isDone ? "bg-ink" : "bg-line-2")} />
                ) : null}
              </li>
            );
          })}
        </ol>

        {/* Progress bar — determinate during authoring (N/M), indeterminate
            sweep while planning the outline, full once refining/rendering. */}
        <div className="relative h-[6px] w-full overflow-hidden rounded-[2px] border-2 border-ink bg-line">
          {active === 1 && total > 0 ? (
            <div
              className="absolute inset-y-0 left-0 bg-accent transition-[width] duration-500 ease-out"
              style={{ width: `${pct}%` }}
            />
          ) : done || active === 2 ? (
            <div className="absolute inset-0 bg-accent" />
          ) : (
            <span className="dw-progress-bar" />
          )}
        </div>
      </div>
    </div>
  );
}
