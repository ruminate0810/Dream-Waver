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
    <div className="my-3 rounded-pixel border-2 border-ink bg-surface/60 p-5 shadow-pixel-sm">
      <header className="mb-4 flex items-baseline justify-between gap-3">
        <div className="flex items-baseline gap-3">
          <span className="inline-flex h-5 w-5 items-center justify-center self-center text-accent">
            <Gamepad2 size={14} strokeWidth={1.8} />
          </span>
          <span className="font-pixel text-[0.55rem] uppercase tracking-wide text-accent">
            § Plan
          </span>
          <span className="font-mono text-[10px] font-semibold tracking-wide text-muted">
            Agent design brief · 这游戏要做成什么样
          </span>
        </div>
        {plan.genre || plan.difficulty ? (
          <span className="font-mono text-[10px] font-semibold tracking-wide text-ink-2">
            {[plan.genre, plan.difficulty].filter(Boolean).join(" · ")}
          </span>
        ) : null}
      </header>

      {plan.pitch ? (
        <p className="mb-4 font-mono text-[15px] font-semibold leading-relaxed text-ink">
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

      <footer className="mt-4 flex items-baseline gap-2 border-t border-line pt-3">
        <Sparkles size={11} strokeWidth={1.8} className="self-center text-muted" />
        <p className="font-mono text-[9px] uppercase tracking-wide text-muted">
          Plan is locked. HTML generation is running — preview will appear shortly.
        </p>
      </footer>
    </div>
  );
}

function PlanList({ label, items }: { label: string; items: string[] }) {
  return (
    <div>
      <dt className="mb-2 font-mono text-[10px] font-semibold tracking-wide text-muted">
        {label}
      </dt>
      <dd>
        <ul className="space-y-1">
          {items.map((it, i) => (
            <li
              key={i}
              className="font-mono text-[13px] leading-snug text-ink-2 before:mr-2 before:text-accent before:content-['·']"
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
      <dt className="mb-2 font-mono text-[10px] font-semibold tracking-wide text-muted">
        {label}
      </dt>
      <dd className="font-mono text-[13px] leading-relaxed text-ink-2">
        {body}
      </dd>
    </div>
  );
}
