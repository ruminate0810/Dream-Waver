import type { AgentEvent } from "@/components/chat/transport";

// sceneGrammar.ts (v21) — the semantic-binding table as DATA.
//
// This file is the code entity behind the design claim「场景语法与流水线语义
// 绑定」(docs/claw-v2-design.md §3, paper §3.3): every scene element the
// office stages maps to a pipeline-semantic event, declared here as rules and
// executed by a tiny interpreter (directivesFor). ClawOffice consumes the
// directives; it no longer hard-codes event→scene if-else.
//
// Scope note: this table covers SCENE-level staging (meetings, debate,
// celebration). Per-worker reactions (eureka/facepalm on tool.end) live in
// useWorkerStates' reducer — same binding discipline, different layer (they
// mutate per-worker state, not the shared stage). Ambient behaviors
// (strolls/coffee/chat) are deliberately NOT here: they are the default idle
// state, not semantic events (DG3).
//
// Rules marked `legacy: true` re-derive stage state by inference (from
// claw.plan arrival / critic tool activity) and only apply to event streams
// that predate the first-class `claw.phase` events — old runs and replays.
// Once a stream has shown one claw.phase event, legacy rules are ignored.

export type PhaseName =
  | "clarify"
  | "plan"
  | "debate"
  | "exec"
  | "write"
  | "review"
  | "produce"
  | "video";

export type SceneDirective =
  /** Open/extend/close a meeting scene. `open` books the meeting window,
   *  `extend` pushes the deadline (the stage keeps the room while the phase
   *  is live), `close` releases it after a short grace so walk-outs read
   *  naturally rather than teleporting. */
  | { type: "meeting"; kind: "kickoff" | "review"; op: "open" | "extend" | "close" }
  /** Stage the debate proposal exchange around the table (payload —
   *  proposals/agreed — is parsed by the consumer from the event itself). */
  | { type: "debate" }
  /** The run delivered: ring the bell, confetti, then the off-duty party.
   *  (Currently triggered by run-status flip in ClawOffice; kept in the
   *  grammar so the celebration tier (v22 诚实语法) has a declared seam.) */
  | { type: "celebrate" };

type EventMatcher = {
  kind: AgentEvent["kind"] | string;
  /** claw.phase only: which pipeline stage. */
  phase?: PhaseName;
  /** claw.phase only: boundary side. */
  status?: "start" | "end";
  /** tool.* only: the emitting sub-agent (role key). */
  agent?: string;
};

export type GrammarRule = {
  id: string;
  /** Which SA level this binding serves (self-documenting; used by docs/tests). */
  sa: "L1" | "L2" | "L3";
  when: EventMatcher;
  stage: SceneDirective[];
  /** Inference fallback for pre-v21 event streams (no claw.phase). */
  legacy?: boolean;
  note?: string;
};

export const SCENE_GRAMMAR: GrammarRule[] = [
  // ── First-class phase boundaries (v21) ─────────────────────────────────
  {
    id: "plan-opens-kickoff",
    sa: "L2",
    when: { kind: "claw.phase", phase: "plan", status: "start" },
    stage: [{ type: "meeting", kind: "kickoff", op: "open" }],
    note: "规划开始 → 全员例会,调度员坐主位",
  },
  {
    id: "debate-holds-kickoff",
    sa: "L2",
    when: { kind: "claw.phase", phase: "debate", status: "start" },
    stage: [{ type: "meeting", kind: "kickoff", op: "extend" }],
    note: "辩论进行中 → 例会持续",
  },
  {
    id: "exec-dismisses-meeting",
    sa: "L1",
    when: { kind: "claw.phase", phase: "exec", status: "start" },
    stage: [{ type: "meeting", kind: "kickoff", op: "close" }],
    note: "执行开始 → 散会回工位(带走位缓冲)",
  },
  {
    id: "review-opens-review-meeting",
    sa: "L2",
    when: { kind: "claw.phase", phase: "review", status: "start" },
    stage: [{ type: "meeting", kind: "review", op: "open" }],
    note: "评审开始 → 评审会,评审员坐主位",
  },
  {
    id: "review-end-dismisses",
    sa: "L2",
    when: { kind: "claw.phase", phase: "review", status: "end" },
    stage: [{ type: "meeting", kind: "review", op: "close" }],
    note: "评审结束 → 散会;会议时长 = 真实评审耗时(+走位缓冲)",
  },

  // ── Payload-carrying semantic events (phase-independent) ───────────────
  {
    id: "debate-payload-staged",
    sa: "L2",
    when: { kind: "claw.debate" },
    stage: [{ type: "debate" }, { type: "meeting", kind: "kickoff", op: "extend" }],
    note: "提案与共识只在 claw.debate 里,任何流都要上演",
  },

  // ── Legacy inference (pre-v21 streams only) ────────────────────────────
  {
    id: "legacy-plan-meeting",
    sa: "L2",
    when: { kind: "claw.plan" },
    stage: [{ type: "meeting", kind: "kickoff", op: "open" }],
    legacy: true,
    note: "老事件流:计划到达≈规划会议",
  },
  {
    id: "legacy-critic-review",
    sa: "L2",
    when: { kind: "tool.start", agent: "critic" },
    stage: [{ type: "meeting", kind: "review", op: "open" }],
    legacy: true,
    note: "老事件流:critic 工具活动≈评审会(时长靠猜,v21 起弃用)",
  },
  {
    id: "legacy-critic-review-hold",
    sa: "L2",
    when: { kind: "tool.end", agent: "critic" },
    stage: [{ type: "meeting", kind: "review", op: "extend" }],
    legacy: true,
  },
];

// ── Interpreter ───────────────────────────────────────────────────────────

function matches(m: EventMatcher, ev: AgentEvent): boolean {
  if (ev.kind !== m.kind) return false;
  const d = ev.data as Record<string, unknown>;
  if (m.phase !== undefined && d.claw_phase !== m.phase) return false;
  if (m.status !== undefined && d.claw_phase_status !== m.status) return false;
  if (m.agent !== undefined && d.agent !== m.agent) return false;
  return true;
}

/** directivesFor runs the grammar over one event. `phaseAware` = the stream
 *  has produced at least one claw.phase event; legacy inference rules are
 *  then suppressed (the real boundaries are authoritative). Pure function —
 *  the same event sequence always yields the same directive sequence, which
 *  is what makes replay (v24 时光机) and snapshot tests possible. */
export function directivesFor(ev: AgentEvent, phaseAware: boolean): SceneDirective[] {
  const out: SceneDirective[] = [];
  for (const rule of SCENE_GRAMMAR) {
    if (rule.legacy && phaseAware) continue;
    if (matches(rule.when, ev)) out.push(...rule.stage);
  }
  return out;
}
