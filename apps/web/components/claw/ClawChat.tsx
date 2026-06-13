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
import { AnimatePresence, motion } from "framer-motion";
import clsx from "clsx";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

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
import { narrationFor, nextSteps, workerColor, workerZh } from "./narrate";

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
  | { kind: "say"; worker: string; text: string; id: string }
  | { kind: "debate"; agreed: string; id: string }
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
  const saidRef = useRef<Set<string>>(new Set());

  // 交付汇报 — the moment the run flips to finished, the coordinator hands
  // over whatever was actually produced (not always a report!): report /
  // figures / videos / deck, each mentioned only if it exists.
  const deliveredRef = useRef(false);
  useEffect(() => {
    if (run.status !== "finished" || deliveredRef.current) return;
    deliveredRef.current = true;
    const parts: string[] = [];
    if ((run.artifact_version ?? 0) > 0) parts.push(`报告 v${run.artifact_version}`);
    if ((run.figures?.length ?? 0) > 0) parts.push(`配图 ${run.figures!.length} 张`);
    if ((run.videos?.length ?? 0) > 0) parts.push(`短视频 ${run.videos!.length} 段`);
    if (run.deck) parts.push(`幻灯片 ${run.deck.slide_count ?? "一"} 页`);
    const text =
      parts.length > 0
        ? `🔔 交付!这单的产出:${parts.join("、")} — 都放进作品包了(右侧窗口 / dock 可开)。要继续加工,点下面的「下一步」。`
        : "🔔 收工!这单没有产出文件 — 结论都在上面的对话里。需要落成报告的话,和我说一声。";
    setBubbles((prev) => [...prev, { kind: "say", worker: "coordinator", text, id: cryptoRandomId() }]);
  }, [run.status, run.artifact_version, run.figures, run.videos, run.deck]);

  // 澄清 — instead of a rigid form card, the 调度员 ASKS in the chat: when the
  // run pauses for clarification, drop a conversational coordinator bubble
  // with the questions; the user just types the answer in the input below
  // (which resumes the run). Re-armable if a later turn pauses again.
  const clarifyShownRef = useRef(false);
  useEffect(() => {
    const qs = run.clarification_questions ?? [];
    if (run.status === "awaiting_input" && qs.length > 0) {
      if (clarifyShownRef.current) return;
      clarifyShownRef.current = true;
      const body = qs.map((q, i) => `${i + 1}. ${q}`).join("\n");
      const text = `开工前我想先跟你对一下,这样产出更贴合你 👇\n\n${body}\n\n回我一句就行 —— 或者直接说「开干」,我自己来定。`;
      setBubbles((prev) =>
        prev.some((b) => b.id === "clarify") ? prev : [...prev, { kind: "say", worker: "coordinator", text, id: "clarify" }],
      );
    } else if (run.status !== "awaiting_input") {
      clarifyShownRef.current = false;
      setBubbles((prev) => prev.filter((b) => b.id !== "clarify"));
    }
  }, [run.status, run.clarification_questions]);

  useEffect(() => {
    const handle = (ev: AgentEvent) => {
      const k: EventKind = ev.kind;
      // 角色发声 — interleave first-person team lines from the real events.
      const say = narrationFor(ev, saidRef.current);
      if (say) {
        setBubbles((prev) => [
          ...prev,
          { kind: "say", worker: say.worker, text: say.text, id: cryptoRandomId() },
        ]);
      }
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
      } else if (k === "claw.debate") {
        // 协商: render each proposal as a worker line + the consensus card.
        const raw = ev.data.claw_debate_json;
        if (raw) {
          try {
            const d = JSON.parse(raw) as {
              proposals?: { role: string; text: string }[];
              agreed?: string;
            };
            const add: Bubble[] = (d.proposals ?? []).map((p) => ({
              kind: "say" as const,
              worker: p.role,
              text: p.text,
              id: cryptoRandomId(),
            }));
            if (d.agreed?.trim()) {
              add.push({ kind: "debate", agreed: d.agreed.trim(), id: cryptoRandomId() });
            }
            if (add.length) setBubbles((prev) => [...prev, ...add]);
          } catch {
            /* malformed payload — ignore */
          }
        }
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

  const send = useCallback(
    async (raw: string) => {
      const text = raw.trim();
      if (!text || sending) return;
      setSending(true);
      saidRef.current = new Set(); // a fresh turn — let the team narrate again
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
    [sending, run.job_id, onPendingEdit],
  );

  const onSubmit = useCallback(
    (e?: FormEvent | KeyboardEvent) => {
      e?.preventDefault();
      void send(draft);
    },
    [draft, send],
  );

  const status = useMemo(() => {
    const tail = bubbles[bubbles.length - 1];
    if (tail?.kind === "assistant" && tail.status === "thinking") return "thinking";
    if (run.status === "running") return "thinking";
    if (run.status === "error") return "error";
    return "ready";
  }, [bubbles, run.status]);

  const running = sending || run.status === "running";
  const awaiting = run.status === "awaiting_input";

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
          <AnimatePresence initial={false}>
            {bubbles.map((b) => (
              <motion.div
                key={b.id}
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, height: 0, marginTop: -16 }}
                transition={{ duration: 0.18, ease: "easeOut" }}
              >
                <BubbleRow bubble={b} />
              </motion.div>
            ))}
          </AnimatePresence>
        </div>
      </div>

      <AnimatePresence>
        {run.status === "finished" && (run.artifact_version ?? 0) > 0 && !running && (
          <motion.div
            className="overflow-hidden border-t-2 border-line px-1 pb-1 pt-2"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: "auto" }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.22, ease: "easeOut" }}
          >
            <p className="mb-1.5 font-mono text-[10px] font-bold tracking-wide text-muted">下一步 ↓ 一点就派活</p>
            <div className="flex flex-wrap gap-1.5">
              {nextSteps(run).map((s, i) => (
                <motion.button
                  key={s.label}
                  type="button"
                  disabled={sending}
                  onClick={() => void send(s.text)}
                  initial={{ opacity: 0, y: 6 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: 0.05 + i * 0.04, duration: 0.16 }}
                  className="rounded-pixel border-2 border-line-2 bg-surface px-2.5 py-1 font-mono text-[11px] font-semibold text-ink-2 transition-colors hover:border-ink hover:bg-accent-soft hover:text-accent disabled:opacity-50"
                  title={s.text}
                >
                  {s.label}
                </motion.button>
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>

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
            placeholder={awaiting ? "在这儿回答调度员的问题就能开工，或直接说「开干」…" : "追问或继续派活，例如「把价格表改成人民币，并补一段风险提示」"}
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
        {awaiting ? (
          <div className="mt-2 flex items-center justify-between gap-2">
            <span className="font-mono text-[11px] text-accent">↑ 调度员在等你回话</span>
            <button
              type="button"
              onClick={() => void send("开干,按你的判断直接做。")}
              disabled={sending}
              className="rounded-pixel border-2 border-ink bg-surface px-2 py-0.5 font-mono text-[11px] font-semibold text-ink-2 shadow-pixel-sm transition-colors hover:text-ink disabled:opacity-50"
            >
              直接开干 →
            </button>
          </div>
        ) : (
          <p className="mt-2 font-mono text-[11px] text-muted">Enter 发送 · Shift+Enter 换行</p>
        )}
      </form>
    </div>
  );
}

function BubbleRow({ bubble }: { bubble: Bubble }) {
  if (bubble.kind === "debate") {
    return (
      <div className="rounded-pixel border-2 border-accent/45 bg-accent-soft/55 px-3 py-2.5">
        <p className="mb-1 font-mono text-[10px] font-bold tracking-wide text-accent">⚖ 开工例会 · 一致方案</p>
        <p className="whitespace-pre-wrap font-mono text-[13px] leading-relaxed text-ink">{bubble.agreed}</p>
      </div>
    );
  }
  if (bubble.kind === "say") {
    return (
      <div className="flex items-start gap-2.5">
        <span
          className="mt-0.5 grid h-7 w-7 shrink-0 place-items-center rounded-pixel border-2 border-ink font-mono text-[10px] font-bold text-white"
          style={{ background: workerColor(bubble.worker) }}
          aria-hidden
        >
          {workerZh(bubble.worker).slice(0, 1)}
        </span>
        <div className="flex-1 pt-0.5">
          <span className="font-mono text-[11px] font-bold text-ink-2">{workerZh(bubble.worker)}</span>
          <div className="font-mono text-[13.5px] leading-relaxed text-ink">
            <ChatMarkdown text={bubble.text} />
          </div>
        </div>
      </div>
    );
  }
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
      <div className="min-w-0 flex-1 pt-1">
        <div
          className={clsx(
            "font-mono text-[14.5px] leading-relaxed",
            bubble.status === "thinking" ? "text-ink-2" : "text-ink",
          )}
        >
          <ChatMarkdown text={bubble.text} />
          {bubble.status === "thinking" ? (
            <span className="ml-2 inline-flex items-center">
              <Loader2 size={11} strokeWidth={2} className="animate-spin text-accent" />
            </span>
          ) : null}
        </div>
        {bubble.tools && bubble.tools.length > 0 ? <ToolStrip calls={bubble.tools} /> : null}
      </div>
    </div>
  );
}

// ChatMarkdown — chat-bubble-scale markdown so assistant replies render as
// formatted text instead of raw `## / ** / -` source. Headings collapse to
// bold lines (a bubble is no place for an H1), lists stay tight, tables get
// a thin border. Inherits the bubble's font size.
function ChatMarkdown({ text }: { text: string }) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        p: ({ children }) => <p className="my-1 first:mt-0 last:mb-0">{children}</p>,
        h1: ({ children }) => <p className="my-1.5 font-bold">{children}</p>,
        h2: ({ children }) => <p className="my-1.5 font-bold">{children}</p>,
        h3: ({ children }) => <p className="my-1 font-bold">{children}</p>,
        ul: ({ children }) => <ul className="my-1 list-disc space-y-0.5 pl-5">{children}</ul>,
        ol: ({ children }) => <ol className="my-1 list-decimal space-y-0.5 pl-5">{children}</ol>,
        li: ({ children }) => <li className="leading-snug">{children}</li>,
        strong: ({ children }) => <strong className="font-bold text-ink">{children}</strong>,
        a: ({ href, children }) => (
          <a href={href} target="_blank" rel="noreferrer" className="text-accent underline">
            {children}
          </a>
        ),
        code: ({ children }) => (
          <code className="rounded-[3px] border border-line-2 bg-surface-2 px-1 py-px text-[0.92em]">{children}</code>
        ),
        pre: ({ children }) => (
          <pre className="my-1.5 overflow-x-auto rounded-pixel border border-line-2 bg-surface-2 p-2 text-[12px] leading-snug">
            {children}
          </pre>
        ),
        table: ({ children }) => (
          <div className="my-1.5 overflow-x-auto">
            <table className="border-collapse text-[12.5px]">{children}</table>
          </div>
        ),
        th: ({ children }) => <th className="border border-line-2 bg-surface-2 px-2 py-0.5 text-left">{children}</th>,
        td: ({ children }) => <td className="border border-line-2 px-2 py-0.5">{children}</td>,
        blockquote: ({ children }) => (
          <blockquote className="my-1 border-l-2 border-line-2 pl-2 text-ink-2">{children}</blockquote>
        ),
        hr: () => <hr className="my-2 border-line-2" />,
        img: () => null, // images belong in the work package, not chat bubbles
      }}
    >
      {text}
    </ReactMarkdown>
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
  } else if (run.status === "awaiting_input") {
    // the clarification effect will drop the coordinator's question bubble
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
