"use client";

import { useCallback, useEffect, useReducer, useRef } from "react";

import {
  postSlideClarification,
  postSlideMessage,
  postSlideOutlineApproval,
  postSlideWizardStep,
  type SlideJob,
} from "@/lib/api";
import {
  useAgentEventStream,
  type AgentEvent,
  type EventKind,
  type Tokens,
  type WizardStepView,
} from "./transport";

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
};

export type Turn = {
  id: string;
  kind: TurnKind;
  userMessage?: string;
  steps: Step[];
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
  // step 1).
  dispatchWizardStep: (step: number, answer: string, skip: boolean) => Promise<void>;
};

// OutlineEditsPayload mirrors the Go-side slides.OutlineEdits struct
// — the shape POST /messages accepts under the `edits` key.
export type OutlineEditsPayload = {
  theme?: string;
  renames?: Array<{ index: number; title: string }>;
  delete_indices?: number[];
};

// ─── Reducer ─────────────────────────────────────────────────────────

type Action =
  | { type: "ws"; event: AgentEvent }
  | { type: "user_message"; id: string; text: string }
  | { type: "post_error"; turnId: string; err: string }
  | { type: "queue"; text: string }
  | { type: "unqueue" };

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
    slidesRendered: 0,
    slidesTotal: 0,
    status: "running",
  };
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

function reduce(state: State, action: Action): State {
  switch (action.type) {
    case "user_message": {
      // Optimistically open a new turn for the user's typed instruction.
      // The backend will emit step.start shortly; if it doesn't, the
      // turn still shows the user's text so they get feedback.
      const t = emptyTurn(action.id, "edit", action.text);
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
          patchLastStep(t, (s) => {
            const cur = s.thought;
            const nextThought: Thought = cur
              ? { ...cur, text: cur.text + delta }
              : { step: s.index, text: delta };
            return { ...s, thought: nextThought };
          }),
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
          patchLastStep(t, (s) => ({ ...s, thought })),
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
          patchLastStep(t, (s) => ({
            ...s,
            toolCalls: [...s.toolCalls, entry],
          })),
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
          ...t,
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
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...t,
          slidesRendered: Math.max(t.slidesRendered, data.slide_index ?? 0),
        })),
      };
    }
    case "slides.render.end":
    case "slides.updated":
      // Live-preview pane subscribes to slides.updated directly; the
      // session fold ignores it. render.end is implicit in agent.finish
      // for the chat surface.
      return state;

    case "agent.finish": {
      if (lastIdx < 0) return state;
      const closed = patchTurn(state.turns, lastIdx, (t) => ({
        ...t,
        status: "done" as TurnStatus,
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
        })),
      };
    }

    // ─── Sprint L1: HILT pause gates ───────────────────────────────
    case "outline.clarification_required": {
      // Attach the questions to the active turn AND flip its status
      // to "awaiting_user" so the UI knows to render the gate card.
      if (lastIdx < 0 || !data.clarification_questions) return state;
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...t,
          pending: { kind: "clarification", questions: data.clarification_questions! },
          status: "awaiting_user" as TurnStatus,
        })),
      };
    }
    case "outline.review_required": {
      if (lastIdx < 0 || !data.review_outline_json) return state;
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
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...t,
          pending: { kind: "outline_review", outline },
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
      if (lastIdx < 0) {
        const t: Turn = {
          id: "t0",
          kind: "initial",
          steps: [],
          slidesRendered: 0,
          slidesTotal: 0,
          status: "awaiting_user",
          pending: { kind: "wizard", view },
        };
        return { ...state, turns: [...state.turns, t] };
      }
      return {
        ...state,
        turns: patchTurn(state.turns, lastIdx, (t) => ({
          ...t,
          pending: { kind: "wizard", view },
          status: "awaiting_user" as TurnStatus,
        })),
      };
    }
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

  const busy =
    state.turns.length > 0 &&
    state.turns[state.turns.length - 1].status === "running";

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
        const msg = err instanceof Error ? err.message : String(err);
        dispatch({ type: "post_error", turnId: lastTurn.id, err: msg });
      }
    },
    [state.turns, job.job_id, job.session_id],
  );

  const dispatchOutlineApproval = useCallback(
    async (edits?: OutlineEditsPayload) => {
      const lastIdx = state.turns.length - 1;
      const lastTurn = lastIdx >= 0 ? state.turns[lastIdx] : null;
      if (!lastTurn || lastTurn.pending?.kind !== "outline_review") return;
      // Same optimistic flip as clarification — the resumed Phase 3
      // will emit slides.content / slides.render.* events that fold
      // onto the active turn.
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
        const msg = err instanceof Error ? err.message : String(err);
        dispatch({ type: "post_error", turnId: lastTurn.id, err: msg });
      }
    },
    [state.turns, job.job_id, job.session_id],
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
    async (step: number, answer: string, skip: boolean) => {
      const lastIdx = state.turns.length - 1;
      const lastTurn = lastIdx >= 0 ? state.turns[lastIdx] : null;
      if (!lastTurn || lastTurn.pending?.kind !== "wizard") return;

      // Optimistically clear so the spinner shows.
      dispatch({
        type: "ws",
        event: {
          session_id: job.session_id,
          kind: "step.start",
          at: new Date().toISOString(),
          data: { agent: "slides", step: lastTurn.steps.length + 1 },
        },
      });
      try {
        await postSlideWizardStep(job.job_id, step, answer, skip);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        dispatch({ type: "post_error", turnId: lastTurn.id, err: msg });
      }
    },
    [state.turns, job.job_id, job.session_id],
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
