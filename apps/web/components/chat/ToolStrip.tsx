"use client";

import { useState } from "react";
import clsx from "clsx";
import {
  Loader2,
  Check,
  X,
  ChevronDown,
  Map,
  PenLine,
  Layers,
  Search,
  Power,
  Wrench,
  Scale,
  RefreshCw,
  Microscope,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

// ToolStrip is the row of "what the agent invoked, in order" cards rendered
// inside a Phase body. The visual treatment matches the editorial language:
// thin hairline rules, mono caps for tool names, vermillion pulse for the
// running entry, a quiet check for completed ones. Output content is
// truncated by default and reveals on click — same as how a journal might
// fold lengthy citations into footnotes.

export type ToolCallStatus = "running" | "done" | "error";

export type ToolCallEntry = {
  id: string;
  name: string;            // 'plan_outline', etc.
  status: ToolCallStatus;
  /** Truncated args preview from tool.start (~240 chars). */
  input?: string;
  /** Wall-clock ms from tool.end. */
  durationMs?: number;
  output?: string;         // truncated to ~400 chars by the backend
  error?: string;
};

const ICON_FOR: Record<string, LucideIcon> = {
  web_research: Search,
  tavily_search: Search,
  plan_outline: Map,
  write_content: PenLine,
  render_deck: Layers,
  slide_render: Layers,
  code_execute: Wrench,
  write_game: Layers,
  terminate: Power,
  // Sprint O — reflection tools
  critic_outline: Scale,
  critic_content: Scale,
  critic_deck: Scale,
  revise_outline: RefreshCw,
  revise_slide: RefreshCw,
  analyze_deck: Microscope,
};

const ZH_LABEL: Record<string, string> = {
  web_research: "联网查证",
  tavily_search: "联网查证",
  plan_outline: "规划大纲",
  write_content: "撰写内容",
  render_deck: "排版渲染",
  slide_render: "排版渲染",
  code_execute: "沙箱执行",
  write_game: "生成游戏",
  terminate: "收尾",
  // Sprint O — reflection tools
  critic_outline: "审阅大纲",
  critic_content: "审阅内容",
  critic_deck: "审阅整稿",
  revise_outline: "修订大纲",
  revise_slide: "修订单页",
  analyze_deck: "通读分析",
};

// Sprint O — tool category groups for visual distinction. Critic tools
// get a vermillion left-edge bar (they're "review" gestures, distinct
// from "do something" tools); introspect tools (analyze_deck) get a
// subtle ink bar so the user can tell at a glance that the agent is
// reading rather than mutating.
type ToolCategory = "action" | "critic" | "introspect";

function categoryOf(name: string): ToolCategory {
  if (name.startsWith("critic_")) return "critic";
  if (name === "analyze_deck") return "introspect";
  return "action";
}

export function ToolStrip({ calls }: { calls: ToolCallEntry[] }) {
  if (!calls.length) return null;
  return (
    <ol className="mt-6 divide-y divide-[color:var(--rule)] border-t border-[color:var(--rule)]">
      {calls.map((c) => (
        <li key={c.id}>
          <ToolCard call={c} />
        </li>
      ))}
    </ol>
  );
}

function ToolCard({ call }: { call: ToolCallEntry }) {
  const [open, setOpen] = useState(false);
  const Icon = ICON_FOR[call.name] ?? Wrench;
  const zh = ZH_LABEL[call.name];
  const category = categoryOf(call.name);
  const hasBody = Boolean(call.output || call.error || call.input);

  return (
    <div
      className={clsx(
        "relative py-4",
        // Sprint O — category-specific left-edge bar so critic /
        // introspect calls read as a different gesture than action
        // tools. The bar is decorative; the icon already carries
        // the canonical signal.
        category === "critic" && "border-l-2 border-[color:var(--vermillion)] pl-3 -ml-3",
        category === "introspect" && "border-l-2 border-[color:var(--ink-soft)]/40 pl-3 -ml-3",
      )}
    >
      <button
        type="button"
        onClick={() => hasBody && setOpen((v) => !v)}
        className={clsx(
          "group flex w-full items-center gap-4 text-left",
          hasBody && "cursor-pointer",
        )}
        aria-expanded={open}
        disabled={!hasBody}
      >
        <span
          className={clsx(
            "flex h-9 w-9 shrink-0 items-center justify-center border border-[color:var(--rule)]",
            call.status === "running" &&
              "bg-[color:var(--paper)] text-[color:var(--vermillion)]",
            call.status === "done" &&
              "bg-[color:var(--paper)] text-[color:var(--ink)]",
            call.status === "error" && "bg-red-50 text-red-700",
          )}
        >
          <Icon size={16} strokeWidth={1.6} />
        </span>

        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-3">
            <span className="font-mono-jb text-[11px] uppercase tracking-[0.22em] text-[color:var(--ink)]">
              {call.name}
            </span>
            {zh && (
              <span className="font-display text-[15px] text-[color:var(--ink-soft)]">
                {zh}
              </span>
            )}
            {typeof call.durationMs === "number" && call.durationMs > 0 && (
              <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
                {formatDuration(call.durationMs)}
              </span>
            )}
          </div>
          {call.error && (
            <p className="mt-1 font-display text-sm italic text-red-700">
              {call.error}
            </p>
          )}
        </div>

        <StatusPip status={call.status} />
        {hasBody && (
          <ChevronDown
            size={14}
            strokeWidth={1.6}
            className={clsx(
              "ml-2 text-[color:var(--ink-faint)] transition-transform duration-200",
              open && "rotate-180",
            )}
          />
        )}
      </button>

      {hasBody && open && (
        <div className="mt-3 ml-13 max-h-64 overflow-y-auto border-l-2 border-[color:var(--rule)] pl-4">
          {call.input && (
            <div className="mb-3">
              <p className="mb-1 font-mono-jb text-[9px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
                Input
              </p>
              <pre className="whitespace-pre-wrap break-words font-mono-jb text-[11px] leading-relaxed text-[color:var(--ink-soft)]">
                {call.input}
              </pre>
            </div>
          )}
          {(call.output || call.error) && (
            <div>
              {call.input && (
                <p className="mb-1 font-mono-jb text-[9px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
                  {call.error ? "Error" : "Output"}
                </p>
              )}
              <pre className="whitespace-pre-wrap break-words font-mono-jb text-[11px] leading-relaxed text-[color:var(--ink-soft)]">
                {call.output || call.error}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// formatDuration renders ms either as "312ms" or "4.7s" depending on
// magnitude. Below 1 s integers; above, one decimal place.
function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function StatusPip({ status }: { status: ToolCallStatus }) {
  if (status === "running") {
    return (
      <span className="inline-flex items-center gap-1.5 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--vermillion)]">
        <Loader2 size={11} className="animate-spin" strokeWidth={1.8} />
        Running
      </span>
    );
  }
  if (status === "done") {
    return (
      <span className="inline-flex items-center gap-1.5 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-soft)]">
        <Check size={11} strokeWidth={2.4} />
        Done
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-red-700">
      <X size={11} strokeWidth={2.4} />
      Error
    </span>
  );
}
