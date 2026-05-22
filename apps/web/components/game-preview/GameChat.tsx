"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { ArrowUp, Loader2, User2, Gamepad2 } from "lucide-react";
import clsx from "clsx";

import { postGameMessage, type GameJob } from "@/lib/api";
import {
  useAgentEventStream,
  type AgentEvent,
  type EventKind,
} from "@/components/chat/transport";

// GameChat is the chat surface for /code/[id]. It's deliberately simpler
// than the slides ChatThread: there are no per-slide events, no tool
// strip, no critic. Each "turn" is just the user's prompt → assistant's
// one-sentence description → "done". Follow-up edits append another turn.
//
// We render straight from a flat list of bubbles instead of building a
// turn data model, because there's no nested structure to render.

type Bubble =
  | { kind: "user"; text: string; id: string }
  | { kind: "assistant"; text: string; status: "thinking" | "done"; id: string }
  | { kind: "error"; text: string; id: string };

export function GameChat({
  job,
  onPendingEdit,
}: {
  job: GameJob;
  /**
   * Fired right after a follow-up POST succeeds. The page lifts this to
   * restart its polling loop, which had stopped once the job first
   * reached "finished". Without it the workspace would stay frozen on
   * the prior artifact until a manual refresh.
   */
  onPendingEdit?: () => void;
}) {
  const stream = useAgentEventStream();
  const [bubbles, setBubbles] = useState<Bubble[]>(() => seedBubbles(job));
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  // Track whether the *current* turn is still in flight so we know when
  // to flip the trailing assistant bubble from "thinking" to "done".
  const inFlightRef = useRef<boolean>(job.status === "running");
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const formRef = useRef<HTMLFormElement | null>(null);

  // ── Subscribe to live agent events ───────────────────────────────────
  useEffect(() => {
    const handle = (ev: AgentEvent) => {
      const k: EventKind = ev.kind;
      if (k === "llm.thought") {
        const text = ev.data.text?.trim();
        if (!text) return;
        setBubbles((prev) => upsertLatestAssistantText(prev, text));
      } else if (k === "agent.finish") {
        setBubbles((prev) => finalizeLatestAssistant(prev));
        inFlightRef.current = false;
      } else if (k === "agent.error") {
        const msg = ev.data.error || "Generation failed.";
        setBubbles((prev) => [
          ...stripPendingAssistant(prev),
          { kind: "error", text: msg, id: cryptoRandomId() },
        ]);
        inFlightRef.current = false;
      }
    };
    return stream.subscribe(handle);
  }, [stream]);

  // ── Keep scrolled to bottom ─────────────────────────────────────────
  useEffect(() => {
    scrollRef.current?.scrollTo({
      top: scrollRef.current.scrollHeight,
      behavior: "smooth",
    });
  }, [bubbles.length, sending]);

  // When the *job* flips back to running after a follow-up, append a
  // pending assistant bubble so the user sees "thinking" feedback even
  // before any llm.thought event arrives.
  useEffect(() => {
    if (job.status === "running" && !inFlightRef.current) {
      inFlightRef.current = true;
      setBubbles((prev) => {
        // Don't add a pending bubble if the last one is already pending.
        const tail = prev[prev.length - 1];
        if (tail && tail.kind === "assistant" && tail.status === "thinking") {
          return prev;
        }
        return [
          ...prev,
          {
            kind: "assistant",
            text: "正在为你修改这个游戏…",
            status: "thinking",
            id: cryptoRandomId(),
          },
        ];
      });
    }
  }, [job.status]);

  const onSubmit = useCallback(
    async (e?: FormEvent | KeyboardEvent) => {
      e?.preventDefault();
      const text = draft.trim();
      if (!text || sending) return;
      setSending(true);
      const userBubble: Bubble = {
        kind: "user",
        text,
        id: cryptoRandomId(),
      };
      const pendingBubble: Bubble = {
        kind: "assistant",
        text: "正在为你修改这个游戏…",
        status: "thinking",
        id: cryptoRandomId(),
      };
      setBubbles((prev) => [...prev, userBubble, pendingBubble]);
      setDraft("");
      try {
        await postGameMessage(job.job_id, text);
        inFlightRef.current = true;
        onPendingEdit?.();
      } catch (err) {
        setBubbles((prev) => [
          ...stripPendingAssistant(prev),
          {
            kind: "error",
            text: err instanceof Error ? err.message : "请求失败",
            id: cryptoRandomId(),
          },
        ]);
      } finally {
        setSending(false);
      }
    },
    [draft, sending, job.job_id, onPendingEdit],
  );

  const status = useMemo(() => {
    const tail = bubbles[bubbles.length - 1];
    if (tail?.kind === "assistant" && tail.status === "thinking") return "thinking";
    if (job.status === "running") return "thinking";
    if (job.status === "error") return "error";
    return "ready";
  }, [bubbles, job.status]);

  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-[color:var(--rule)] px-1 pb-3 pt-1">
        <p className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
          Conversation · {status === "thinking" ? "Composing" : status === "error" ? "Halted" : "Idle"}
        </p>
        <h2 className="mt-1 font-display text-[20px] leading-tight text-[color:var(--ink)]">
          {job.title || "Untitled game"}
        </h2>
      </div>

      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto px-1 pb-6 pt-4 [scrollbar-width:thin]"
      >
        <div className="flex flex-col gap-4">
          {bubbles.map((b) => (
            <BubbleRow key={b.id} bubble={b} />
          ))}
        </div>
      </div>

      <form
        ref={formRef}
        onSubmit={onSubmit}
        className="border-t border-[color:var(--rule)] bg-[color:var(--paper)]/85 px-1 pb-2 pt-3"
      >
        <div className="flex items-end gap-2 border border-[color:var(--rule)] bg-white px-3 py-2 focus-within:border-[color:var(--ink)]/40">
          <textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) onSubmit(e);
            }}
            disabled={sending || job.status === "running"}
            rows={2}
            placeholder="想改点什么？比如「让小蛇撞到自己时屏幕红色闪烁」"
            className="flex-1 resize-none bg-transparent font-display text-[15px] leading-snug text-[color:var(--ink)] placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-60"
          />
          <button
            type="submit"
            disabled={!draft.trim() || sending || job.status === "running"}
            className={clsx(
              "flex h-9 w-9 items-center justify-center rounded transition-colors",
              !draft.trim() || sending || job.status === "running"
                ? "cursor-not-allowed bg-zinc-100 text-zinc-300"
                : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)]",
            )}
            aria-label="Send"
          >
            {sending || job.status === "running" ? (
              <Loader2 size={14} strokeWidth={2} className="animate-spin" />
            ) : (
              <ArrowUp size={14} strokeWidth={2} />
            )}
          </button>
        </div>
        <p className="mt-1.5 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
          Enter 发送 · Shift+Enter 换行
        </p>
      </form>
    </div>
  );
}

// ── Bubble renderer ────────────────────────────────────────────────────

function BubbleRow({ bubble }: { bubble: Bubble }) {
  if (bubble.kind === "user") {
    return (
      <div className="flex gap-3">
        <Avatar variant="user" />
        <div className="flex-1 pt-1">
          <p className="font-display text-[15px] leading-snug text-[color:var(--ink)]">
            {bubble.text}
          </p>
        </div>
      </div>
    );
  }
  if (bubble.kind === "error") {
    return (
      <div className="flex gap-3">
        <Avatar variant="assistant" />
        <div className="flex-1 pt-1">
          <p className="font-display text-[15px] italic leading-snug text-red-800">
            {bubble.text}
          </p>
        </div>
      </div>
    );
  }
  return (
    <div className="flex gap-3">
      <Avatar variant="assistant" />
      <div className="flex-1 pt-1">
        <p
          className={clsx(
            "font-display text-[15px] leading-snug",
            bubble.status === "thinking"
              ? "italic text-[color:var(--ink-soft)]"
              : "text-[color:var(--ink)]",
          )}
        >
          {bubble.text}
          {bubble.status === "thinking" ? (
            <span className="ml-2 inline-flex items-center">
              <Loader2 size={11} strokeWidth={2} className="animate-spin" />
            </span>
          ) : null}
        </p>
      </div>
    </div>
  );
}

function Avatar({ variant }: { variant: "user" | "assistant" }) {
  const isUser = variant === "user";
  return (
    <div
      className={clsx(
        "mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full border",
        isUser
          ? "border-[color:var(--rule)] bg-white text-[color:var(--ink-soft)]"
          : "border-[color:var(--ink)] bg-[color:var(--ink)] text-[color:var(--paper)]",
      )}
    >
      {isUser ? (
        <User2 size={13} strokeWidth={1.8} />
      ) : (
        <Gamepad2 size={13} strokeWidth={1.8} />
      )}
    </div>
  );
}

// ── Helpers ────────────────────────────────────────────────────────────

function seedBubbles(job: GameJob): Bubble[] {
  const out: Bubble[] = [
    { kind: "user", text: job.input.prompt, id: "seed-user" },
  ];
  // If the job already finished by the time we mount (rare, but possible
  // when navigating back to an old job), seed a done bubble; otherwise
  // start with a thinking placeholder so the UI isn't empty.
  if (job.status === "finished") {
    out.push({
      kind: "assistant",
      text: "已经为你完成了游戏 — 在右侧开玩 →",
      status: "done",
      id: "seed-asst",
    });
  } else if (job.status === "error") {
    out.push({
      kind: "error",
      text: job.error || "Generation failed",
      id: "seed-err",
    });
  } else {
    out.push({
      kind: "assistant",
      text: "正在构思你的游戏…",
      status: "thinking",
      id: "seed-asst",
    });
  }
  return out;
}

// upsertLatestAssistantText replaces the text of the trailing thinking
// bubble (if any) instead of appending a new one — keeps the chat compact
// when llm.thought fires after the placeholder is already on screen.
function upsertLatestAssistantText(prev: Bubble[], text: string): Bubble[] {
  for (let i = prev.length - 1; i >= 0; i--) {
    const b = prev[i];
    if (b.kind === "assistant" && b.status === "thinking") {
      const next = prev.slice();
      next[i] = { ...b, text };
      return next;
    }
  }
  return [...prev, { kind: "assistant", text, status: "thinking", id: cryptoRandomId() }];
}

function finalizeLatestAssistant(prev: Bubble[]): Bubble[] {
  for (let i = prev.length - 1; i >= 0; i--) {
    const b = prev[i];
    if (b.kind === "assistant" && b.status === "thinking") {
      const next = prev.slice();
      next[i] = { ...b, status: "done" };
      return next;
    }
  }
  return prev;
}

function stripPendingAssistant(prev: Bubble[]): Bubble[] {
  for (let i = prev.length - 1; i >= 0; i--) {
    const b = prev[i];
    if (b.kind === "assistant" && b.status === "thinking") {
      const next = prev.slice();
      next.splice(i, 1);
      return next;
    }
  }
  return prev;
}

function cryptoRandomId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return Math.random().toString(36).slice(2);
}
