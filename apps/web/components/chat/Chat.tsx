"use client";

import { useCallback, useEffect, useReducer, useRef } from "react";
import { ArrowDownToLine, RotateCcw, ArrowUpRight } from "lucide-react";

import { eventsURL, postSlideMessage, type SlideJob } from "@/lib/api";
import { PreviewGrid } from "@/components/slides-preview/PreviewGrid";
import { Brief, Phase, PressTicker, type SectionStatus } from "./Bubble";
import { ToolStrip, type ToolCallEntry } from "./ToolStrip";
import { ThoughtCollapse, type Thought } from "./ThoughtCollapse";
import { FollowupInput } from "./FollowupInput";

// Chat is the orchestrator for the editorial generation timeline. It owns
// the WebSocket event stream and turns it into a phase-by-phase view of
// the agent's work. In "agent" mode (default) the reducer captures the
// LLM's reasoning notes and the tool-call lifecycle, then surfaces them
// inside each Phase body via <ToolStrip /> and <ThoughtCollapse />. In
// "pipeline" mode the same component still works — the agent fields just
// stay empty.

// ── State machine ─────────────────────────────────────────────────

type ChatPhase =
  | "planning"
  | "outline_ready"
  | "writing"
  | "rendering"
  | "done"
  | "error";

type ChatState = {
  phase: ChatPhase;
  outlineSlideCount: number;
  outlineTitle: string;
  slidesRendered: number;
  slidesTotal: number;
  errorMsg?: string;

  // Agent-mode bookkeeping for the initial generation only. Pipeline
  // mode never emits these so the arrays stay empty.
  currentStep: number;
  thoughts: Thought[];
  toolCalls: ToolCallEntry[];

  // Follow-up turns the user initiated AFTER the initial deck rendered.
  // Each turn is a self-contained chat unit with its own tool calls and
  // reasoning notes; new step.start / llm.thought / tool.* events while
  // a turn is "running" land in the last turn's arrays.
  turns: Turn[];
  followupBusy: boolean;
  // Bumped every time a turn finishes so the PreviewGrid can append a
  // cache-busting query string to its <img src> URLs.
  previewVersion: number;
};

type Turn = {
  id: string;
  userMessage: string;
  toolCalls: ToolCallEntry[];
  thoughts: Thought[];
  status: "running" | "done" | "error";
  errorMsg?: string;
};

const initialState: ChatState = {
  phase: "planning",
  outlineSlideCount: 0,
  outlineTitle: "",
  slidesRendered: 0,
  slidesTotal: 0,
  currentStep: 0,
  thoughts: [],
  toolCalls: [],
  turns: [],
  followupBusy: false,
  previewVersion: 0,
};

type WSEvent = {
  session_id: string;
  kind: string;
  at: string;
  data?: {
    // slides.* events
    outline_title?: string;
    slide_count?: number;
    slide_index?: number;
    pptx_path?: string;
    // agent loop events
    agent?: string;
    step?: number;
    text?: string;
    tool_calls?: string[];
    tool_name?: string;
    tool_id?: string;
    tool_output?: string;
    tokens?: {
      input?: number;
      output?: number;
      cache_read?: number;
      cache_creation?: number;
    };
    // shared
    error?: string;
    [k: string]: unknown;
  };
};

type Action =
  | { type: "ws"; event: WSEvent }
  | { type: "start_followup"; id: string; userMessage: string };

function reducer(s: ChatState, a: Action): ChatState {
  if (a.type === "start_followup") {
    return {
      ...s,
      followupBusy: true,
      // Reset the step counter so the new turn's step.start frames don't
      // look like they continue from the initial generation's count.
      currentStep: 0,
      turns: [
        ...s.turns,
        {
          id: a.id,
          userMessage: a.userMessage,
          toolCalls: [],
          thoughts: [],
          status: "running",
        },
      ],
    };
  }

  const ev = a.event;
  const d = ev.data ?? {};
  // While a follow-up turn is in flight, agent-loop events belong to that
  // turn; pre-finish (initial generation) they belong to the root state.
  const activeTurnIdx = s.followupBusy ? s.turns.length - 1 : -1;

  switch (ev.kind) {
    // ─── Slides high-level phases (initial gen only) ────────────
    case "slides.outline":
      if (activeTurnIdx >= 0) return s; // ignore during follow-up turns
      return {
        ...s,
        phase: "writing",
        outlineTitle: (d.outline_title as string) ?? "",
        outlineSlideCount: (d.slide_count as number) ?? 0,
      };
    case "slides.render.start":
      if (activeTurnIdx >= 0) return s;
      return {
        ...s,
        phase: "rendering",
        slidesTotal: (d.slide_count as number) ?? s.outlineSlideCount,
        slidesRendered: 0,
      };
    case "slides.content":
      if (activeTurnIdx >= 0) return s;
      return { ...s, slidesRendered: s.slidesRendered + 1 };
    case "slides.render.end":
      if (activeTurnIdx >= 0) return s;
      return { ...s, phase: "done" };

    case "agent.finish":
      if (activeTurnIdx >= 0) {
        // Follow-up turn just completed — mark it done, bump preview
        // version so thumbnails re-fetch, allow another follow-up.
        return {
          ...s,
          followupBusy: false,
          previewVersion: s.previewVersion + 1,
          turns: s.turns.map((t, i) =>
            i === activeTurnIdx ? { ...t, status: "done" } : t,
          ),
        };
      }
      return { ...s, phase: "done" };

    // ─── Agent loop (routed to active turn if in follow-up) ────
    case "step.start": {
      const step = (d.step as number) ?? s.currentStep + 1;
      return { ...s, currentStep: step };
    }

    case "llm.thought": {
      if (!d.text || typeof d.text !== "string" || !d.text.trim()) return s;
      const note: Thought = {
        step: s.currentStep,
        text: d.text,
        inputTokens: d.tokens?.input,
        outputTokens: d.tokens?.output,
        cacheRead: d.tokens?.cache_read,
      };
      if (activeTurnIdx >= 0) {
        return {
          ...s,
          turns: s.turns.map((t, i) =>
            i === activeTurnIdx ? { ...t, thoughts: [...t.thoughts, note] } : t,
          ),
        };
      }
      return { ...s, thoughts: [...s.thoughts, note] };
    }

    case "tool.start": {
      if (!d.tool_id || !d.tool_name) return s;
      const entry: ToolCallEntry = { id: d.tool_id, name: d.tool_name, status: "running" };
      if (activeTurnIdx >= 0) {
        return {
          ...s,
          turns: s.turns.map((t, i) =>
            i === activeTurnIdx ? { ...t, toolCalls: [...t.toolCalls, entry] } : t,
          ),
        };
      }
      return { ...s, toolCalls: [...s.toolCalls, entry] };
    }

    case "tool.end": {
      if (!d.tool_id) return s;
      const status: ToolCallEntry["status"] = d.error ? "error" : "done";
      const patch = (c: ToolCallEntry): ToolCallEntry =>
        c.id === d.tool_id
          ? {
              ...c,
              status,
              output: typeof d.tool_output === "string" ? d.tool_output : c.output,
              error: typeof d.error === "string" && d.error ? d.error : undefined,
            }
          : c;
      if (activeTurnIdx >= 0) {
        return {
          ...s,
          turns: s.turns.map((t, i) =>
            i === activeTurnIdx ? { ...t, toolCalls: t.toolCalls.map(patch) } : t,
          ),
        };
      }
      return { ...s, toolCalls: s.toolCalls.map(patch) };
    }

    case "agent.error":
      if (activeTurnIdx >= 0) {
        return {
          ...s,
          followupBusy: false,
          turns: s.turns.map((t, i) =>
            i === activeTurnIdx
              ? { ...t, status: "error", errorMsg: (d.error as string) ?? "unknown error" }
              : t,
          ),
        };
      }
      return { ...s, phase: "error", errorMsg: (d.error as string) ?? "unknown error" };

    default:
      return s;
  }
}

// ── Visual phase descriptors ──────────────────────────────────────

const VISUAL_PHASES = [
  {
    key: "composition" as const,
    index: "01",
    kicker: "Composition",
    zhTitle: "构思大纲",
    enSubtitle: "Drafting the architecture",
    tools: ["web_research", "tavily_search", "plan_outline"],
  },
  {
    key: "prose" as const,
    index: "02",
    kicker: "Prose",
    zhTitle: "撰写内容",
    enSubtitle: "Setting words to each page",
    tools: ["write_content"],
  },
  {
    key: "press" as const,
    index: "03",
    kicker: "Press",
    zhTitle: "排版印刷",
    enSubtitle: "Inking the layout, plate by plate",
    tools: ["render_deck", "slide_render"],
  },
  {
    key: "issue" as const,
    index: "04",
    kicker: "Issue",
    zhTitle: "成卷",
    enSubtitle: "The volume is ready",
    tools: [] as string[],
  },
];
// NOTE: no outer `as const`. The empty `tools` on the "issue" entry
// otherwise collapses the union of `tools` to `readonly never[]` at the
// type-system level, breaking `vp.tools.includes(c.name)` at the call
// sites below. The per-key `as const` casts on each entry are enough to
// keep VisualKey discriminated.

type VisualKey = (typeof VISUAL_PHASES)[number]["key"];

function statusForPhase(state: ChatState, key: VisualKey): SectionStatus {
  if (state.phase === "error") {
    const errorPhaseKey = currentVisualKey(state);
    if (key === errorPhaseKey) return "error";
    const order = VISUAL_PHASES.map((v) => v.key);
    if (order.indexOf(key) < order.indexOf(errorPhaseKey)) return "done";
    return "pending";
  }

  switch (key) {
    case "composition":
      return state.phase === "planning" ? "running" : "done";
    case "prose":
      if (state.phase === "planning") return "pending";
      if (state.phase === "writing") return "running";
      return "done";
    case "press":
      if (["planning", "writing"].includes(state.phase)) return "pending";
      if (state.phase === "rendering") return "running";
      return "done";
    case "issue":
      return state.phase === "done" ? "done" : "pending";
  }
}

function currentVisualKey(state: ChatState): VisualKey {
  if (state.phase === "planning") return "composition";
  if (state.phase === "writing") return "prose";
  if (state.phase === "rendering") return "press";
  return "issue";
}

const STYLE_LABELS: Record<string, string> = {
  minimalist: "Minimalist",
  corporate: "Corporate",
  "pitch-deck": "Pitch Deck",
  academic: "Academic",
  playful: "Playful",
};

// ── Chat ──────────────────────────────────────────────────────────

export function Chat({
  job,
  sessionId,
  compact = false,
}: {
  job: SlideJob;
  sessionId: string;
  // compact=true means the right-hand <LivePreviewStack> is rendering
  // the actual deck, so the chat side should NOT also show the PNG
  // thumbnail grid (duplicate preview, takes vertical space the timeline
  // would rather use). Colophon (download button) still renders.
  compact?: boolean;
}) {
  const [state, dispatch] = useReducer(reducer, initialState);
  const wsRef = useRef<WebSocket | null>(null);
  const tailRef = useRef<HTMLDivElement | null>(null);

  const startInDone = job.status === "finished";
  const startInError = job.status === "error";

  useEffect(() => {
    if (startInDone) {
      dispatch({ type: "ws", event: { session_id: "", kind: "agent.finish", at: "" } });
      return;
    }
    if (startInError) {
      dispatch({
        type: "ws",
        event: {
          session_id: "",
          kind: "agent.error",
          at: "",
          data: { error: job.error ?? "" },
        },
      });
      return;
    }
    if (!sessionId) return;
    // The WebSocket persists across initial-gen + follow-up turns — the
    // backend reuses the same Hub subscription for both. We just keep
    // dispatching events; the reducer routes them by phase / turn.
    const ws = new WebSocket(eventsURL(sessionId));
    wsRef.current = ws;
    ws.onmessage = (m) => {
      try {
        const ev = JSON.parse(m.data) as WSEvent;
        dispatch({ type: "ws", event: ev });
      } catch {
        /* drop malformed frame */
      }
    };
    return () => ws.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, startInDone, startInError]);

  // Submit handler for the follow-up input. Optimistically records the
  // user's message as a new turn, then POSTs it; the WebSocket will push
  // back step.start / tool.* events the reducer routes into that turn.
  const onFollowup = useCallback(
    async (text: string) => {
      const turnId = `t-${Date.now()}`;
      dispatch({ type: "start_followup", id: turnId, userMessage: text });
      try {
        await postSlideMessage(job.job_id, text);
      } catch (err) {
        // Surface the error on the turn so the user knows the request
        // never reached the agent loop.
        dispatch({
          type: "ws",
          event: {
            session_id: sessionId,
            kind: "agent.error",
            at: "",
            data: { error: err instanceof Error ? err.message : String(err) },
          },
        });
      }
    },
    [job.job_id, sessionId],
  );

  useEffect(() => {
    tailRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [state.phase, state.slidesRendered, state.toolCalls.length]);

  const input = job.input ?? {};
  const styleLabel =
    STYLE_LABELS[input.force_theme || input.style || ""] ?? "";

  // Compact mode collapses the outer page-wrapper paddings — the
  // two-column page already supplies the horizontal margin via its grid
  // gutter — and removes the wide center column max-width.
  return (
    <article
      className={
        compact
          ? "relative w-full px-1 pb-32 pt-6"
          : "relative mx-auto max-w-3xl px-6 pb-40 pt-14 md:max-w-4xl md:px-12"
      }
    >
      <Masthead startedAt={job.started_at} sessionId={sessionId} mode={job.mode} />

      <Brief
        topic={input.topic || job.title || ""}
        audience={input.audience}
        slides={input.slide_count}
        style={styleLabel}
      />

      {VISUAL_PHASES.map((vp) => {
        const status = statusForPhase(state, vp.key);
        if (vp.key === "issue" && status !== "done") return null;

        const phaseTools = state.toolCalls.filter((c) =>
          vp.tools.includes(c.name),
        );
        const phaseThoughts =
          vp.key === currentVisualKey(state) || vp.key === "composition"
            ? state.thoughts.filter((t) =>
                phaseThoughtSteps(state, vp.key).includes(t.step),
              )
            : [];

        return (
          <Phase
            key={vp.key}
            index={vp.index}
            status={status}
            kicker={vp.kicker}
            zhTitle={vp.zhTitle}
            enSubtitle={vp.enSubtitle}
            emphasis={vp.key === "issue"}
          >
            <PhaseBody
              vpKey={vp.key}
              state={state}
              job={job}
              status={status}
              previewVersion={state.previewVersion}
              compact={compact}
            />
            <ToolStrip calls={phaseTools} />
            <ThoughtCollapse thoughts={phaseThoughts} />
          </Phase>
        );
      })}

      {/* Follow-up turns — every chat edit after the initial deck */}
      {state.turns.map((turn, i) => (
        <FollowupTurn
          key={turn.id}
          turn={turn}
          index={i + VISUAL_PHASES.length + 1}
        />
      ))}

      {/* Once the initial generation is done, allow the user to keep
          editing via the chat input. Disabled while a turn is in flight. */}
      {state.phase === "done" && job.mode !== "pipeline" && (
        <FollowupInput busy={state.followupBusy} onSubmit={onFollowup} />
      )}

      <div ref={tailRef} />
    </article>
  );
}

// Renders one follow-up turn as a numbered editorial section, mirroring
// the visual rhythm of the initial-generation phases. The user's message
// is the section's "epigraph"; the agent's tool calls and reasoning sit
// below in the same ToolStrip / ThoughtCollapse primitives.
function FollowupTurn({ turn, index }: { turn: Turn; index: number }) {
  const status: SectionStatus =
    turn.status === "running" ? "running" : turn.status === "error" ? "error" : "done";
  return (
    <Phase
      index={pad2(index)}
      status={status}
      kicker="Revision"
      zhTitle="修订"
      enSubtitle={turn.status === "running" ? "Applying the change…" : "Change applied"}
    >
      <blockquote className="border-l-[3px] border-[color:var(--ink)]/30 pl-5 font-display text-[22px] italic leading-snug text-[color:var(--ink)]">
        {turn.userMessage}
      </blockquote>
      {turn.errorMsg && (
        <p className="mt-3 font-display text-base italic text-red-800">{turn.errorMsg}</p>
      )}
      <ToolStrip calls={turn.toolCalls} />
      <ThoughtCollapse thoughts={turn.thoughts} />
    </Phase>
  );
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`;
}

// phaseThoughtSteps figures out which agent step numbers belong to a given
// visual phase. We map each tool call to its phase, then collect the steps
// where that phase's tools ran. Thoughts emitted ON those steps are the
// model's reasoning that produced those tool calls.
function phaseThoughtSteps(state: ChatState, key: VisualKey): number[] {
  const vp = VISUAL_PHASES.find((v) => v.key === key);
  if (!vp) return [];
  const steps: number[] = [];
  for (const c of state.toolCalls) {
    if (vp.tools.includes(c.name)) {
      // We didn't record step per tool call, so approximate: thoughts
      // from steps 1..N where N is the count of completed tools in this
      // phase. Good enough — the editorial collapse panel groups them
      // under the phase regardless.
      steps.push(state.currentStep);
    }
  }
  // Fall back: include the current step too, so live "thinking" shows up
  // before its tool call completes.
  steps.push(state.currentStep);
  return Array.from(new Set(steps));
}

// ── Masthead ──────────────────────────────────────────────────────

function Masthead({
  startedAt,
  sessionId,
  mode,
}: {
  startedAt?: string;
  sessionId: string;
  mode?: SlideJob["mode"];
}) {
  const date = startedAt
    ? new Date(startedAt).toISOString().slice(0, 10)
    : new Date().toISOString().slice(0, 10);
  const time = startedAt
    ? new Date(startedAt).toISOString().slice(11, 16) + " UTC"
    : "";
  const ref = sessionId ? sessionId.slice(0, 4).toUpperCase() : "————";
  return (
    <header className="mb-14 border-b border-[color:var(--rule)] pb-6">
      <p className="flex flex-wrap items-baseline gap-x-5 gap-y-1 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
        <span>Dream-Waver</span>
        <Sep />
        <span>Composition Log</span>
        <Sep />
        <span className="tabular-nums">No. {ref}</span>
        <Sep />
        <span className="tabular-nums">{date}</span>
        {time && (
          <>
            <Sep />
            <span className="tabular-nums">{time}</span>
          </>
        )}
        {mode && (
          <>
            <Sep />
            <span className="tabular-nums text-[color:var(--vermillion)]">
              {mode === "agent" ? "Agent" : "Pipeline"} mode
            </span>
          </>
        )}
      </p>
    </header>
  );
}

function Sep() {
  return (
    <span aria-hidden className="text-[color:var(--ink-faint)]">
      ·
    </span>
  );
}

// ── Per-phase body content ─────────────────────────────────────────

function PhaseBody({
  vpKey,
  state,
  job,
  status,
  previewVersion,
  compact = false,
}: {
  vpKey: VisualKey;
  state: ChatState;
  job: SlideJob;
  status: SectionStatus;
  previewVersion: number;
  compact?: boolean;
}) {
  if (status === "error") {
    return (
      <p className="font-display text-base italic text-red-800">
        {state.errorMsg || job.error || "Unknown failure."}
      </p>
    );
  }

  switch (vpKey) {
    case "composition":
      if (status === "running") {
        return (
          <p className="font-display italic">
            Analysing topic, deciding chapters &amp; rhythm…
          </p>
        );
      }
      return (
        <div className="space-y-3">
          {state.outlineTitle && (
            <p>
              <span className="font-display italic text-[color:var(--ink)]">
                &ldquo;{state.outlineTitle}&rdquo;
              </span>
            </p>
          )}
          <p className="font-mono-jb text-[11px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
            {state.outlineSlideCount} chapters set
          </p>
        </div>
      );

    case "prose":
      if (status === "pending") return null;
      if (status === "running") {
        return (
          <p className="font-display italic">
            Drafting each page from the outline…
          </p>
        );
      }
      return (
        <p className="font-mono-jb text-[11px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
          Manuscript ready
        </p>
      );

    case "press":
      if (status === "pending") return null;
      return (
        <PressTicker
          now={state.slidesRendered}
          total={state.slidesTotal || job.slide_count || 1}
        />
      );

    case "issue":
      return (
        <div className="mt-2">
          {/* In compact mode the right-pane LivePreviewStack is already
              showing the live, editable deck — rendering the PNG
              thumbnail grid here would just duplicate the surface. */}
          {!compact && job.preview_urls?.length ? (
            <PreviewGrid urls={job.preview_urls} cacheBust={previewVersion} />
          ) : null}
          <Colophon job={job} />
        </div>
      );
  }
}

// ── Colophon: the action bar at the foot of the final "Issue" section ─

function Colophon({ job }: { job: SlideJob }) {
  return (
    <footer className="mt-10 flex flex-wrap items-center gap-x-3 gap-y-3 border-t border-[color:var(--rule)] pt-6">
      {job.download_url && (
        <a
          href={job.download_url}
          className="group inline-flex items-center gap-2.5 bg-[color:var(--ink)] px-5 py-3 text-[color:var(--paper)] transition-colors hover:bg-[color:var(--vermillion)]"
        >
          <span className="font-mono-jb text-[10px] font-medium uppercase tracking-[0.22em]">
            Download .pptx
          </span>
          <ArrowDownToLine
            size={14}
            strokeWidth={1.6}
            className="transition-transform group-hover:translate-y-0.5"
          />
        </a>
      )}
      <a
        href="/slides/new"
        className="group inline-flex items-center gap-2 border border-[color:var(--rule)] px-5 py-3 text-[color:var(--ink)] transition-colors hover:border-[color:var(--ink)]/30 hover:bg-white/60"
      >
        <RotateCcw size={13} strokeWidth={1.6} />
        <span className="font-mono-jb text-[10px] font-medium uppercase tracking-[0.22em]">
          New Edition
        </span>
      </a>
      <a
        href="/"
        className="ml-auto inline-flex items-center gap-1.5 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)] transition-colors hover:text-[color:var(--ink)]"
      >
        <span>Index</span>
        <ArrowUpRight size={11} strokeWidth={1.6} />
      </a>
    </footer>
  );
}
