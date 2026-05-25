"use client";

import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import {
  ArrowDownToLine,
  ArrowUp,
  Check,
  ChevronRight,
  Layers,
  Loader2,
  Map as MapIcon,
  Palette,
  Paperclip,
  PenLine,
  Plus,
  Power,
  Search,
  Sparkles,
  Trash2,
  Wrench,
  X,
} from "lucide-react";
import clsx from "clsx";

import type { SlideJob } from "@/lib/api";
import { useAgentSession, type Step, type ToolCallEntry, type Turn } from "./session";
import { ClarificationCard } from "./ClarificationCard";
import { OutlineReviewCard } from "./OutlineReviewCard";

// ChatThread is the production chat surface: vertical stack of role-
// labelled messages, tool calls collapse to Claude-Code-style inline
// blocks (⏵ tool_name), composer is pinned to the bottom of the
// scroll container and always typing-ready.
//
// Visual reference: claude.ai / claude code / chatgpt. We deliberately
// abandon the editorial chrome (masthead, phase breadcrumb, magazine
// blockquote bubbles) because the user wants Dream-Waver to feel like
// an actual conversation, not a published artifact.

export function ChatThread({
  job,
  sessionId: _sessionId,
}: {
  job: SlideJob;
  sessionId: string;
}) {
  const session = useAgentSession(job);
  const messages = useMemo(() => buildMessages(session.turns, job), [session.turns, job]);

  // Auto-scroll the message column when new content arrives. Anchored
  // to the bottom — same behaviour as ChatGPT/Claude.
  const scrollRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
  }, [messages.length, session.busy]);

  // Composer state. The send button is enabled even while the agent
  // is busy — the session hook will queue the message (single slot,
  // newer-wins) and drain it the moment agent.finish lands. The user
  // sees a "queued" row in the thread so they know it's pending.
  const [draft, setDraft] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const canSend = draft.trim().length > 0;

  const send = (e?: FormEvent | KeyboardEvent) => {
    e?.preventDefault();
    const t = draft.trim();
    if (!t) return;
    session.dispatchUserMessage(t);
    setDraft("");
    textareaRef.current?.focus();
  };

  return (
    <div className="flex h-full flex-col">
      {/* Message stack — scrolls inside this column */}
      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto px-4 pb-6 pt-4"
      >
        <div className="mx-auto max-w-2xl space-y-8">
          {messages.map((m, i) => (
            <MessageRow key={m.id} msg={m} isLast={i === messages.length - 1} />
          ))}

          {session.busy ? <ThinkingRow /> : null}

          {session.pendingMessage ? (
            <PendingRow text={session.pendingMessage} onCancel={session.cancelPending} />
          ) : null}

          {/* Sprint L1 — HILT pause gates. Render the active turn's
              `pending` payload as the matching interactive card. The
              optimistic dispatch in session.ts flips the turn back to
              "running" the moment the user clicks submit, so these
              cards auto-vanish without round-trip wait. */}
          {(() => {
            const lastTurn = session.turns[session.turns.length - 1];
            const pending = lastTurn?.pending;
            if (!pending) return null;
            if (pending.kind === "clarification") {
              return (
                <ClarificationCard
                  questions={pending.questions}
                  busy={session.busy}
                  onSubmit={session.dispatchClarification}
                />
              );
            }
            if (pending.kind === "outline_review") {
              return (
                <OutlineReviewCard
                  outline={pending.outline}
                  busy={session.busy}
                  onApprove={session.dispatchOutlineApproval}
                />
              );
            }
            return null;
          })()}

          {job.download_url ? <DownloadRow href={job.download_url} /> : null}
        </div>
      </div>

      {/* Composer — sticks to the bottom of the chat column, always
          reachable. */}
      <Composer
        draft={draft}
        setDraft={setDraft}
        canSend={canSend}
        busy={session.busy}
        onSend={send}
        textareaRef={textareaRef}
        modeNotice={job.mode === "pipeline" ? "Pipeline mode · 此 deck 不支持 chat 编辑" : ""}
      />
    </div>
  );
}

// ─── Message model ────────────────────────────────────────────────────

// The Turn[] from useAgentSession is rich; for the chat surface we
// flatten it into a linear list of "Messages" — one of three kinds:
//
//   user   — the user's text instruction
//   ai     — the agent's natural-language reasoning (an llm.thought.text)
//   tool   — a single tool invocation collapsing args + status + output
//
// agent-reply lines (extracted from tool output's `message` field) are
// folded into a subsequent "ai" message in-stream so the chat reads as
// continuous prose rather than tool-then-narration-then-tool stutter.

type Msg =
  | { id: string; kind: "user"; text: string }
  | { id: string; kind: "ai"; text: string }
  | { id: string; kind: "tool"; call: ToolCallEntry }
  | { id: string; kind: "error"; text: string };

function buildMessages(turns: Turn[], job: SlideJob): Msg[] {
  const out: Msg[] = [];
  for (let ti = 0; ti < turns.length; ti++) {
    const t = turns[ti];
    // First message of the conversation: if Turn 0 has no userMessage,
    // synthesize it from the job's stored input topic so the thread
    // opens with the user's actual request, not silently with the AI.
    if (t.kind === "initial" && !t.userMessage && ti === 0) {
      const topic = job.input?.topic || job.title;
      if (topic) {
        out.push({ id: `${t.id}-user`, kind: "user", text: topic });
      }
    } else if (t.userMessage) {
      out.push({ id: `${t.id}-user`, kind: "user", text: t.userMessage });
    }

    for (const s of t.steps) {
      pushStep(out, t.id, s);
    }
    if (t.status === "error" && t.errorMsg) {
      out.push({ id: `${t.id}-err`, kind: "error", text: t.errorMsg });
    }
  }
  return out;
}

function pushStep(out: Msg[], turnId: string, s: Step) {
  // Order within a step: agent's reasoning text first, then the tool
  // invocations it triggered. The tool's friendly "message" output
  // becomes an inline "ai" follow-up so the surface reads as continuous
  // prose.
  if (s.thought?.text) {
    out.push({
      id: `${turnId}-${s.index}-th`,
      kind: "ai",
      text: s.thought.text,
    });
  }
  for (const c of s.toolCalls) {
    out.push({ id: `${turnId}-${s.index}-${c.id}`, kind: "tool", call: c });
    const msg = extractToolMessage(c);
    if (msg && c.status === "done") {
      out.push({ id: `${turnId}-${s.index}-${c.id}-r`, kind: "ai", text: msg });
    }
  }
}

// ─── Rows ─────────────────────────────────────────────────────────────

function MessageRow({ msg, isLast: _isLast }: { msg: Msg; isLast: boolean }) {
  switch (msg.kind) {
    case "user":
      return <UserRow text={msg.text} />;
    case "ai":
      return <AiRow text={msg.text} />;
    case "tool":
      return <ToolRow call={msg.call} />;
    case "error":
      return <ErrorRow text={msg.text} />;
  }
}

function UserRow({ text }: { text: string }) {
  return (
    <div className="space-y-1.5">
      <Label kind="user" />
      <p className="whitespace-pre-wrap text-[15.5px] leading-relaxed text-[color:var(--ink)]">
        {text}
      </p>
    </div>
  );
}

function AiRow({ text }: { text: string }) {
  return (
    <div className="space-y-1.5">
      <Label kind="ai" />
      <p className="whitespace-pre-wrap text-[15.5px] leading-relaxed text-[color:var(--ink)]/95">
        {text}
      </p>
    </div>
  );
}

function ErrorRow({ text }: { text: string }) {
  return (
    <div className="space-y-1.5">
      <Label kind="ai" />
      <p className="text-[14.5px] leading-relaxed text-red-700">
        Error · {text}
      </p>
    </div>
  );
}

function ThinkingRow() {
  return (
    <div className="space-y-1.5">
      <Label kind="ai" />
      <div className="inline-flex items-center gap-2 text-[14px] text-[color:var(--ink-soft)]">
        <span className="inline-flex items-baseline gap-1">
          <Dot delay={0} />
          <Dot delay={150} />
          <Dot delay={300} />
        </span>
        <span className="italic">thinking…</span>
      </div>
    </div>
  );
}

// PendingRow renders the queued user message — appears below the
// thinking indicator so the user sees their next instruction lined up.
// Cancel × removes it from the queue without sending.
function PendingRow({ text, onCancel }: { text: string; onCancel: () => void }) {
  return (
    <div className="space-y-1.5 opacity-65">
      <div className="flex items-center gap-2">
        <span className="flex h-5 w-5 items-center justify-center rounded-full border border-dashed border-[color:var(--ink)]/40 text-[10px] font-medium text-[color:var(--ink-soft)]">
          你
        </span>
        <span className="text-[12px] font-medium text-[color:var(--ink-soft)]">
          You
        </span>
        <span className="font-mono text-[10px] uppercase tracking-[0.2em] text-[color:var(--vermillion)]">
          · 排队中
        </span>
        <button
          type="button"
          onClick={onCancel}
          className="ml-1 inline-flex h-4 w-4 items-center justify-center text-[color:var(--ink-faint)] transition-colors hover:text-[color:var(--ink)]"
          aria-label="Cancel queued message"
          title="撤回排队消息"
        >
          <X size={11} strokeWidth={1.8} />
        </button>
      </div>
      <p className="whitespace-pre-wrap text-[15.5px] italic leading-relaxed text-[color:var(--ink)]/70">
        {text}
      </p>
    </div>
  );
}

function DownloadRow({ href }: { href: string }) {
  return (
    <div className="space-y-1.5">
      <Label kind="ai" />
      <a
        href={href}
        className="inline-flex items-center gap-2 border border-[color:var(--ink)]/15 bg-white px-3.5 py-2 text-[13px] font-medium text-[color:var(--ink)] transition-colors hover:border-[color:var(--ink)]/40 hover:bg-[color:var(--paper)]/60"
      >
        <ArrowDownToLine size={14} strokeWidth={1.8} />
        <span>Download .pptx</span>
      </a>
    </div>
  );
}

// ─── Tool row (Claude Code style) ─────────────────────────────────────

const TOOL_ICON: Record<string, React.ComponentType<{ size?: number; strokeWidth?: number; className?: string }>> = {
  plan_outline: MapIcon,
  web_research: Search,
  tavily_search: Search,
  write_content: PenLine,
  render_deck: Layers,
  slide_render: Layers,
  edit_slide_text: PenLine,
  regenerate_slide: PenLine,
  delete_slide: Trash2,
  add_slide: Plus,
  change_theme: Layers,
  apply_brand: Palette,
  terminate: Power,
};

const TOOL_LABEL_ZH: Record<string, string> = {
  plan_outline: "规划大纲",
  web_research: "联网研究",
  tavily_search: "联网研究",
  write_content: "撰写内容",
  render_deck: "渲染 PPTX",
  slide_render: "渲染 PPTX",
  edit_slide_text: "改文字",
  regenerate_slide: "重写整页",
  delete_slide: "删除页",
  add_slide: "新增页",
  change_theme: "换主题",
  apply_brand: "应用品牌",
  terminate: "完成",
};

function ToolRow({ call }: { call: ToolCallEntry }) {
  const Icon = TOOL_ICON[call.name] ?? Wrench;
  const zh = TOOL_LABEL_ZH[call.name] ?? "";
  const [open, setOpen] = useState(false);

  const hasBody = !!(call.output || call.error);
  const summary = useMemo(() => extractToolSummary(call), [call]);

  return (
    <div className="rounded-sm border border-[color:var(--rule)] bg-white/50">
      <button
        type="button"
        onClick={() => hasBody && setOpen((v) => !v)}
        className={clsx(
          "flex w-full items-center gap-2.5 px-3 py-2 text-left transition-colors",
          hasBody && "hover:bg-[color:var(--paper)]/60",
          !hasBody && "cursor-default",
        )}
      >
        {hasBody ? (
          <ChevronRight
            size={13}
            strokeWidth={1.8}
            className={clsx(
              "shrink-0 text-[color:var(--ink-faint)] transition-transform",
              open && "rotate-90",
            )}
          />
        ) : (
          <span className="w-[13px]" />
        )}
        <Icon size={13} strokeWidth={1.8} className="shrink-0 text-[color:var(--ink-soft)]" />
        <span className="font-mono text-[12px] text-[color:var(--ink-soft)]">
          {call.name}
        </span>
        {zh ? (
          <span className="text-[12px] italic text-[color:var(--ink-faint)]">
            · {zh}
          </span>
        ) : null}
        <span className="ml-auto inline-flex items-center gap-1.5">
          {summary ? (
            <span className="hidden truncate text-[12px] text-[color:var(--ink-faint)] md:inline-block md:max-w-[220px]">
              {summary}
            </span>
          ) : null}
          <StatusIcon status={call.status} />
        </span>
      </button>

      {open && hasBody ? (
        <div className="border-t border-[color:var(--rule)] bg-[color:var(--paper)]/40 px-3 py-2.5">
          {call.error ? (
            <pre className="whitespace-pre-wrap font-mono text-[12px] leading-relaxed text-red-700">
              {call.error}
            </pre>
          ) : (
            <pre className="max-h-72 overflow-y-auto whitespace-pre-wrap font-mono text-[11.5px] leading-relaxed text-[color:var(--ink-soft)]">
              {prettify(call.output ?? "")}
            </pre>
          )}
        </div>
      ) : null}
    </div>
  );
}

function StatusIcon({ status }: { status: ToolCallEntry["status"] }) {
  if (status === "running") {
    return (
      <Loader2
        size={12}
        strokeWidth={2}
        className="animate-spin text-[color:var(--vermillion)]"
      />
    );
  }
  if (status === "done") {
    return <Check size={12} strokeWidth={2.2} className="text-emerald-600" />;
  }
  return <X size={12} strokeWidth={2.2} className="text-red-700" />;
}

// ─── Label (the "You" / "Dream-Waver" caption above each message) ─────

function Label({ kind }: { kind: "user" | "ai" }) {
  if (kind === "user") {
    return (
      <div className="flex items-center gap-2">
        <span className="flex h-5 w-5 items-center justify-center rounded-full bg-[color:var(--ink)] text-[10px] font-medium text-[color:var(--paper)]">
          你
        </span>
        <span className="text-[12px] font-medium text-[color:var(--ink-soft)]">You</span>
      </div>
    );
  }
  return (
    <div className="flex items-center gap-2">
      <span className="flex h-5 w-5 items-center justify-center rounded-full bg-[color:var(--vermillion)] text-[color:var(--paper)]">
        <Sparkles size={11} strokeWidth={2} />
      </span>
      <span className="text-[12px] font-medium text-[color:var(--ink-soft)]">
        Dream-Waver
      </span>
    </div>
  );
}

function Dot({ delay }: { delay: number }) {
  return (
    <span
      className="inline-block h-1 w-1 rounded-full bg-[color:var(--ink-soft)]"
      style={{
        animation: "dw-blink 1.2s ease-in-out infinite",
        animationDelay: `${delay}ms`,
      }}
    />
  );
}

// ─── Composer (sticky bottom) ─────────────────────────────────────────

function Composer({
  draft,
  setDraft,
  canSend,
  busy,
  onSend,
  textareaRef,
  modeNotice,
}: {
  draft: string;
  setDraft: (s: string) => void;
  canSend: boolean;
  busy: boolean;
  onSend: (e?: FormEvent | KeyboardEvent) => void;
  textareaRef: React.RefObject<HTMLTextAreaElement | null>;
  modeNotice: string;
}) {
  return (
    <div className="border-t border-[color:var(--rule)] bg-[color:var(--paper)]/85 px-4 pb-5 pt-4 backdrop-blur-[2px]">
      <form
        onSubmit={onSend}
        className="mx-auto max-w-2xl rounded-2xl border border-[color:var(--ink)]/15 bg-white px-4 py-3 shadow-[0_1px_2px_rgba(26,22,20,0.04)] focus-within:border-[color:var(--ink)]/35"
      >
        <textarea
          ref={textareaRef}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            // Plain Enter sends; Shift+Enter inserts a newline.
            // Matches ChatGPT/Claude muscle memory.
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              onSend(e);
            }
          }}
          rows={1}
          placeholder={
            busy
              ? "继续打 — 这条会在 agent 完成后自动发出"
              : "继续对话 — 修改、添加、应用品牌色…"
          }
          className="block w-full resize-none bg-transparent text-[15px] leading-relaxed text-[color:var(--ink)] placeholder:text-[color:var(--ink-faint)] focus:outline-none"
          style={{ maxHeight: 200 }}
        />
        <div className="mt-2 flex items-center justify-between">
          <div className="flex items-center gap-1">
            <ComposerIcon
              aria-label="Attach files (coming soon)"
              title="附件 · 即将上线"
              disabled
            >
              <Paperclip size={15} strokeWidth={1.8} />
            </ComposerIcon>
            {modeNotice ? (
              <span className="ml-2 text-[11px] italic text-[color:var(--ink-faint)]">
                {modeNotice}
              </span>
            ) : null}
          </div>

          <button
            type="submit"
            disabled={!canSend}
            className={clsx(
              "inline-flex h-8 w-8 items-center justify-center rounded-full transition-colors",
              canSend
                ? "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)]"
                : "bg-[color:var(--paper)] text-[color:var(--ink-faint)]",
            )}
            aria-label="Send"
          >
            <ArrowUp size={14} strokeWidth={2.2} />
          </button>
        </div>
      </form>
      <p className="mx-auto mt-2 max-w-2xl text-center text-[11px] text-[color:var(--ink-faint)]">
        Enter 发送 · Shift+Enter 换行
      </p>
    </div>
  );
}

function ComposerIcon({
  children,
  disabled,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      {...rest}
      className={clsx(
        "flex h-7 w-7 items-center justify-center rounded-full transition-colors",
        disabled
          ? "cursor-not-allowed text-[color:var(--ink-faint)]"
          : "text-[color:var(--ink-soft)] hover:bg-[color:var(--paper)]/60",
      )}
    >
      {children}
    </button>
  );
}

// ─── Helpers ──────────────────────────────────────────────────────────

// Pull a short, human-readable summary out of a tool call for the
// collapsed-row right side. Falls back to "" when the output isn't
// structured. Prefers `message` (which our tools all set) but also
// surfaces other useful one-liners.
function extractToolSummary(call: ToolCallEntry): string {
  if (call.status === "running") return "";
  if (call.status === "error") return "";
  if (!call.output) return "";
  try {
    const j = JSON.parse(call.output) as Record<string, unknown>;
    const msg = typeof j.message === "string" ? j.message : "";
    if (msg) return msg;
    if (typeof j.theme === "string") return `theme=${j.theme}`;
    if (typeof j.outline_title === "string") return j.outline_title;
    if (typeof j.slide_count === "number") return `${j.slide_count} slides`;
  } catch {
    /* fallthrough */
  }
  return "";
}

function extractToolMessage(call: ToolCallEntry): string {
  if (!call.output) return "";
  try {
    const j = JSON.parse(call.output) as Record<string, unknown>;
    return typeof j.message === "string" ? j.message : "";
  } catch {
    return "";
  }
}

// prettify pretty-prints JSON if the output is JSON; otherwise leaves
// the string intact. Used inside the expanded tool block.
function prettify(s: string): string {
  const trimmed = s.trim();
  if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) return s;
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return s;
  }
}
