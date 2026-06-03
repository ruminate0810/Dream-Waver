"use client";

import { useCallback, useEffect, useReducer, useRef } from "react";

import {
  ApiError,
  postSlideClarification,
  postSlideMessage,
  postSlideOutlineApproval,
  postSlideWizardStep,
  type SlideJob,
} from "@/lib/api";
import {
  useAgentEventStream,
  useNotifyDeckGone,
  type AgentEvent,
  type EventKind,
  type GamePlanView,
  type Tokens,
  type WizardStepView,
} from "./transport";

// 404 / 410 from a POST to /messages mean the deck truly isn't on the
// server any more (orchestrator restart wiped state AND Postgres also
// doesn't have it, or it's someone else's deck link). Bouncing to
// DeckNotFound is the correct UX for these.
//
// 409 Conflict, on the other hand, means "the deck IS still here, but
// you sent an action that doesn't fit the current pending state" —
// typical trigger: user double-clicked 完成 / 保存并继续, or the FE's
// optimistic dispatch raced an event. The first click advanced the
// gate; the second one is benign. We swallow it silently rather than
// poison the turn or bounce the page.
//
// Returns true when the error was handled (caller should skip the
// normal post_error path).
//
// `onGateAdvanced` (optional) fires for the 409 case — when the gate
// is already past, we let the caller synthesise an agent.finish so
// the spinner / card clear immediately. Without this callback, the
// session would have to wait for the next polling tick to notice
// status=finished and run the session.ts:710 fallback path.
function handleDeckGone(
  err: unknown,
  notify: () => void,
  onGateAdvanced?: () => void,
): boolean {
  if (!(err instanceof ApiError)) return false;
  if (err.status === 409) {
    // Superseded duplicate / late-fire. The deck IS still here, the
    // gate just advanced (e.g. another tab submitted, or the user is
    // clicking a stale tab whose React state lags the backend). The
    // spinner / card needs to clear; agent.finish in the reducer
    // resets both status=done and pending=undefined.
    // eslint-disable-next-line no-console
    console.debug("[session] superseded gate POST ignored:", err.message);
    onGateAdvanced?.();
    return true;
  }
  if (err.status === 410 || err.status === 404) {
    notify();
    return true;
  }
  return false;
}

// session.ts is the SINGLE place reducer logic lives.
//
// The backend emits a perfectly turn-shaped event stream:
//   step.start(step=1) → llm.thought → tool.start → tool.end → … → agent.finish
// Each "step.start" with step === 1 *opens a new Turn*. Everything else
// attaches to the most recent open Turn. That's the whole rule — no
// "if (followupBusy && turns.length > 0)" branching anywhere.
//
// Initial generation arrives as Turn 0 (kind="initial", no user message
// — we know what the user asked from job.input). Follow-up edits become
// Turn 1, 2, 3… with the user's typed instruction.

// ─── Public types (the Turn shape consumers render) ──────────────────

export type ToolCallStatus = "running" | "done" | "error";

export type ToolCallEntry = {
  id: string;
  name: string;
  status: ToolCallStatus;
  /** Truncated args preview from tool.start (~240 chars). */
  input?: string;
  /** Wall-clock ms from tool.end. */
  durationMs?: number;
  output?: string;
  error?: string;
};

export type Thought = {
  step: number;
  text: string;
  inputTokens?: number;
  outputTokens?: number;
  cacheRead?: number;
};

export type Step = {
  index: number;
  thought?: Thought;
  toolCalls: ToolCallEntry[];
};

export type TurnKind = "initial" | "edit";
export type TurnStatus = "running" | "done" | "error" | "awaiting_user";

// Sprint L1 — HILT pause payload attached to a turn while the
// initial-generation goroutine has exited waiting on the user. Cleared
// when the resume action completes.
export type PendingGate =
  | { kind: "clarification"; questions: string[] }
  | { kind: "outline_review"; outline: OutlineForReview }
  // Sprint N1 — multi-step wizard. The whole step view (step/total/
  // options/etc.) rides on this payload so the WizardCard can render
  // without round-tripping through any other state.
  | { kind: "wizard"; view: WizardStepView };

// OutlineForReview is the subset of stages.OutlineResult the review
// card actually edits. Round-trips back to the server as
// `OutlineEdits` when the user clicks 「保存并继续」.
export type OutlineForReview = {
  title: string;
  subtitle?: string;
  theme: string;
  slides: Array<{
    index: number;
    type: string;
    headline: string;
    key_points?: string[];
    speaker_notes?: string;
  }>;
  // Sprint BR — attribution fields populated by the planner. Both
  // optional — empty/missing means the outline ran free-form (no
  // blueprint, no RAG).
  reference_slugs?: string[]; // BR.3
  blueprint_id?: string;      // BR.1
};

// Sprint O.5 — compose-phase checklist. Populated on
// slides.compose.start; titles + layouts are parallel arrays. As
// slides.content events arrive (per-slide render), the matching
// index is appended to `rendered`. `done` flips at
// slides.compose.end so the strip can fade / collapse.
export type ComposeProgress = {
  titles: string[];
  layouts: string[];
  /** 1-based indices of slides whose render has fired. */
  rendered: number[];
  done: boolean;
  /** Wall-clock ms from slides.compose.end. */
  durationMs?: number;
};

// Sprint AA.4 — dialogue messages folded into a Turn's `messages`
// timeline. Each `wizard.step` becomes an "agent-question"; each
// `user.answer` (emitted by the backend in AA.3 OR optimistically by
// the FE dispatcher before POST) becomes a "user-answer". The chat
// thread renders these as bubbles so the wizard reads as a real
// dialogue instead of a single floating modal.
//
// `id` is deterministic — agent-question uses `q-${step}-${total}` and
// user-answer uses `a-${step}` — so re-emits from a replay or a backend
// re-issue dedupe by-key in the reducer. `optimistic: true` marks a
// locally-appended user-answer so a subsequent backend `user.answer`
// event replaces it without flashing two bubbles.
export type ConversationMessage =
  | {
      role: "agent-question";
      id: string;
      step: number;
      total: number;
      view: WizardStepView;
    }
  | {
      role: "user-answer";
      id: string;
      step: number;
      text: string;
      optimistic?: boolean;
    };

export type Turn = {
  id: string;
  kind: TurnKind;
  userMessage?: string;
  steps: Step[];
  // Sprint AA.4 — wizard / clarification dialogue messages threaded
  // into the chat in chronological order. Empty for edit turns.
  messages: ConversationMessage[];
  // Slides-skill annotations attached to *this* turn — only Turn 0
  // typically populates them, but a regenerate_slide turn could too.
  outlineTitle?: string;
  outlineSlideCount?: number;
  slidesRendered: number;
  slidesTotal: number;
  status: TurnStatus;
  errorMsg?: string;
  // Sprint L1 — HILT pause payload. Non-null while waiting on user
  // input; reducer sets it from outline.review_required /
  // outline.clarification_required events.
  pending?: PendingGate;
  // Sprint O.5 — Phase 3 compose visibility (slides only). Solves
  // the "write_content is a black box" complaint without refactoring
  // the batched content stage.
  compose?: ComposeProgress;
  // Sprint O.5 — games plan (informational; no approval gate in MVP).
  // Renders as a structured card before HTML generation completes.
  gamePlan?: GamePlanView;
  // Bridges the silent gap between a user action (chat message OR
  // gate submission) and the first real backend event. Set
  // optimistically by the dispatcher / `user_message` reducer, cleared
  // the moment any real progress event lands (tool.start, llm.thought,
  // llm.token, slides.outline, slides.compose.start, agent.finish,
  // agent.error). Renders as a vermillion busy chip so the user knows
  // the agent took the action and is working — instead of staring at
  // a frozen UI for the 1-3s the resume goroutine + first LLM round
  // trip takes.
  busyHint?: { kind: "preparing" | "editing"; at: number };
};

export type AgentSession = {
  turns: Turn[];
  busy: boolean;
  // Bumped each time agent.finish closes a follow-up turn. The live
  // preview pane reads this so iframes can cache-bust.
  previewVersion: number;
  // A message the user typed while the agent was still busy. It's
  // shown in the thread as a "queued" row and auto-fires the moment
  // the current turn closes. Null when nothing is queued.
  pendingMessage: string | null;
  dispatchUserMessage: (text: string) => Promise<void>;
  cancelPending: () => void;
  // Sprint L1 — HILT resume dispatchers. dispatchClarification posts
  // the H2 gate answers; dispatchOutlineApproval posts the H1 gate
  // approval (with optional edits). Both clear the corresponding
  // turn's `pending` and flip status back to running.
  dispatchClarification: (answers: string[]) => Promise<void>;
  dispatchOutlineApproval: (edits?: OutlineEditsPayload) => Promise<void>;
  // Sprint N1 — wizard step dispatcher. `skip` true bypasses the
  // step (only allowed on optional steps; backend rejects skip on
  // step 1). N1.i — `back` true (default false) treats `step` as
  // the step to GO BACK to; mutually exclusive with `skip`.
  dispatchWizardStep: (step: number, answer: string, skip: boolean, back?: boolean) => Promise<void>;
};

// OutlineEditsPayload mirrors the Go-side slides.OutlineEdits struct
// — the shape POST /messages accepts under the `edits` key.
// Sprint S — added relayouts so the user can force a per-slide layout
// from the review card before content writing.
export type OutlineEditsPayload = {
  theme?: string;
  renames?: Array<{ index: number; title: string }>;
  relayouts?: Array<{ index: number; layout: string }>;
  delete_indices?: number[];
};

// ─── Reducer ─────────────────────────────────────────────────────────

type Action =
  | { type: "ws"; event: AgentEvent }
  | { type: "user_message"; id: string; text: string }
  | { type: "post_error"; turnId: string; err: string }
  | { type: "queue"; text: string }
  | { type: "unqueue" }
  // Sprint AA.4 — optimistically append a user-answer message right
  // when the user clicks a chip / submits a free-text answer, before
  // the backend echo arrives. The reducer's user.answer case
  // upserts-by-id, so the backend event harmlessly replaces this one
  // when it lands (no flicker — same id, same key).
  | { type: "optimistic_answer"; step: number; text: string }
  // Optimistic clear of the active turn's HILT gate, fired by the
  // dispatchers (clarification / outline_approval / wizard_step) the
  // moment the user submits. The card disappears immediately and we
  // don't have to wait for the backend's first WS event to arrive —
  // critical when the WS happens to be dropped (or simply slow) at
  // submit time. If a wizard.step event lands afterwards (multi-step
  // wizard advancing to the next question) it RE-SETS pending; no
  // visible flicker because that lands in the same tick.
  | { type: "gate_submitted" };

type State = {
  turns: Turn[];
  previewVersion: number;
  // Pending message held while the agent is busy. The hook below
  // watches the busy → !busy transition and drains this slot by
  // calling dispatchUserMessage for real. One-slot only; a second
  // queued message replaces the first.
  pending: string | null;
};

function emptyTurn(id: string, kind: TurnKind, userMessage?: string): Turn {
  return {
    id,
    kind,
    userMessage,
    steps: [],
    messages: [],
    slidesRendered: 0,
    slidesTotal: 0,
    status: "running",
  };
}

// Sprint AA.4 — append-or-replace a conversation message by id. Used
// by the wizard.step / user.answer reducer cases so a re-emit (replay,
// resume, backend retry) updates in place rather than stacking dupes.
// Replacing also lets a backend `user.answer` overwrite an optimistic
// local one with the authoritative copy (server may have post-processed
// the text — e.g. trimmed whitespace).
function upsertMessage(messages: ConversationMessage[], msg: ConversationMessage): ConversationMessage[] {
  const idx = messages.findIndex((m) => m.id === msg.id);
  if (idx >= 0) {
    const next = messages.slice();
    next[idx] = msg;
    return next;
  }
  return [...messages, msg];
}

// Turn 0 is implicit — it exists from page load whether or not the
// WebSocket caught the opening step.start. That handles two real
// races: (a) mid-flight reload where step.start fired before subscribe,
// and (b) pipeline mode, which emits only slides.* events with no
// step.start at all. Both cases now find a turn to attach to.
const initialState: State = {
  turns: [emptyTurn("t0", "initial")],
  previewVersion: 0,
  pending: null,
};

// Walk back to the most-recent turn that's still open. The fold rule:
// once a turn closes (status !== "running") it never re-opens, so any
// subsequent event before the next user_message *must* belong to a new
// turn. step.start(step=1) creates one; everything else is dropped
// (defensive: malformed stream, late events after agent.finish).
function lastOpenTurnIdx(turns: Turn[]): number {
  for (let i = turns.length - 1; i >= 0; i--) {
    if (turns[i].status === "running") return i;
  }
  return -1;
}

// latestTurnIdx returns the most recent turn regardless of status. Used
// exclusively by Sprint L1/N1 pause-gate handlers (outline.review,
// clarification, wizard.step) because those events legitimately
// re-open a turn that the Sprint O agent loop already closed via
// agent.finish (terminate inside the outline/content sub-phase emits
// agent.finish before the gate fires).
function latestTurnIdx(turns: Turn[]): number {
  return turns.length - 1;
}

// Mutate a single turn at idx by applying fn; returns a new turns array.
function patchTurn(turns: Turn[], idx: number, fn: (t: Turn) => Turn): Turn[] {
  const next = turns.slice();
  next[idx] = fn(next[idx]);
  return next;
}

// Mutate the last step of a turn — used for thought + tool.start/end.
// Creates an empty step if none exists yet (shouldn't happen in a
// well-formed stream, but defensive against missing step.start).
function patchLastStep(turn: Turn, fn: (s: Step) => Step): Turn {
  if (turn.steps.length === 0) {
    return { ...turn, steps: [fn({ index: 1, toolCalls: [] })] };
  }
  const steps = turn.steps.slice();
  steps[steps.length - 1] = fn(steps[steps.length - 1]);
  return { ...turn, steps };
}

// clearBusyHint drops Turn.busyHint once a real progress signal lands.
// The hint is a placeholder for the silent gap between a user action
// (chat send / gate submit) and the first backend event; the moment
// any meaningful event arrives, the placeholder gives way to the real
// ToolStrip / phase body.
function clearBusyHint(t: Turn): Turn {
  if (!t.busyHint) return t;
  return { ...t, busyHint: undefined };
}

function reduce(state: State, action: Action): State {
  switch (action.type) {
    case "user_message": {
      // Optimistically open a new turn for the user's typed instruction.
      // The backend will emit step.start shortly; if it doesn't, the
      // turn still shows the user's text so they get feedback.
      const t: Turn = {
        ...emptyTurn(action.id, "edit", action.text),
        // Surface a "正在编辑" busy chip until the first real backend
        // event (tool.start / llm.thought) lands. Without this the
        // turn renders just the user's italic quote for the 1-3s
        // before the agent's first LLM round-trip completes — looks
        // dead.
        busyHint: { kind: "editing", at: Date.now() },
      };
      // Sending a message always clears any pending slot — either it
      // was the queued message being drained, or the user typed
      // something new while busy=false.
      return { ...state, turns: [...state.turns, t], pending: null };
    }
    case "post_error": {
      // postSlideMessage failed; mark the optimistic turn as error so
      // the UI doesn't spin forever.
      const idx = state.turns.findIndex((t) => t.id === action.turnId);
      if (idx < 0) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, idx, (t) => ({
          ...t,
          status: "error",
          errorMsg: action.err,
        })),
      };
    }
    case "queue":
      // Single-slot queue: a second queued message replaces the first.
      // The user just typed something newer; they probably meant the
      // new one.
      return { ...state, pending: action.text };
    case "unqueue":
      return { ...state, pending: null };
    case "gate_submitted": {
      // Optimistic clear of the active turn's HILT pending. Fired by
      // the gate dispatchers (clarification / outline_approval /
      // wizard_step) the moment a POST is sent. The card disappears
      // immediately even if the backend's WS happens to be dropped
      // or slow. If the next backend event is wizard.step (multi-
      // step wizard continuing), the reducer's wizard.step case
      // re-sets pending in the same render tick — no visible flicker.
      //
      // CRITICAL: status flips from "awaiting_user" back to "running".
      // Without this, the turn stays "awaiting_user" → lastOpenTurnIdx
      // returns -1 → every subsequent backend event (step.start, tool.
      // start, agent.finish from Phase 3) is silently dropped on the
      // floor. Right-pane LivePreviewStack still works because it reads
      // job.slide_count separately, but the chat thread stalls on the
      // last Phase-1 tool card forever. Discovered after Sprint AC
      // shipped — symptom was "I clicked 保存并继续 and chat never
      // moved past terminate".
      //
      // ALSO sets busyHint so the ChatThread can render a "正在准备"
      // hint until the first real backend event lands.
      const lastIdx = state.turns.length - 1;
      if (lastIdx < 0) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...t,
          pending: undefined,
          status: "running" as TurnStatus,
          busyHint: { kind: "preparing", at: Date.now() },
        })),
      };
    }
    case "optimistic_answer": {
      // Sprint AA.4 — append the user's reply to the active turn's
      // dialogue timeline the moment the chip / submit is clicked,
      // before the POST round-trips. The backend's authoritative
      // user.answer event will upsert-by-id (a-${step}) so when it
      // arrives there's no duplicate and no flash. We DO NOT mutate
      // any other turn state here — the existing gate_submitted
      // action (fired separately by dispatchWizardStep) handles
      // pending / busyHint / status flips.
      const lastIdx = state.turns.length - 1;
      if (lastIdx < 0) return state;
      const msg: ConversationMessage = {
        role: "user-answer",
        id: `a-${action.step}`,
        step: action.step,
        text: action.text,
        optimistic: true,
      };
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...t,
          messages: upsertMessage(t.messages, msg),
        })),
      };
    }
    case "ws":
      return reduceWS(state, action.event);
  }
}

function reduceWS(state: State, ev: AgentEvent): State {
  const { kind, data } = ev;
  const lastIdx = lastOpenTurnIdx(state.turns);

  switch (kind as EventKind) {
    case "step.start": {
      // Turn 0 is pre-created, so lastIdx ≥ 0 in practice. step.start
      // appends a new Step record to the most recent open turn (Turn 0
      // for initial gen; the edit turn for follow-ups).
      if (lastIdx < 0) return state;
      const step = data.step ?? 1;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => {
          if (t.steps.find((s) => s.index === step)) return t;
          return { ...t, steps: [...t.steps, { index: step, toolCalls: [] }] };
        }),
      };
    }

    case "step.end":
      // Step durations land here (data.duration_ms). For now we let the
      // existing Turn → status logic close things out via agent.finish;
      // J-3 (ToolStrip / timeline UI) will surface the per-step ms.
      return state;

    case "llm.token": {
      // Streaming text delta. Append to the latest thought in the
      // current step. The subsequent llm.thought event overwrites with
      // the cleaned summary — so the bubble shows the streaming raw
      // text first, then settles to the final form.
      if (lastIdx < 0) return state;
      const delta = data.text ?? "";
      if (!delta) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) =>
          clearBusyHint(
            patchLastStep(t, (s) => {
              const cur = s.thought;
              const nextThought: Thought = cur
                ? { ...cur, text: cur.text + delta }
                : { step: s.index, text: delta };
              return { ...s, thought: nextThought };
            }),
          ),
        ),
      };
    }

    case "llm.thought": {
      if (lastIdx < 0) return state;
      const text = (data.text ?? "").trim();
      if (!text) return state;
      const thought: Thought = {
        step: state.turns[lastIdx].steps.length || 1,
        text,
        inputTokens: data.tokens?.input,
        outputTokens: data.tokens?.output,
        cacheRead: data.tokens?.cache_read,
      };
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) =>
          clearBusyHint(patchLastStep(t, (s) => ({ ...s, thought }))),
        ),
      };
    }

    case "tool.start": {
      if (lastIdx < 0) return state;
      if (!data.tool_name || !data.tool_id) return state;
      const entry: ToolCallEntry = {
        id: data.tool_id,
        name: data.tool_name,
        status: "running",
        input: data.tool_input,
      };
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) =>
          clearBusyHint(
            patchLastStep(t, (s) => ({
              ...s,
              toolCalls: [...s.toolCalls, entry],
            })),
          ),
        ),
      };
    }

    case "tool.end": {
      if (lastIdx < 0) return state;
      const id = data.tool_id;
      if (!id) return state;
      const isErr = !!data.error;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => {
          // Walk every step looking for the matching tool id — tool.end
          // for an earlier step's tool can arrive after step.start of
          // the next step in theory; defensive O(N×M) is fine because
          // steps and tools per step are both small.
          const steps = t.steps.map((s) => ({
            ...s,
            toolCalls: s.toolCalls.map((c) =>
              c.id === id
                ? {
                    ...c,
                    status: (isErr ? "error" : "done") as ToolCallStatus,
                    output: data.tool_output,
                    error: data.error,
                    durationMs: data.duration_ms,
                  }
                : c,
            ),
          }));
          return { ...t, steps };
        }),
      };
    }

    // ─── Slides-skill annotations on the active turn ───────────────
    case "slides.outline": {
      if (lastIdx < 0) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...clearBusyHint(t),
          outlineTitle: data.outline_title,
          outlineSlideCount: data.slide_count,
        })),
      };
    }
    case "slides.render.start": {
      if (lastIdx < 0) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...t,
          slidesRendered: 0,
          slidesTotal: data.slide_count ?? t.slidesTotal,
        })),
      };
    }
    case "slides.content": {
      if (lastIdx < 0) return state;
      const idx = data.slide_index ?? 0;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...t,
          slidesRendered: Math.max(t.slidesRendered, idx),
          // Sprint O.5 — tick off the compose checklist as render
          // events arrive. Compose strip uses this to flip the per-
          // slide indicator from "queued" → "done".
          compose: t.compose
            ? {
                ...t.compose,
                rendered:
                  idx > 0 && !t.compose.rendered.includes(idx)
                    ? [...t.compose.rendered, idx]
                    : t.compose.rendered,
              }
            : t.compose,
        })),
      };
    }
    case "slides.render.end":
    case "slides.updated":
      // Live-preview pane subscribes to slides.updated directly; the
      // session fold ignores it. render.end is implicit in agent.finish
      // for the chat surface.
      return state;

    // ─── Sprint O.5: compose-phase visibility ─────────────────────
    case "slides.compose.start": {
      if (lastIdx < 0) return state;
      const titles = data.slide_titles ?? [];
      const layouts = data.slide_layouts ?? [];
      if (titles.length === 0) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...clearBusyHint(t),
          compose: {
            titles,
            layouts,
            rendered: [],
            done: false,
          },
        })),
      };
    }
    case "slides.compose.end": {
      if (lastIdx < 0) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) =>
          t.compose
            ? {
                ...t,
                compose: { ...t.compose, done: true, durationMs: data.duration_ms },
              }
            : t,
        ),
      };
    }

    // ─── Sprint O.5: games plan card ──────────────────────────────
    case "game.plan": {
      if (lastIdx < 0 || !data.game_plan_json) return state;
      let view: GamePlanView;
      try {
        view = JSON.parse(data.game_plan_json) as GamePlanView;
      } catch {
        // eslint-disable-next-line no-console
        console.warn("[O.5] failed to parse game_plan_json", data.game_plan_json);
        return state;
      }
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({ ...t, gamePlan: view })),
      };
    }

    case "agent.finish": {
      if (lastIdx < 0) return state;
      const closed = patchTurn(state.turns, lastIdx, (t) => ({
        ...t,
        status: "done" as TurnStatus,
        // Once the turn is done, any pending HILT gate is moot.
        // Clearing it here makes the wizard / outline review / clarify
        // cards disappear even when the FE first learns about the
        // close via polling fallback (session.ts:710 path) — important
        // when the user is on a stale tab where the WS dropped and
        // they keep clicking the gate button getting 409s. Without
        // this clear, the card would linger forever even after Turn 0
        // closed.
        pending: undefined,
        busyHint: undefined,
      }));
      // Bump preview version on every close — initial gen *and* every
      // follow-up edit. The live preview's per-slide version bumping
      // (slides.updated) is finer-grained; this is for components that
      // only need a coarse "something changed" signal (e.g. PreviewGrid).
      return { ...state, turns: closed, previewVersion: state.previewVersion + 1 };
    }

    case "agent.error": {
      if (lastIdx < 0) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...t,
          status: "error" as TurnStatus,
          errorMsg: data.error,
          // Same as agent.finish — clear the gate so the user isn't
          // stuck clicking a card that posts to a dead session.
          pending: undefined,
          busyHint: undefined,
        })),
      };
    }

    // ─── Sprint L1: HILT pause gates ───────────────────────────────
    //
    // Pause events use latestTurnIdx (not lastOpenTurnIdx) because the
    // Sprint O agent loop emits agent.finish (status: "done") from the
    // outline / content sub-phase before the L1 gate fires. Sticking
    // to lastOpenTurnIdx would drop the pending payload — symptom:
    // user sees "terminate · 完成" then nothing, no review card, deck
    // never appears. The pause RE-OPENS the closed turn into
    // awaiting_user, which is the correct end state.
    case "outline.clarification_required": {
      const target = latestTurnIdx(state.turns);
      if (target < 0 || !data.clarification_questions) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, target, (t) => ({
          ...t,
          pending: { kind: "clarification", questions: data.clarification_questions! },
          busyHint: undefined,
          status: "awaiting_user" as TurnStatus,
        })),
      };
    }
    case "outline.review_required": {
      const target = latestTurnIdx(state.turns);
      if (target < 0 || !data.review_outline_json) return state;
      let outline: OutlineForReview;
      try {
        outline = JSON.parse(data.review_outline_json) as OutlineForReview;
      } catch {
        // Malformed payload — log and ignore so we don't crash the
        // turn. The user will just see no card and have to refresh.
        // eslint-disable-next-line no-console
        console.warn("[L1] failed to parse review_outline_json", data.review_outline_json);
        return state;
      }
      return {
        ...state,
        turns: patchTurn(state.turns, target, (t) => ({
          ...t,
          pending: { kind: "outline_review", outline },
          busyHint: undefined,
          status: "awaiting_user" as TurnStatus,
        })),
      };
    }

    // ─── Sprint N1: wizard step ────────────────────────────────────
    case "wizard.step": {
      if (!data.wizard_step_json) return state;
      let view: WizardStepView;
      try {
        view = JSON.parse(data.wizard_step_json) as WizardStepView;
      } catch {
        // eslint-disable-next-line no-console
        console.warn("[N1] failed to parse wizard_step_json", data.wizard_step_json);
        return state;
      }
      // The wizard fires on Turn 0 (initial generation). If no turn
      // exists yet — the wizard event arrived before any step.start —
      // synthesise a Turn 0 here so the card can attach to it.
      const target = latestTurnIdx(state.turns);
      // Sprint AA.4 — also fold the step into the dialogue timeline so
      // the chat renders it as an inline agent-question bubble (the
      // floating-modal WizardCard is being retired). `pending` is kept
      // for backward compat with hydration / L1 fallbacks but the
      // primary surface is now the messages array.
      const qMsg: ConversationMessage = {
        role: "agent-question",
        id: `q-${view.step}-${view.total}`,
        step: view.step,
        total: view.total,
        view,
      };
      if (target < 0) {
        const t: Turn = {
          id: "t0",
          kind: "initial",
          steps: [],
          messages: [qMsg],
          slidesRendered: 0,
          slidesTotal: 0,
          status: "awaiting_user",
          pending: { kind: "wizard", view },
        };
        return { ...state, turns: [...state.turns, t] };
      }
      return {
        ...state,
        turns: patchTurn(state.turns, target, (t) => ({
          ...t,
          pending: { kind: "wizard", view },
          messages: upsertMessage(t.messages, qMsg),
          // A new wizard step replaces the busy chip — the question
          // bubble itself is the in-flight UI now.
          busyHint: undefined,
          status: "awaiting_user" as TurnStatus,
        })),
      };
    }

    // ─── Sprint AA.4 — user's wizard / clarification reply ─────────
    case "user.answer": {
      const target = latestTurnIdx(state.turns);
      if (target < 0) return state;
      const step = data.answer_to_step ?? 0;
      const text = data.answer_text ?? "";
      const aMsg: ConversationMessage = {
        role: "user-answer",
        id: `a-${step}`,
        step,
        text,
        // The backend echo always overrides any local optimistic flag
        // — server text is authoritative.
        optimistic: false,
      };
      return {
        ...state,
        turns: patchTurn(state.turns, target, (t) => ({
          ...t,
          messages: upsertMessage(t.messages, aMsg),
        })),
      };
    }

    // Defensive default — unknown / future event kinds (e.g. Sprint
    // O.5 compose/games events being added incrementally in
    // transport.tsx) are ignored rather than crashing the fold. The
    // ChatThread surface only knows how to render the kinds it has
    // explicit cases for.
    default:
      return state;
  }
}

// ─── Hook ────────────────────────────────────────────────────────────

/**
 * useAgentSession folds the live event stream into typed Turn[].
 * Returns the rendered state + a dispatcher for outgoing user messages.
 *
 * Call exactly once per page (inside <AgentSessionProvider>). The
 * resulting object is referentially stable across renders except when
 * state actually changes.
 */
export function useAgentSession(job: SlideJob): AgentSession {
  const stream = useAgentEventStream();
  const notifyDeckGone = useNotifyDeckGone();
  const [state, dispatch] = useReducer(reduce, initialState);

  // Subscribe to the shared stream on mount.
  useEffect(() => {
    return stream.subscribe((ev) => dispatch({ type: "ws", event: ev }));
  }, [stream]);

  // Backfill from polling state. Two scenarios this guards against:
  //
  //  (a) The user navigated directly to /slides/{id} for a job that
  //      already finished — the WS will be silent because the agent
  //      loop ended long ago. We synth a slides.outline + agent.finish
  //      so Turn 0 closes with sensible content.
  //
  //  (b) The deck rendered while the page was open but events were
  //      dropped (slow connect). job.slide_count grew via polling but
  //      Turn 0 still shows no outline. Same synth path.
  //
  // We DO NOT clobber if Turn 0 already has live data — the guard checks
  // whether outlineSlideCount is unset.
  useEffect(() => {
    const t0 = state.turns.find((t) => t.kind === "initial");
    if (!t0) return;
    if (job.slide_count && job.slide_count > 0 && !t0.outlineSlideCount) {
      dispatch({
        type: "ws",
        event: {
          session_id: job.session_id,
          kind: "slides.outline",
          at: new Date().toISOString(),
          data: {
            outline_title: job.title ?? job.input?.topic,
            slide_count: job.slide_count,
          },
        },
      });
    }
    if (job.status === "finished" && t0.status === "running") {
      dispatch({
        type: "ws",
        event: {
          session_id: job.session_id,
          kind: "agent.finish",
          at: new Date().toISOString(),
          data: { agent: "slides" },
        },
      });
    }
    if (job.status === "error" && t0.status === "running") {
      dispatch({
        type: "ws",
        event: {
          session_id: job.session_id,
          kind: "agent.error",
          at: new Date().toISOString(),
          data: { agent: "slides", error: job.error },
        },
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [job.slide_count, job.status, job.error]);

  // Sprint N1.h — hydrate the active HILT pending from the SlideJob
  // payload when status is awaiting_*. Without this, a user who
  // refreshes the page (or navigates to /slides/{id} after the gate
  // event already fired) sees no card — the WS event won't replay.
  //
  // The synthesized event mirrors what the live WS would have sent;
  // the reducer's existing case handlers attach it to Turn 0 or
  // create one as needed. We DON'T fire if the active turn already
  // has matching pending (avoid clobbering the live state).
  useEffect(() => {
    if (!job.pending) return;
    const t = state.turns[state.turns.length - 1];
    if (t?.pending) return; // already hydrated (via WS or prior render)

    const baseEv = {
      session_id: job.session_id,
      at: new Date().toISOString(),
    };
    if (job.pending.kind === "wizard") {
      dispatch({
        type: "ws",
        event: {
          ...baseEv,
          kind: "wizard.step",
          data: { wizard_step_json: JSON.stringify(job.pending.wizard) },
        },
      });
    } else if (job.pending.kind === "outline_review") {
      dispatch({
        type: "ws",
        event: {
          ...baseEv,
          kind: "outline.review_required",
          data: { review_outline_json: job.pending.outline_json },
        },
      });
    } else if (job.pending.kind === "clarification") {
      dispatch({
        type: "ws",
        event: {
          ...baseEv,
          kind: "outline.clarification_required",
          data: { clarification_questions: job.pending.questions },
        },
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [job.pending?.kind, job.status]);

  const busy =
    state.turns.length > 0 &&
    state.turns[state.turns.length - 1].status === "running";

  // Synthesise an agent.finish event for the current turn — used by
  // the 409 handler to clear the stale gate card + spinner when the
  // backend reports the gate already advanced. Reducer's agent.finish
  // case clears t.status (→ done) + t.pending (→ undefined), so the
  // OutlineReviewCard / WizardCard disappear and busy flips false.
  // Cheap fallback: if polling is alive it would do the same within
  // ~2s, but firing immediately removes the "rapid 409 spam" feel
  // when the user clicks a stale button several times.
  const synthFinish = () => {
    dispatch({
      type: "ws",
      event: {
        session_id: job.session_id,
        kind: "agent.finish",
        at: new Date().toISOString(),
        data: { agent: "slides" },
      },
    });
  };

  // The actual send — opens a new turn + POSTs. Wrapped in a ref so
  // the busy-drain effect below can call it without ending up as a
  // useEffect dep that re-fires on every render.
  const fireRef = useRef<(text: string) => Promise<void>>(async () => {});
  fireRef.current = async (text: string) => {
    const id =
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `turn-${Date.now()}`;
    dispatch({ type: "user_message", id, text });
    try {
      await postSlideMessage(job.job_id, text);
    } catch (err) {
      if (handleDeckGone(err, notifyDeckGone, synthFinish)) return;
      const msg = err instanceof Error ? err.message : String(err);
      dispatch({ type: "post_error", turnId: id, err: msg });
    }
  };

  // dispatchUserMessage is what the composer calls. If the agent is
  // busy, we queue (single slot, newer-wins). Otherwise we fire now.
  // The hook contract is the same — caller does not have to know
  // about queueing.
  const dispatchUserMessage = useCallback(
    async (text: string) => {
      if (busy) {
        dispatch({ type: "queue", text });
        return;
      }
      await fireRef.current(text);
    },
    [busy],
  );

  const cancelPending = useCallback(() => {
    dispatch({ type: "unqueue" });
  }, []);

  // Sprint L1 — resume dispatchers for the two HILT gates. Both clear
  // the active turn's `pending` payload (so the card vanishes) AND
  // flip its status back to "running" so the chat shell knows new
  // events are imminent. Failures bubble back to the card via the
  // standard post_error path.
  const dispatchClarification = useCallback(
    async (answers: string[]) => {
      const lastIdx = state.turns.length - 1;
      const lastTurn = lastIdx >= 0 ? state.turns[lastIdx] : null;
      if (!lastTurn || lastTurn.pending?.kind !== "clarification") return;
      // Optimistically clear the gate + go back to running. WS events
      // from the resumed Phase 1 will fold normally onto this turn.
      // gate_submitted clears the active turn's pending so the card
      // disappears the instant the user submits — important when the
      // backend WS happens to be slow / dropped.
      dispatch({ type: "gate_submitted" });
      dispatch({
        type: "ws",
        event: {
          session_id: job.session_id,
          kind: "step.start",
          at: new Date().toISOString(),
          data: { agent: "slides", step: 1 },
        },
      });
      try {
        await postSlideClarification(job.job_id, answers);
      } catch (err) {
        if (handleDeckGone(err, notifyDeckGone, synthFinish)) return;
        const msg = err instanceof Error ? err.message : String(err);
        dispatch({ type: "post_error", turnId: lastTurn.id, err: msg });
      }
    },
    [state.turns, job.job_id, job.session_id, notifyDeckGone],
  );

  const dispatchOutlineApproval = useCallback(
    async (edits?: OutlineEditsPayload) => {
      const lastIdx = state.turns.length - 1;
      const lastTurn = lastIdx >= 0 ? state.turns[lastIdx] : null;
      if (!lastTurn || lastTurn.pending?.kind !== "outline_review") return;
      // Same optimistic flip as clarification — the resumed Phase 3
      // will emit slides.content / slides.render.* events that fold
      // onto the active turn.
      dispatch({ type: "gate_submitted" });
      dispatch({
        type: "ws",
        event: {
          session_id: job.session_id,
          kind: "step.start",
          at: new Date().toISOString(),
          data: { agent: "slides", step: 1 },
        },
      });
      try {
        await postSlideOutlineApproval(job.job_id, edits);
      } catch (err) {
        if (handleDeckGone(err, notifyDeckGone, synthFinish)) return;
        const msg = err instanceof Error ? err.message : String(err);
        dispatch({ type: "post_error", turnId: lastTurn.id, err: msg });
      }
    },
    [state.turns, job.job_id, job.session_id, notifyDeckGone],
  );

  // Sprint N1 — wizard step dispatcher. Posts {action:"wizard_step",
  // wizard_step, wizard_answer, wizard_skip} back to /messages; the
  // backend either emits the next step's view OR (on the final step)
  // proceeds to outline planning which itself pauses at the H1 gate.
  //
  // The optimistic UI here is lighter than the L1 dispatchers: we do
  // NOT synthesize a step.start, because the next state we expect is
  // either ANOTHER wizard.step (still awaiting_user) or — on the last
  // step — slides.outline events from Phase 1. Either way the existing
  // event fold handles it. We DO clear the active turn's pending so
  // the WizardCard disappears immediately; the next event re-attaches
  // the pending or transitions to running.
  const dispatchWizardStep = useCallback(
    async (step: number, answer: string, skip: boolean, back = false) => {
      const lastIdx = state.turns.length - 1;
      const lastTurn = lastIdx >= 0 ? state.turns[lastIdx] : null;
      if (!lastTurn || lastTurn.pending?.kind !== "wizard") return;

      // Optimistically clear so the spinner shows. We DON'T fire a
      // synthetic step.start for back nav — that would close the
      // wizard's Turn 0 state and feel jarring; back is just a quick
      // re-render of the previous step's view.
      if (!back) {
        // Sprint AA.4 — also append the user's reply to the dialogue
        // timeline immediately. The backend's user.answer echo
        // (Sprint AA.3) upserts by `a-${step}`, so when it lands the
        // local message is replaced atomically — no flicker. We
        // mirror the backend's placeholder for skip so the bubble
        // reads as a real choice rather than going blank.
        const optimisticText = skip ? "（跳过）" : answer;
        dispatch({ type: "optimistic_answer", step, text: optimisticText });
        dispatch({ type: "gate_submitted" });
        dispatch({
          type: "ws",
          event: {
            session_id: job.session_id,
            kind: "step.start",
            at: new Date().toISOString(),
            data: { agent: "slides", step: lastTurn.steps.length + 1 },
          },
        });
      }
      try {
        await postSlideWizardStep(job.job_id, step, answer, skip, back);
      } catch (err) {
        if (handleDeckGone(err, notifyDeckGone, synthFinish)) return;
        const msg = err instanceof Error ? err.message : String(err);
        dispatch({ type: "post_error", turnId: lastTurn.id, err: msg });
      }
    },
    [state.turns, job.job_id, job.session_id, notifyDeckGone],
  );

  // Drain on busy → !busy. When the current turn closes (agent.finish
  // or agent.error) we fire any queued message. The dispatch sets
  // pending=null inside the user_message case so we don't re-fire on
  // the next render.
  useEffect(() => {
    if (busy) return;
    const text = state.pending;
    if (!text) return;
    // Schedule on the next tick so the closing turn's UI flush
    // completes before the next turn opens. Avoids a one-frame jank
    // where two "running" turns are visible simultaneously.
    const id = setTimeout(() => {
      fireRef.current(text);
    }, 0);
    return () => clearTimeout(id);
  }, [busy, state.pending]);

  return {
    turns: state.turns,
    busy,
    previewVersion: state.previewVersion,
    pendingMessage: state.pending,
    dispatchUserMessage,
    cancelPending,
    dispatchClarification,
    dispatchOutlineApproval,
    dispatchWizardStep,
  };
}

// ─── Re-exports for consumers ────────────────────────────────────────

export type { Tokens };
