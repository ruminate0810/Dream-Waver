"use client";

import { Check, Loader2 } from "lucide-react";
import clsx from "clsx";

import type { ComposeProgress } from "./session";

// Sprint O.5 — compose-phase visibility for slides.
//
// Slides Phase 3's content stage is ONE batched LLM call — we can't
// emit per-slide "writing slide N" events without refactoring the
// stage to loop. Instead we publish the planned slide list up front
// (titles + layouts from the approved outline) and tick entries off
// as the existing per-slide render events arrive.
//
// Net effect for the user: instead of an opaque "writing content…"
// while a 30-second LLM call runs, they see the full deck plan
// (1. Title · cover / 2. Intro · split-image / ...) checking
// itself off as each slide renders.
//
// The strip collapses (just shows the "8 slides composed in 32.4s"
// summary) once `done` flips true via slides.compose.end.

export function ComposeStrip({ compose }: { compose: ComposeProgress }) {
  const total = compose.titles.length;
  const renderedCount = compose.rendered.length;
  const pct = total === 0 ? 0 : Math.round((renderedCount / total) * 100);

  return (
    <div className="my-3 border border-[color:var(--rule)] bg-white/60 p-4">
      <header className="mb-3 flex items-baseline justify-between gap-3">
        <div className="flex items-baseline gap-3">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--vermillion)]">
            § Compose
          </span>
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
            写 {total} 张幻灯片
          </span>
        </div>
        <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-soft)]">
          {compose.done
            ? compose.durationMs
              ? `${(compose.durationMs / 1000).toFixed(1)}s`
              : "done"
            : `${renderedCount}/${total} · ${pct}%`}
        </span>
      </header>

      <ul className="grid grid-cols-1 gap-1.5">
        {compose.titles.map((title: string, i: number) => {
          const idx = i + 1;
          const layout = compose.layouts[i] ?? "";
          const isDone = compose.rendered.includes(idx);
          // When the strip is "done" but a slide never ticked, treat
          // it as done — the agent finished but the per-slide render
          // event must have been missed (render.end didn't fire it,
          // or it raced past the subscriber). Better silent
          // graceful than a strip half full of running spinners.
          const treatDone = isDone || (compose.done && renderedCount === 0);

          return (
            <li
              key={idx}
              className={clsx(
                "flex items-baseline gap-3 border-l-2 py-1.5 pl-3 text-[13px] transition-colors",
                treatDone
                  ? "border-[color:var(--ink)]/40"
                  : "border-[color:var(--rule)]",
              )}
            >
              <span
                className={clsx(
                  "inline-flex h-4 w-4 shrink-0 items-center justify-center",
                  treatDone
                    ? "text-[color:var(--vermillion)]"
                    : "text-[color:var(--ink-faint)]",
                )}
                aria-hidden
              >
                {treatDone ? (
                  <Check size={11} strokeWidth={2.4} />
                ) : (
                  <Loader2 size={11} className="animate-spin" strokeWidth={1.6} />
                )}
              </span>
              <span className="font-mono-jb text-[9px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
                {String(idx).padStart(2, "0")}
              </span>
              <span
                className={clsx(
                  "flex-1 truncate font-display leading-snug",
                  treatDone
                    ? "text-[color:var(--ink)]"
                    : "text-[color:var(--ink-soft)]",
                )}
              >
                {title || `Slide ${idx}`}
              </span>
              {layout ? (
                <span className="ml-2 font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
                  {layout}
                </span>
              ) : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
