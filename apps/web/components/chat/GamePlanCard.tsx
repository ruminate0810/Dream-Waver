"use client";

import { Gamepad2, Sparkles } from "lucide-react";

import type { GamePlanView } from "./transport";

// Sprint O.5 — games plan visibility.
//
// Before games' Pipeline.generate calls the worker LLM to write
// HTML, an upstream plan_game LLM call produces a structured
// pitch: mechanics, controls, win condition, art direction. This
// card renders that plan as a "here's what's coming" preview so
// the user doesn't watch a blank loading state for 30 seconds.
//
// MVP has NO approval gate — HTML generation is already running
// in parallel. The plan card is informational. Adding a gate
// would mean copying slides' PendingUserAction machinery into
// games (separate sprint).

export function GamePlanCard({ plan }: { plan: GamePlanView }) {
  return (
    <div className="my-3 border border-[color:var(--rule)] bg-white/60 p-5">
      <header className="mb-4 flex items-baseline justify-between gap-3">
        <div className="flex items-baseline gap-3">
          <span className="inline-flex h-5 w-5 items-center justify-center self-center text-[color:var(--vermillion)]">
            <Gamepad2 size={14} strokeWidth={1.8} />
          </span>
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--vermillion)]">
            § Plan
          </span>
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
            Agent design brief · 这游戏要做成什么样
          </span>
        </div>
        {plan.genre || plan.difficulty ? (
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-soft)]">
            {[plan.genre, plan.difficulty].filter(Boolean).join(" · ")}
          </span>
        ) : null}
      </header>

      {plan.pitch ? (
        <p className="mb-4 font-display text-[17px] italic leading-relaxed text-[color:var(--ink)]">
          {plan.pitch}
        </p>
      ) : null}

      <dl className="grid grid-cols-1 gap-x-8 gap-y-4 md:grid-cols-2">
        {plan.mechanics?.length ? (
          <PlanList label="Mechanics · 机制" items={plan.mechanics} />
        ) : null}
        {plan.controls?.length ? (
          <PlanList label="Controls · 控件" items={plan.controls} />
        ) : null}
        {plan.win_condition ? (
          <PlanText label="Win · 胜利条件" body={plan.win_condition} />
        ) : null}
        {plan.loss_condition ? (
          <PlanText label="Lose · 失败条件" body={plan.loss_condition} />
        ) : null}
        {plan.art_direction ? (
          <div className="md:col-span-2">
            <PlanText label="Art · 视觉" body={plan.art_direction} />
          </div>
        ) : null}
      </dl>

      <footer className="mt-4 flex items-baseline gap-2 border-t border-[color:var(--rule)] pt-3">
        <Sparkles size={11} strokeWidth={1.8} className="self-center text-[color:var(--ink-faint)]" />
        <p className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
          Plan is locked. HTML generation is running — preview will appear shortly.
        </p>
      </footer>
    </div>
  );
}

function PlanList({ label, items }: { label: string; items: string[] }) {
  return (
    <div>
      <dt className="mb-2 font-mono-jb text-[9px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
        {label}
      </dt>
      <dd>
        <ul className="space-y-1">
          {items.map((it, i) => (
            <li
              key={i}
              className="font-display text-[14px] leading-snug text-[color:var(--ink-soft)] before:mr-2 before:text-[color:var(--vermillion)] before:content-['·']"
            >
              {it}
            </li>
          ))}
        </ul>
      </dd>
    </div>
  );
}

function PlanText({ label, body }: { label: string; body: string }) {
  return (
    <div>
      <dt className="mb-2 font-mono-jb text-[9px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
        {label}
      </dt>
      <dd className="font-display text-[14px] leading-relaxed text-[color:var(--ink-soft)]">
        {body}
      </dd>
    </div>
  );
}
