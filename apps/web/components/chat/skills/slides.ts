"use client";

import type { Step, Thought, ToolCallEntry, Turn } from "../session";

// skills/slides.ts owns the slides-specific opening view: which tools
// map to which editorial phase, and how each phase's status derives
// from the underlying tool call state. Pure data + helpers — no React.
//
// When SheetSkill / DocSkill / WebsiteSkill arrive, each gets its own
// file here. <SlidesOpening> imports from this one; future
// <SheetOpening> imports from skills/sheet.ts, and so on.

export type PhaseKey = "composition" | "prose" | "press" | "issue";

export type PhaseDef = {
  key: PhaseKey;
  kicker: string;
  zhTitle: string;
  enSubtitle: string;
  // tool names whose start/end events count toward this phase. Order
  // doesn't matter; matching is by Set membership.
  matches: ReadonlyArray<string>;
};

export const SLIDES_PHASES: ReadonlyArray<PhaseDef> = [
  {
    key: "composition",
    kicker: "I",
    zhTitle: "组篇",
    enSubtitle: "Outline · plan_outline",
    matches: ["plan_outline", "web_research", "tavily_search"],
  },
  {
    key: "prose",
    kicker: "II",
    zhTitle: "撰写",
    enSubtitle: "Manuscript · write_content",
    matches: ["write_content"],
  },
  {
    key: "press",
    kicker: "III",
    zhTitle: "印制",
    enSubtitle: "Press · render_deck",
    matches: ["render_deck", "slide_render"],
  },
  {
    key: "issue",
    kicker: "IV",
    zhTitle: "出版",
    enSubtitle: "Issue · download & preview",
    matches: [],
  },
];

export type PhaseStatus = "pending" | "running" | "done" | "error";

export type PhaseState = PhaseDef & {
  status: PhaseStatus;
  toolCalls: ToolCallEntry[];
  thoughts: Thought[];
};

// derivePhaseStates walks Turn 0 and buckets its steps' tool calls +
// thoughts into the 4 editorial phases. Status is derived from the
// underlying tool states:
//   - any error tool → phase status = error
//   - any running tool → phase status = running
//   - any done tool, no others → phase status = done
//   - no tools at all → phase status = pending
//
// The "issue" phase is special: it has no tools of its own; it becomes
// "done" the moment the press phase completes, since that's when the
// pptx artifact exists.
export function derivePhaseStates(turn: Turn | undefined): PhaseState[] {
  // Flatten every step in the turn into one pool of tool calls + thoughts
  // so phase membership is by tool name only (steps are an agent-loop
  // detail, irrelevant to which editorial phase a tool belongs to).
  const allToolCalls: ToolCallEntry[] = [];
  const allThoughts: Thought[] = [];
  if (turn) {
    for (const s of turn.steps) {
      allToolCalls.push(...s.toolCalls);
      if (s.thought) allThoughts.push(s.thought);
    }
  }

  const phaseStates: PhaseState[] = SLIDES_PHASES.map((def) => {
    const match = new Set(def.matches);
    const calls = allToolCalls.filter((c) => match.has(c.name));
    // Thoughts attach to whichever step's first tool belongs to this
    // phase. If no tool in this phase ran yet, no thought either.
    const thoughtSteps = new Set(
      turn?.steps
        .filter((s) => s.toolCalls.some((c) => match.has(c.name)))
        .map((s) => s.index) ?? [],
    );
    const thoughts = allThoughts.filter((t) => thoughtSteps.has(t.step));

    let status: PhaseStatus;
    if (calls.length === 0) {
      status = "pending";
    } else if (calls.some((c) => c.status === "error")) {
      status = "error";
    } else if (calls.some((c) => c.status === "running")) {
      status = "running";
    } else {
      status = "done";
    }
    return { ...def, status, toolCalls: calls, thoughts };
  });

  // "issue" is downstream of press — when press finishes, issue is done.
  const press = phaseStates.find((p) => p.key === "press");
  const issueIdx = phaseStates.findIndex((p) => p.key === "issue");
  if (press && issueIdx >= 0) {
    if (press.status === "done") phaseStates[issueIdx] = { ...phaseStates[issueIdx], status: "done" };
    else if (press.status === "error") phaseStates[issueIdx] = { ...phaseStates[issueIdx], status: "error" };
    else if (press.status === "running") phaseStates[issueIdx] = { ...phaseStates[issueIdx], status: "pending" };
  }

  return phaseStates;
}

// Helper for the unused-import warning: re-export so the opening file
// can `import type { Step } from "./skills/slides"` and stay isolated
// from session internals.
export type { Step };
