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
  terminate: Power,
};

const ZH_LABEL: Record<string, string> = {
  web_research: "联网查证",
  tavily_search: "联网查证",
  plan_outline: "规划大纲",
  write_content: "撰写内容",
  render_deck: "排版渲染",
  slide_render: "排版渲染",
  terminate: "收尾",
};

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
  const hasBody = Boolean(call.output || call.error);

  return (
    <div className="py-4">
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
          <pre className="whitespace-pre-wrap break-words font-mono-jb text-[11px] leading-relaxed text-[color:var(--ink-soft)]">
            {call.output || call.error}
          </pre>
        </div>
      )}
    </div>
  );
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
