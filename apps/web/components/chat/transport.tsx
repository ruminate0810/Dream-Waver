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

import { eventsURL } from "@/lib/api";

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
  | "wizard.step";

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

  const [status, setStatus] = useState<ConnectionStatus>("connecting");

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

    const broadcast = (ev: AgentEvent) => {
      listenersRef.current.forEach((l) => {
        try {
          l(ev);
        } catch (err) {
          // A listener throwing must not break the fan-out for siblings.
          // Surface to the dev console; production builds drop it.
          // eslint-disable-next-line no-console
          console.error("[AgentEventStream] listener threw:", err);
        }
      });
    };

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
  const subscribe = useCallback((listener: (ev: AgentEvent) => void) => {
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
