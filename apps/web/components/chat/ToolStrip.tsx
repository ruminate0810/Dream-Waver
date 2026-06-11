"use client";

import React, { useState } from "react";
import clsx from "clsx";

import { useHeightCollapse } from "@/lib/motion";
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
  ListChecks,
  Image as ImageIcon,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { RunningClock, ToolProgressBar, fmtDuration } from "./ToolProgress";

// ToolStrip is the row of "what the agent invoked, in order" cards rendered
// inside a Phase body. The visual treatment matches the pixel language:
// hard ink-bordered window cards, mono tool names, violet pulse for the
// running entry, a quiet grass check for completed ones. Output content is
// truncated by default and reveals on click — same as how a terminal might
// fold lengthy logs behind a disclosure.

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
  web_search: Search,
  // Claw vertical
  plan_tasks: Map,
  update_task: ListChecks,
  write_document: PenLine,
  generate_image: ImageIcon,
  generate_deck: Layers,
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
  web_search: "联网搜索",
  // Claw vertical
  plan_tasks: "规划任务",
  update_task: "勾选进度",
  write_document: "撰写报告",
  generate_image: "生成配图",
  generate_deck: "生成幻灯",
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
// get a violet left-edge bar (they're "review" gestures, distinct
// from "do something" tools); introspect tools (analyze_deck) get a
// muted bar so the user can tell at a glance that the agent is
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
    <ol className="mt-6 space-y-2">
      {calls.map((c, i) => (
        // Sprint Z.5 — new ToolCard fades + rises into the list.
        // First few entries stagger (40ms apart) so a burst of
        // critic→revise calls reads as a sequence not a slam.
        // Late single arrivals (capped at 5 × 40ms) snap in alone.
        <li
          key={c.id}
          className="animate-phase-in"
          style={{ animationDelay: `${Math.min(i, 5) * 40}ms` }}
        >
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
  // Sprint Z.5 — height collapse on body open/close. The body is
  // always mounted so the height tween has something to measure;
  // useHeightCollapse handles the open/close lifecycle.
  const bodyRef = useHeightCollapse(open) as React.RefObject<HTMLDivElement | null>;

  return (
    <div
      className={clsx(
        "overflow-hidden rounded-pixel border-2 border-ink bg-surface/60",
        // Sprint O — category-specific left-edge bar so critic /
        // introspect calls read as a different gesture than action
        // tools. The bar is decorative; the icon already carries
        // the canonical signal.
        category === "critic" && "border-l-4 border-l-accent",
        category === "introspect" && "border-l-4 border-l-muted",
      )}
    >
      <button
        type="button"
        onClick={() => hasBody && setOpen((v) => !v)}
        className={clsx(
          "group flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors",
          hasBody && "cursor-pointer hover:bg-surface-2",
        )}
        aria-expanded={open}
        disabled={!hasBody}
      >
        <span
          className={clsx(
            "flex h-9 w-9 shrink-0 items-center justify-center rounded-pixel border-[1.5px] border-ink",
            call.status === "running" && "bg-accent-soft text-accent",
            call.status === "done" && "bg-surface-2 text-ink",
            call.status === "error" && "bg-[#fdece9] text-[#a23a2a]",
          )}
        >
          <Icon size={16} strokeWidth={1.6} />
        </span>

        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-3">
            <span className="font-mono text-[12px] font-semibold text-ink">
              {call.name}
            </span>
            {zh && (
              <span className="font-mono text-[13px] text-ink-2">
                {zh}
              </span>
            )}
            {typeof call.durationMs === "number" && call.durationMs > 0 && (
              <span className="font-mono text-[10px] tabular-nums text-muted">
                {fmtDuration(call.durationMs)}
              </span>
            )}
            {call.status === "running" && (
              <RunningClock className="font-mono text-[10px] tabular-nums text-accent" />
            )}
          </div>
          {call.error && (
            <p className="mt-1 font-mono text-[13px] text-[#a23a2a]">
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
              "ml-2 text-muted transition-transform duration-200",
              open && "rotate-180",
            )}
          />
        )}
      </button>

      {call.status === "running" ? <ToolProgressBar label={`${call.name} 运行中`} /> : null}

      {/* Sprint Z.5 — body always mounted, controlled by useHeightCollapse.
          When `open` false the wrapper height is tweened to 0 + opacity 0;
          when true it's tweened to scrollHeight + opacity 1. Keeps the
          DOM stable so screen readers + GSAP measurements behave. */}
      {hasBody && (
        <div
          ref={bodyRef}
          className="overflow-hidden"
          style={{ height: 0, opacity: 0 }}
        >
          <div className="border-t-2 border-ink bg-paper/40 px-3 py-2.5">
            <div className="max-h-64 overflow-y-auto">
              {call.input && (
                <div className="mb-3">
                  <p className="mb-1 font-pixel text-[0.55rem] tracking-wide text-muted">
                    Input
                  </p>
                  <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-ink-2">
                    {call.input}
                  </pre>
                </div>
              )}
              {(call.output || call.error) && (
                <div>
                  {call.input && (
                    <p className="mb-1 font-pixel text-[0.55rem] tracking-wide text-muted">
                      {call.error ? "Error" : "Output"}
                    </p>
                  )}
                  <pre
                    className={clsx(
                      "whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed",
                      call.error ? "text-[#a23a2a]" : "text-ink-2",
                    )}
                  >
                    {call.output || call.error}
                  </pre>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function StatusPip({ status }: { status: ToolCallStatus }) {
  if (status === "running") {
    return (
      <span className="inline-flex items-center gap-1.5 font-pixel text-[0.55rem] tracking-wide text-accent">
        <Loader2 size={11} className="animate-spin" strokeWidth={1.8} />
        Running
      </span>
    );
  }
  if (status === "done") {
    return (
      <span className="inline-flex items-center gap-1.5 font-pixel text-[0.55rem] tracking-wide text-grass">
        <Check size={11} strokeWidth={2.4} />
        Done
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 font-pixel text-[0.55rem] tracking-wide text-[#a23a2a]">
      <X size={11} strokeWidth={2.4} />
      Error
    </span>
  );
}
