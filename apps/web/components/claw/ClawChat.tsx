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
import { ArrowUp, Loader2, User2, Bot } from "lucide-react";
import clsx from "clsx";

import { postClawMessage, type ClawRun } from "@/lib/api";
import {
  useAgentEventStream,
  type AgentEvent,
  type EventKind,
} from "@/components/chat/transport";
import {
  ToolStrip,
  type ToolCallEntry,
  type ToolCallStatus,
} from "@/components/chat/ToolStrip";

// ClawChat is the process timeline for /claw/[id]. Structurally a sibling of
// GameChat: a flat list of bubbles (no nested turn model). It folds the
// generic agent events — llm.token / llm.thought / tool.start / tool.end /
// agent.finish|error — into bubbles + a ToolStrip. The claw.* events
// (plan / task.update / artifact.updated) are deliberately IGNORED here:
// the page owns the plan checklist + artifact panel, and WorkerDesk owns
// the worker animation, each subscribing to the same stream independently.

type Bubble =
  | { kind: "user"; text: string; id: string }
  | {
      kind: "assistant";
      text: string;
      status: "thinking" | "done";
      id: string;
      tools?: ToolCallEntry[];
      streaming?: boolean;
    }
  | { kind: "error"; text: string; id: string };

export function ClawChat({
  run,
  onPendingEdit,
}: {
  run: ClawRun;
  /** Fired after a follow-up POST succeeds so the page restarts polling. */
  onPendingEdit?: () => void;
}) {
  const stream = useAgentEventStream();
  const [bubbles, setBubbles] = useState<Bubble[]>(() => seedBubbles(run));
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const inFlightRef = useRef<boolean>(run.status === "running");
  const scrollRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const handle = (ev: AgentEvent) => {
      const k: EventKind = ev.kind;
      if (k === "llm.token") {
        const delta = ev.data.text ?? "";
        if (!delta) return;
        setBubbles((prev) => appendStreamingDelta(prev, delta));
      } else if (k === "llm.thought") {
        const text = ev.data.text?.trim();
        if (!text) return;
        setBubbles((prev) => upsertLatestAssistantText(prev, text));
      } else if (k === "tool.start") {
        const name = ev.data.tool_name;
        const id = ev.data.tool_id;
        if (!name || !id) return;
        setBubbles((prev) =>
          upsertToolCall(prev, { id, name, status: "running", input: ev.data.tool_input }),
        );
      } else if (k === "tool.end") {
        const id = ev.data.tool_id;
        if (!id) return;
        const isErr = !!ev.data.error;
        setBubbles((prev) =>
          patchToolCall(prev, id, {
            status: (isErr ? "error" : "done") as ToolCallStatus,
            output: ev.data.tool_output,
            error: ev.data.error,
            durationMs: ev.data.duration_ms,
          }),
        );
      } else if (k === "agent.finish") {
        setBubbles((prev) => finalizeLatestAssistant(prev));
        inFlightRef.current = false;
      } else if (k === "agent.error") {
        const msg = ev.data.error || "任务失败。";
        setBubbles((prev) => [
          ...stripPendingAssistant(prev),
          { kind: "error", text: msg, id: cryptoRandomId() },
        ]);
        inFlightRef.current = false;
      }
      // claw.* events intentionally ignored here (page + WorkerDesk own them).
    };
    return stream.subscribe(handle);
  }, [stream]);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: "smooth" });
  }, [bubbles.length, sending]);

  useEffect(() => {
    if (run.status === "running" && !inFlightRef.current) {
      inFlightRef.current = true;
      setBubbles((prev) => {
        const tail = prev[prev.length - 1];
        if (tail && tail.kind === "assistant" && tail.status === "thinking") return prev;
        return [
          ...prev,
          { kind: "assistant", text: "正在处理你的新要求…", status: "thinking", id: cryptoRandomId() },
        ];
      });
    }
  }, [run.status]);

  const onSubmit = useCallback(
    async (e?: FormEvent | KeyboardEvent) => {
      e?.preventDefault();
      const text = draft.trim();
      if (!text || sending) return;
      setSending(true);
      const userBubble: Bubble = { kind: "user", text, id: cryptoRandomId() };
      const pendingBubble: Bubble = {
        kind: "assistant",
        text: "正在处理你的新要求…",
        status: "thinking",
        id: cryptoRandomId(),
      };
      setBubbles((prev) => [...prev, userBubble, pendingBubble]);
      setDraft("");
      try {
        await postClawMessage(run.job_id, text);
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
    [draft, sending, run.job_id, onPendingEdit],
  );

  const status = useMemo(() => {
    const tail = bubbles[bubbles.length - 1];
    if (tail?.kind === "assistant" && tail.status === "thinking") return "thinking";
    if (run.status === "running") return "thinking";
    if (run.status === "error") return "error";
    return "ready";
  }, [bubbles, run.status]);

  const running = sending || run.status === "running";

  return (
    <div className="flex h-full flex-col">
      <div className="border-b-2 border-ink px-1 pb-3 pt-2">
        <p className="font-pixel text-[0.55rem] tracking-wide text-muted">
          § Process · {status === "thinking" ? "Working" : status === "error" ? "Halted" : "Idle"}
        </p>
        <h2 className="mt-1.5 font-mono text-[18px] font-bold leading-tight tracking-tight text-ink">
          {run.title || "未命名任务"}
        </h2>
      </div>

      <div ref={scrollRef} className="flex-1 overflow-y-auto px-1 pb-6 pt-4 [scrollbar-width:thin]">
        <div className="flex flex-col gap-4">
          {bubbles.map((b) => (
            <BubbleRow key={b.id} bubble={b} />
          ))}
        </div>
      </div>

      <form
        onSubmit={onSubmit}
        className="border-t-2 border-ink bg-paper/85 px-1 pb-3 pt-3 backdrop-blur-[2px]"
      >
        <div className="flex items-end gap-2 rounded-pixel border-2 border-ink bg-surface px-3 py-2 shadow-pixel transition-shadow focus-within:shadow-pixel-lg">
          <textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) onSubmit(e);
            }}
            disabled={running}
            rows={2}
            placeholder="追问或继续派活，例如「把价格表改成人民币，并补一段风险提示」"
            className="flex-1 resize-none bg-transparent font-mono text-[14px] leading-relaxed text-ink placeholder:text-muted focus:outline-none disabled:opacity-60"
          />
          <button
            type="submit"
            disabled={!draft.trim() || running}
            className={clsx(
              "flex h-9 w-9 shrink-0 items-center justify-center rounded-pixel border-2 transition-transform",
              !draft.trim() || running
                ? "cursor-not-allowed border-line-2 bg-surface-2 text-line-2"
                : "border-ink bg-accent text-white shadow-pixel-sm hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none",
            )}
            aria-label="Send"
          >
            {running ? (
              <Loader2 size={14} strokeWidth={2} className="animate-spin" />
            ) : (
              <ArrowUp size={14} strokeWidth={2} />
            )}
          </button>
        </div>
        <p className="mt-2 font-mono text-[11px] text-muted">Enter 发送 · Shift+Enter 换行</p>
      </form>
    </div>
  );
}

function BubbleRow({ bubble }: { bubble: Bubble }) {
  if (bubble.kind === "user") {
    return (
      <div className="flex gap-3">
        <Avatar variant="user" />
        <div className="flex-1 pt-1">
          <p className="font-mono text-[14.5px] leading-relaxed text-ink">{bubble.text}</p>
        </div>
      </div>
    );
  }
  if (bubble.kind === "error") {
    return (
      <div className="flex gap-3">
        <Avatar variant="assistant" />
        <div className="flex-1 pt-1">
          <p className="rounded-pixel border-2 border-ink bg-[#fdece9] px-3 py-2 font-mono text-[13px] leading-relaxed text-[#a23a2a]">
            {bubble.text}
          </p>
        </div>
      </div>
    );
  }
  if (bubble.status === "thinking" && bubble.streaming) {
    return (
      <div className="flex gap-3">
        <Avatar variant="assistant" />
        <div className="min-w-0 flex-1 pt-1">
          <pre className="m-0 max-h-24 overflow-hidden whitespace-pre-wrap break-all rounded-pixel border border-line-2 bg-surface-2/80 px-2.5 py-2 font-mono text-[10px] leading-relaxed text-muted">
            {tailLines(bubble.text, 6)}
            <span className="ml-1 inline-flex">
              <Loader2 size={9} strokeWidth={2} className="animate-spin text-accent" />
            </span>
          </pre>
          {bubble.tools && bubble.tools.length > 0 ? <ToolStrip calls={bubble.tools} /> : null}
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
            "font-mono text-[14.5px] leading-relaxed",
            bubble.status === "thinking" ? "text-ink-2" : "text-ink",
          )}
        >
          {bubble.text}
          {bubble.status === "thinking" ? (
            <span className="ml-2 inline-flex items-center">
              <Loader2 size={11} strokeWidth={2} className="animate-spin text-accent" />
            </span>
          ) : null}
        </p>
        {bubble.tools && bubble.tools.length > 0 ? <ToolStrip calls={bubble.tools} /> : null}
      </div>
    </div>
  );
}

function tailLines(s: string, n: number): string {
  const lines = s.split("\n");
  if (lines.length <= n) return s;
  return lines.slice(-n).join("\n");
}

function Avatar({ variant }: { variant: "user" | "assistant" }) {
  const isUser = variant === "user";
  return (
    <div
      className={clsx(
        "mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full border-2",
        isUser ? "border-ink bg-surface text-ink-2" : "border-ink bg-accent text-white",
      )}
    >
      {isUser ? <User2 size={13} strokeWidth={1.8} /> : <Bot size={13} strokeWidth={1.8} />}
    </div>
  );
}

function seedBubbles(run: ClawRun): Bubble[] {
  const out: Bubble[] = [{ kind: "user", text: run.prompt, id: "seed-user" }];
  if (run.status === "finished") {
    out.push({
      kind: "assistant",
      text: "已完成 — 报告在右侧,可下载或继续追问 →",
      status: "done",
      id: "seed-asst",
    });
  } else if (run.status === "error") {
    out.push({ kind: "error", text: run.error || "任务失败", id: "seed-err" });
  } else {
    out.push({ kind: "assistant", text: "正在规划并开工…", status: "thinking", id: "seed-asst" });
  }
  return out;
}

function upsertToolCall(prev: Bubble[], entry: ToolCallEntry): Bubble[] {
  for (let i = prev.length - 1; i >= 0; i--) {
    const b = prev[i];
    if (b.kind !== "assistant") continue;
    const tools = b.tools ?? [];
    if (tools.find((t) => t.id === entry.id)) return prev;
    const next = prev.slice();
    next[i] = { ...b, tools: [...tools, entry] };
    return next;
  }
  return [
    ...prev,
    { kind: "assistant", text: "正在处理…", status: "thinking", id: cryptoRandomId(), tools: [entry] },
  ];
}

function patchToolCall(prev: Bubble[], id: string, patch: Partial<ToolCallEntry>): Bubble[] {
  for (let i = prev.length - 1; i >= 0; i--) {
    const b = prev[i];
    if (b.kind !== "assistant" || !b.tools) continue;
    const idx = b.tools.findIndex((t) => t.id === id);
    if (idx < 0) continue;
    const tools = b.tools.slice();
    tools[idx] = { ...tools[idx], ...patch };
    const next = prev.slice();
    next[i] = { ...b, tools };
    return next;
  }
  return prev;
}

function appendStreamingDelta(prev: Bubble[], delta: string): Bubble[] {
  for (let i = prev.length - 1; i >= 0; i--) {
    const b = prev[i];
    if (b.kind !== "assistant" || b.status !== "thinking") continue;
    const next = prev.slice();
    next[i] = { ...b, streaming: true, text: b.streaming ? b.text + delta : delta };
    return next;
  }
  return [
    ...prev,
    { kind: "assistant", text: delta, status: "thinking", streaming: true, id: cryptoRandomId() },
  ];
}

function upsertLatestAssistantText(prev: Bubble[], text: string): Bubble[] {
  for (let i = prev.length - 1; i >= 0; i--) {
    const b = prev[i];
    if (b.kind === "assistant" && b.status === "thinking") {
      const next = prev.slice();
      next[i] = { ...b, text, streaming: false };
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
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return Math.random().toString(36).slice(2);
}
