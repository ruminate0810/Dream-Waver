"use client";

import { useEffect, useState } from "react";
import clsx from "clsx";

import type { SlideJob } from "@/lib/api";
import { useAgentSession } from "../chat/session";
import { getActiveToolName, pickThinkingPool } from "../chat/thinking-copy";

// Sprint AF.1 — vertical 5-phase timeline shown in the left rail of
// /slides/[id]. Replaces the implicit "you have no idea what phase
// you're in" experience with an always-visible map of the journey:
//
//   § 01  提问   (clarification / wizard)
//   § 02  大纲   (plan_outline / critic_outline / outline review gate)
//   § 03  撰写   (write_content / critic_content / revise_slide)
//   § 04  渲染   (render_deck)
//   § 05  完成   (done — DeckFinished surface appears)
//
// Visual: matches Sidebar.tsx's mono-jb caps + § N number convention
// + 1px hairline rule. Active phase: vermillion solid dot + bold
// label + italic sub-label (rotating per-tool playful copy from
// thinking-copy, same source as ChatThread's ThinkingRow).
//
// Derivation: job.status is the primary signal; for "running" we
// disambiguate by the latest in-flight tool name.

type PhaseStatus = "done" | "running" | "pending" | "error";

type Phase = {
  key: "ask" | "outline" | "content" | "render" | "done";
  numeral: string; // § 01 / § 02 ...
  zh: string; // 提问 / 大纲 / 撰写 / 渲染 / 完成
  en: string; // small caps subtitle
  // tool names whose presence (running or done) implies this phase has
  // been reached. Phase "done" has no tools — it's gated on job.status.
  tools: ReadonlyArray<string>;
};

const PHASES: ReadonlyArray<Phase> = [
  {
    key: "ask",
    numeral: "§ 01",
    zh: "提问",
    en: "Clarify",
    tools: [], // no tool — driven purely by job.status awaiting_clarification + wizard
  },
  {
    key: "outline",
    numeral: "§ 02",
    zh: "大纲",
    en: "Outline",
    tools: ["plan_outline", "critic_outline", "revise_outline"],
  },
  {
    key: "content",
    numeral: "§ 03",
    zh: "撰写",
    en: "Compose",
    tools: ["write_content", "critic_content", "revise_slide"],
  },
  {
    key: "render",
    numeral: "§ 04",
    zh: "渲染",
    en: "Render",
    tools: ["render_deck"],
  },
  {
    key: "done",
    numeral: "§ 05",
    zh: "完成",
    en: "Issue",
    tools: [],
  },
];

export function DeckPhaseTimeline({ job }: { job: SlideJob }) {
  const session = useAgentSession(job);
  const lastTurn = session.turns[session.turns.length - 1];
  const activeTool = getActiveToolName(lastTurn);

  const states = derivePhaseStates(job.status, session, activeTool);

  return (
    <nav
      aria-label="Deck generation timeline"
      className="flex h-full flex-col gap-1 py-6 px-5"
    >
      <div className="mb-6 flex items-baseline gap-2">
        <span className="font-mono-jb text-[10px] uppercase tracking-[0.28em] text-[color:var(--vermillion)]">
          Vol. 01
        </span>
        <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
          · 制作日志
        </span>
      </div>

      <ol className="flex flex-col gap-0">
        {PHASES.map((phase, i) => (
          <PhaseRow
            key={phase.key}
            phase={phase}
            status={states[i]}
            isLast={i === PHASES.length - 1}
            activeTool={activeTool}
          />
        ))}
      </ol>

      {/* Footer — show slide count + theme so the rail has dense
          information beyond just the timeline. */}
      <div className="mt-auto pt-8 space-y-1.5">
        <Stat label="Pages" value={job.slide_count ? String(job.slide_count) : "—"} />
        <Stat label="Theme" value={job.input?.force_theme ?? "auto"} />
        {job.mode ? (
          <Stat label="Mode" value={job.mode} />
        ) : null}
      </div>
    </nav>
  );
}

function PhaseRow({
  phase,
  status,
  isLast,
  activeTool,
}: {
  phase: Phase;
  status: PhaseStatus;
  isLast: boolean;
  activeTool?: string;
}) {
  // For the active row, surface the rotating per-tool playful copy.
  const isActive = status === "running";
  const pool = isActive ? pickThinkingPool(undefined, activeTool) : [];
  const [idx, setIdx] = useState(0);
  useEffect(() => {
    setIdx(0);
    if (pool.length <= 1) return;
    const id = setInterval(() => {
      setIdx((i) => (i + 1) % pool.length);
    }, 3500);
    return () => clearInterval(id);
  }, [pool.join("|")]);
  const subLabel = pool[idx] ?? pool[0] ?? "";

  return (
    <li className="relative flex gap-3 pb-6">
      {/* Sprint AF.1 v2 — dot + connector live in a flex-col so the
          connector NATURALLY fills the remaining vertical space below
          the dot. No more brittle calc(100%-4px) math that left gaps
          between dots and made the timeline look disconnected. */}
      <div className="flex flex-col items-center">
        <PhaseDot status={status} />
        {!isLast ? (
          <span
            aria-hidden
            className={clsx(
              "mt-1 w-px flex-1 min-h-[28px]",
              status === "done"
                ? "bg-[color:var(--ink-soft)]/55"
                : "bg-[color:var(--rule)]",
            )}
          />
        ) : null}
      </div>

      <div className="min-w-0 flex-1 -mt-0.5 pb-1">
        <div className="flex items-baseline gap-2">
          <span
            className={clsx(
              "font-mono-jb text-[9px] uppercase tracking-[0.22em]",
              status === "done"
                ? "text-[color:var(--ink-faint)]"
                : status === "running"
                ? "text-[color:var(--vermillion)]"
                : status === "error"
                ? "text-[color:var(--vermillion)]"
                : "text-[color:var(--ink-faint)]/60",
            )}
          >
            {phase.numeral}
          </span>
        </div>
        <p
          className={clsx(
            "mt-0.5 font-display text-[15px] leading-tight",
            status === "done"
              ? "text-[color:var(--ink-soft)]"
              : status === "running"
              ? "font-medium text-[color:var(--ink)]"
              : status === "error"
              ? "text-[color:var(--ink)]"
              : "text-[color:var(--ink-faint)]/70",
          )}
        >
          {phase.zh}
          <span className="ml-1.5 font-mono-jb text-[9px] uppercase tracking-[0.2em] text-[color:var(--ink-faint)]">
            · {phase.en}
          </span>
        </p>
        {isActive && subLabel ? (
          <p
            key={subLabel}
            className="animate-phase-in mt-1 font-display text-[12px] italic text-[color:var(--vermillion)]/80"
          >
            {subLabel}
          </p>
        ) : null}
        {status === "error" ? (
          <p className="mt-1 font-display text-[12px] italic text-[color:var(--vermillion)]">
            出错了
          </p>
        ) : null}
      </div>
    </li>
  );
}

function PhaseDot({ status }: { status: PhaseStatus }) {
  const base = "mt-1 flex h-3 w-3 shrink-0 items-center justify-center";
  if (status === "done") {
    return (
      <span className={base} aria-hidden>
        <span className="h-[7px] w-[7px] rounded-full bg-[color:var(--ink-soft)]" />
      </span>
    );
  }
  if (status === "running") {
    return (
      <span className={base} aria-hidden>
        <span className="relative h-3 w-3">
          <span className="absolute inset-0 rounded-full bg-[color:var(--vermillion)]" />
          {/* Pulse ring */}
          <span className="absolute -inset-1 rounded-full border border-[color:var(--vermillion)]/40 animate-ping" />
        </span>
      </span>
    );
  }
  if (status === "error") {
    return (
      <span className={base} aria-hidden>
        <span className="h-3 w-3 rounded-full border-[1.5px] border-[color:var(--vermillion)] bg-[color:var(--paper)]" />
      </span>
    );
  }
  // pending — hollow ring
  return (
    <span className={base} aria-hidden>
      <span className="h-[9px] w-[9px] rounded-full border border-[color:var(--rule)] bg-transparent" />
    </span>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-2 text-[10px]">
      <span className="font-mono-jb uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
        {label}
      </span>
      <span className="truncate font-display text-[color:var(--ink-soft)]">
        {value}
      </span>
    </div>
  );
}

// derivePhaseStates returns the status for each of the 5 phases based
// on (a) the job's coarse status and (b) the most-recent in-flight
// tool name. The mapping is deliberately conservative — a missing
// signal defaults to "pending" rather than guessing "done".
function derivePhaseStates(
  jobStatus: SlideJob["status"],
  session: ReturnType<typeof useAgentSession>,
  activeTool: string | undefined,
): PhaseStatus[] {
  const states: PhaseStatus[] = ["pending", "pending", "pending", "pending", "pending"];

  // Error: mark whichever phase the active tool belongs to as error.
  if (jobStatus === "error") {
    const idx = activeTool ? phaseIndexForTool(activeTool) : -1;
    if (idx >= 0) states[idx] = "error";
    else states[0] = "error";
    // Anything before the error phase is done.
    for (let i = 0; i < states.length; i++) {
      if (i < (idx >= 0 ? idx : 0)) states[i] = "done";
    }
    return states;
  }

  // Finished: everything done.
  if (jobStatus === "finished") {
    return ["done", "done", "done", "done", "done"];
  }

  // HILT pauses — the gate IS the active step for that phase.
  if (jobStatus === "awaiting_clarification") {
    states[0] = "running";
    return states;
  }
  // Wizard pause (Sprint N1+Q dynamic wizard) — also "提问".
  if (session.turns.some((t) => t.pending?.kind === "wizard")) {
    states[0] = "running";
    return states;
  }
  if (jobStatus === "awaiting_outline_approval") {
    states[0] = "done";
    states[1] = "running";
    return states;
  }

  // Running — disambiguate by latest tool.
  if (jobStatus === "running") {
    const idx = activeTool ? phaseIndexForTool(activeTool) : -1;
    if (idx >= 0) {
      for (let i = 0; i < idx; i++) states[i] = "done";
      states[idx] = "running";
    } else {
      // No tool yet — agent just started. Assume we're in phase 1 (ask)
      // since wizard fires first.
      states[0] = "running";
    }
    return states;
  }

  // Fallthrough — early state before any tool fired.
  states[0] = "running";
  return states;
}

function phaseIndexForTool(name: string): number {
  for (let i = 0; i < PHASES.length; i++) {
    if (PHASES[i].tools.includes(name)) return i;
  }
  // Reflection / edit tools (analyze_deck, critic_deck, generate_image,
  // style_slide, etc) don't belong to one initial-gen phase. During
  // initial gen they won't fire; during edit turns the deck is already
  // finished so we'd be in the "done" state anyway. Default to render
  // (closest semantic neighbour) so the dot lands somewhere visible.
  if (name === "render_deck") return 3;
  return -1;
}
