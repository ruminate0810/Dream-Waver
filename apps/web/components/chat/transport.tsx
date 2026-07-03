"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { eventsURL, listSessionEvents } from "@/lib/api";

// transport.ts owns the single WebSocket connection per slide session.
//
// Pre-refactor, both <Chat> and <LivePreviewStack> opened their own
// WS — same session id, same event stream, duplicated reconnect logic.
// This file makes the connection a shared resource:
//
//   <AgentSessionProvider sessionId={...}>
//      <Chat …/>            ← subscribes via useAgentEventStream()
//      <LivePreviewStack …/>← subscribes via useAgentEventStream()
//   </AgentSessionProvider>
//
// Subscribers register a callback; the provider fans every incoming
// event out to all of them. New subscribers picked up later still get
// future events but NOT past ones — by design, since each consumer
// folds the stream into its own state shape.

// ─── Wire-protocol mirror of internal/event/event.go ─────────────────

export type EventKind =
  | "step.start"
  | "step.end"
  | "llm.thought"
  | "llm.token"
  | "tool.start"
  | "tool.end"
  | "slides.outline"
  | "slides.content"
  | "slides.render.start"
  | "slides.render.end"
  | "slides.updated"
  | "agent.finish"
  | "agent.error"
  // Sprint L1 — HILT pause gates
  | "outline.clarification_required"
  | "outline.review_required"
  // Sprint N1 — multi-step wizard
  | "wizard.step"
  // Sprint O.5 — plan-execute visibility
  | "slides.compose.start"
  | "slides.compose.end"
  | "game.plan"
  // Sprint AA.3 — user's wizard / clarification reply mirrored into
  // the persisted event stream so chat history replays as a dialogue.
  | "user.answer"
  // Claw vertical — plan checklist + artifact version beats. The
  // artifact body never rides the WS; claw.artifact.updated is a
  // version notification and the frontend GETs the markdown.
  | "claw.plan"
  | "claw.task.update"
  | "claw.artifact.updated"
  // v6.4 — kickoff debate (协商): proposals + reconciled consensus
  | "claw.debate"
  // v21 — first-class pipeline stage boundaries; the office stages
  // meetings/scenes from these (see components/claw/sceneGrammar.ts)
  | "claw.phase"
  // v22 诚实语法 — quality caveats the run owns up to (revision size,
  // guard backfills, clarify fallback); drives the incident board +
  // celebration tiering
  | "claw.issue";

export type Tokens = {
  input: number;
  output: number;
  cache_read?: number;
  cache_creation?: number;
};

export type EventData = {
  // Agent loop
  agent?: string;
  step?: number;
  // LLM thought
  text?: string;
  tokens?: Tokens;
  // Tool calls
  tool_name?: string;
  tool_id?: string;
  tool_calls?: string[];
  /** Truncated preview of the args sent to the tool. Surfaced on tool.start. */
  tool_input?: string;
  /** Truncated preview of the tool's stdout/result. Surfaced on tool.end. */
  tool_output?: string;
  /** Wall-clock ms — set on tool.end (per-tool) and step.end (whole step). */
  duration_ms?: number;
  // Slides
  outline_title?: string;
  slide_count?: number;
  slide_index?: number;
  slide_bytes?: number;
  pptx_path?: string;
  // Errors
  stage?: string;
  error?: string;

  // Sprint L1 — HILT pause payloads
  clarification_questions?: string[];
  review_outline_json?: string;

  // Sprint N1 — wizard step payload. JSON-serialised WizardStepView
  // from the backend; session.ts decodes it into the typed shape.
  wizard_step_json?: string;

  // Sprint O.5 — slides compose checklist (titles + layouts come on
  // slides.compose.start, populated from the just-approved outline).
  // Parallel arrays; index i refers to the same slide.
  slide_titles?: string[];
  slide_layouts?: string[];

  // Sprint O.5 — games plan payload. JSON-serialised GamePlanView
  // from skill/games/plan.go. session.ts parses into a typed shape.
  game_plan_json?: string;

  // Sprint AA.3 — user.answer payload. answer_to_step pairs the
  // answer with the matching wizard.step (or 1-based clarification
  // question index). answer_text is the literal reply, with
  // human-readable placeholders for skip / back so replay reads as a
  // real dialogue.
  answer_to_step?: number;
  answer_text?: string;

  // Claw vertical. task_titles populates claw.plan (full ordered
  // sub-task list). task_index (1-based) + task_status
  // ("doing"|"done"|"skipped") populate claw.task.update. artifact_version
  // + artifact_bytes populate claw.artifact.updated — a notification
  // only; the markdown rides GET /claw/{id}/artifact.
  task_titles?: string[];
  task_roles?: string[]; // v2: per-task assigned worker (parallel to task_titles)
  task_index?: number;
  task_status?: string;
  artifact_version?: number;
  artifact_bytes?: number;
  artifact_kind?: string; // v2: "report" | "figure" | "deck"

  // v6.4 — claw.debate payload: JSON {proposals:[{role,text}],agreed}.
  claw_debate_json?: string;
  // v21 — claw.phase payload: which pipeline stage is opening/closing.
  claw_phase?: string; // clarify|plan|debate|exec|write|review|produce|video
  claw_phase_status?: string; // "start" | "end"
  // v22 — claw.issue payload: quality caveat kind + count.
  claw_issue_kind?: string; // report_revised | guard_backfill | clarify_fallback
  claw_issue_count?: number;
};

// ─── Sprint O.5 — games plan typed envelope ──────────────────────────
// Mirror of skill/games/plan.go's GamePlanView. The reducer parses
// the JSON string off game_plan_json into one of these.
export type GamePlanView = {
  /** One-line statement of what the game IS — e.g. "snake with
   *  speed-ramping segments and edge wraparound". */
  pitch: string;
  /** Core mechanics — bullet-style strings rendered as a list. */
  mechanics: string[];
  /** Controls (keyboard / touch) — short imperative phrases. */
  controls: string[];
  /** Win + loss conditions, separated so the frontend can label them. */
  win_condition?: string;
  loss_condition?: string;
  /** Art direction — palette / mood / visual references. */
  art_direction?: string;
  /** Genre + difficulty the planner committed to (or "auto"). */
  genre?: string;
  difficulty?: string;
};

// ─── Sprint N1 — wizard step typed envelope ──────────────────────────
// Mirror of skill/slides/wizard.go's WizardStepView. The reducer parses
// the JSON string off wizard_step_json into one of these.
export type WizardScenarioOption = {
  value: string;     // "business" | "academic" | "work" | "training" | "event" | "other"
  label: string;
  icon: string;      // lucide-react icon name
};

export type WizardStepView = {
  step: number;             // 1-based
  total: number;            // total step count
  // Sprint Q — "select" is the new value the LLM-driven wizard emits.
  // "scenario" is the legacy hardcoded-step kind, kept as alias for any
  // in-flight pre-Q session.
  kind: "scenario" | "select" | "free-text";
  question: string;
  placeholder?: string;
  options?: WizardScenarioOption[];
  optional: boolean;
  /** Sprint N1.g — heuristic pre-pick from topic keywords (step 1 only). */
  suggested_value?: string;
  /** Sprint N1.i — accumulated answers so far, for breadcrumb. */
  previous_answers?: Record<string, string>;
  /** Sprint N1.i — Chinese label of the scenario chosen in step 1. */
  previous_scenario?: string;
  /** Sprint N1.i — whether the ← back button should be enabled. */
  can_go_back?: boolean;
};

export type AgentEvent = {
  session_id: string;
  kind: EventKind;
  at: string;
  data: EventData;
};

export type ConnectionStatus = "connecting" | "open" | "closed";

// ─── Context surface ─────────────────────────────────────────────────

export type AgentEventStream = {
  subscribe: (listener: (ev: AgentEvent) => void) => () => void;
  status: ConnectionStatus;
};

const StreamCtx = createContext<AgentEventStream | null>(null);

/** Mounts a caller-supplied stream over the same context every consumer
 *  reads — consumers can't tell live from replayed. This is the seam the
 *  v24 时光机 (and offline verification) plug into. */
export function StaticStreamProvider({
  stream,
  children,
}: {
  stream: AgentEventStream;
  children: React.ReactNode;
}) {
  return <StreamCtx.Provider value={stream}>{children}</StreamCtx.Provider>;
}

/**
 * Hook every event consumer calls. Returns the shared stream — call
 * `subscribe(handler)` inside a useEffect to receive incoming events.
 *
 * Returns null when not mounted under an AgentSessionProvider; the
 * provider is added once at the page level so any null result here
 * is a bug, not a runtime edge case worth defending against.
 */
export function useAgentEventStream(): AgentEventStream {
  const ctx = useContext(StreamCtx);
  if (!ctx) {
    throw new Error("useAgentEventStream must be used inside <AgentSessionProvider>");
  }
  return ctx;
}

// Backend signals "this deck's session is gone" by responding to a
// resume / edit POST with HTTP 410 Gone (apps/web/lib/api throws
// ApiError with status 410). Without a typed signal upward, the
// session reducer would lite up the active turn with an opaque error
// string. Instead, session.ts catches 410/404 from dispatch* and
// calls this notifier; the slides page reacts by flipping into the
// DeckNotFound view (same surface as the polling-404 path).
//
// Default is a no-op so the hook works in test harnesses or any
// future caller that hasn't lifted the callback through props.
const DeckGoneCtx = createContext<() => void>(() => {});

export function useNotifyDeckGone(): () => void {
  return useContext(DeckGoneCtx);
}

// ─── Provider implementation ─────────────────────────────────────────

export function AgentSessionProvider({
  sessionId,
  onDeckGone,
  children,
}: {
  sessionId: string;
  /**
   * Called from session.ts when a resume / edit POST returns 410 (or
   * 404). The slides page passes `() => setNotFound(true)` so the
   * surface flips to its DeckNotFound view — same terminal state the
   * polling effect already lands on. Optional; defaults to a no-op
   * so tests and non-page consumers keep working.
   */
  onDeckGone?: () => void;
  children: ReactNode;
}) {
  // Listener registry — mutated outside React's render cycle so adding
  // a listener doesn't re-trigger the WS effect. The Set identity is
  // stable for the lifetime of the provider; we mutate in place.
  const listenersRef = useRef<Set<(ev: AgentEvent) => void>>(new Set());

  // Sprint AA.2 — replay buffer. On cold mount we fetch the persisted
  // chat_events log and stash it here. Every event is also broadcast
  // to already-registered listeners; and `subscribe` flushes the
  // buffer to any listener that registers LATER (so mount-order between
  // provider hydrate and consumer subscribe doesn't matter — every
  // subscriber sees full history then live events). Live WS frames
  // are also appended so a late subscriber gets the whole story.
  const replayBufferRef = useRef<AgentEvent[]>([]);
  const hydratedRef = useRef(false);

  const [status, setStatus] = useState<ConnectionStatus>("connecting");

  // Stable broadcast — appends to the replay buffer THEN fans out to
  // live listeners. Stable identity so the hydrate effect + WS effect
  // + subscribe all share one implementation.
  const broadcast = useCallback((ev: AgentEvent) => {
    replayBufferRef.current.push(ev);
    listenersRef.current.forEach((l) => {
      try {
        l(ev);
      } catch (err) {
        // A listener throwing must not break fan-out for siblings.
        // eslint-disable-next-line no-console
        console.error("[AgentEventStream] listener threw:", err);
      }
    });
  }, []);

  // Sprint AA.2 — cold-mount hydrate. Fetch the persisted log once and
  // replay each event through broadcast so session.ts folds them into
  // Turn[] exactly like live events. Runs before / in parallel with the
  // WS connect; live events that arrive during hydrate are appended
  // after (WS frames also go through broadcast → buffer), so ordering
  // stays consistent. NOTE: we don't dedup against live events because
  // on a fresh page load the WS only delivers NEW events (post-load),
  // while hydrate delivers the historical ones — no overlap.
  useEffect(() => {
    if (!sessionId || hydratedRef.current) return;
    hydratedRef.current = true;
    let alive = true;
    listSessionEvents(sessionId, 0)
      .then((rows) => {
        if (!alive || rows.length === 0) return;
        for (const r of rows) {
          broadcast({
            session_id: r.session_id,
            kind: r.kind as EventKind,
            at: r.at,
            data: r.data as EventData,
          });
        }
      })
      .catch(() => {
        // Replay is best-effort — a failed fetch just means no history
        // restore; live WS still works. Reset the flag so a later
        // re-render can retry if the session id changes.
      });
    return () => {
      alive = false;
    };
  }, [sessionId, broadcast]);

  // The connection + reconnect lifecycle. Mirrors the previously-proven
  // backoff logic that LivePreviewStack used in isolation, but lives
  // in exactly one place now.
  useEffect(() => {
    if (!sessionId) {
      setStatus("closed");
      return;
    }

    let alive = true;
    let ws: WebSocket | null = null;
    let attempt = 0;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;

    const handleMessage = (m: MessageEvent) => {
      if (!alive) return;
      try {
        const ev = JSON.parse(m.data) as AgentEvent;
        if (ev && ev.kind && ev.data !== undefined) broadcast(ev);
      } catch {
        /* malformed frame — drop silently */
      }
    };

    const scheduleRetry = () => {
      if (!alive) return;
      // 0.6s → 1.2s → 2.4s … capped at 30s. First retry stays in cache.
      const delay = Math.min(600 * 2 ** attempt, 30_000);
      attempt += 1;
      setStatus("connecting");
      retryTimer = setTimeout(connect, delay);
    };

    const connect = () => {
      if (!alive) return;
      try {
        ws = new WebSocket(eventsURL(sessionId));
      } catch {
        scheduleRetry();
        return;
      }
      ws.onopen = () => {
        attempt = 0;
        if (alive) setStatus("open");
      };
      ws.onmessage = handleMessage;
      // close fires after error too — schedule only here so we don't
      // queue two retries for one drop.
      ws.onclose = () => {
        ws = null;
        if (alive) setStatus("closed");
        scheduleRetry();
      };
      ws.onerror = () => {
        /* close handles retry */
      };
    };

    connect();

    return () => {
      alive = false;
      if (retryTimer) clearTimeout(retryTimer);
      if (ws) {
        ws.onopen = null;
        ws.onmessage = null;
        ws.onclose = null;
        ws.onerror = null;
        ws.close();
      }
    };
  }, [sessionId]);

  // subscribe is stable across renders so consumers can pass it to
  // useEffect's deps without re-subscribing on every state change.
  //
  // Sprint AA.2 — on register, FIRST flush the replay buffer to the
  // new listener (so a consumer that mounts after hydrate / after some
  // live events still gets the full history), THEN add it to the live
  // set. This makes mount-order between provider hydrate and consumer
  // subscribe irrelevant: every subscriber sees full history → live.
  const subscribe = useCallback((listener: (ev: AgentEvent) => void) => {
    const buffered = replayBufferRef.current;
    for (const ev of buffered) {
      try {
        listener(ev);
      } catch (err) {
        // eslint-disable-next-line no-console
        console.error("[AgentEventStream] replay listener threw:", err);
      }
    }
    listenersRef.current.add(listener);
    return () => {
      listenersRef.current.delete(listener);
    };
  }, []);

  const value = useMemo<AgentEventStream>(
    () => ({ subscribe, status }),
    [subscribe, status],
  );

  // Stable identity for the deck-gone notifier so consumers don't
  // re-bind effects every render. Falls back to a no-op when the
  // page didn't supply one.
  const deckGoneRef = useRef(onDeckGone);
  deckGoneRef.current = onDeckGone;
  const notifyDeckGone = useCallback(() => {
    deckGoneRef.current?.();
  }, []);

  return (
    <DeckGoneCtx.Provider value={notifyDeckGone}>
      <StreamCtx.Provider value={value}>{children}</StreamCtx.Provider>
    </DeckGoneCtx.Provider>
  );
}
