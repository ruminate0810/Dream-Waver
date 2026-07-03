"use client";

import { useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import type { ClawRun } from "@/lib/api";
import { useAgentEventStream, type AgentEvent } from "@/components/chat/transport";
import { WORKERS } from "./workers";

// AltViews (v22b 三视图) — the checklist and log renderings of the SAME
// event stream the office consumes. Information-equivalent by construction:
// all three views subscribe to one stream and poll one run; only the
// representation differs. This is both the product's drill-down path
// (metaphor by default, detail on demand) and the study's baseline
// conditions A (log) / B (checklist) — see docs/claw-v2-design.md §8.

const ZH: Record<string, string> = Object.fromEntries(WORKERS.map((w) => [w.key, w.zh]));

const PHASE_ZH: Record<string, string> = {
  clarify: "澄清",
  plan: "规划",
  debate: "例会",
  exec: "执行",
  write: "撰稿",
  review: "评审",
  produce: "制片",
  video: "视频",
};

/** Collect the full event history for this mount (capped). Both alt views
 *  need history (the office reads live state instead); ephemeral —
 *  switching views re-accumulates from the switch point, while checklist
 *  rows/artifact versions stay authoritative via the polled run. */
function useEventLog(cap = 600): AgentEvent[] {
  const stream = useAgentEventStream();
  const [log, setLog] = useState<AgentEvent[]>([]);
  useEffect(
    () =>
      stream.subscribe((ev) => {
        setLog((prev) => (prev.length >= cap ? [...prev.slice(-cap + 1), ev] : [...prev, ev]));
      }),
    [stream, cap],
  );
  return log;
}

// ── 清单视图 (condition B — structured text, Cocoa-style) ────────────────

export function ChecklistView({ run }: { run: ClawRun }) {
  const log = useEventLog();
  const phases = useMemo(() => {
    const seen: { phase: string; status: string }[] = [];
    for (const ev of log) {
      if (ev.kind === "claw.phase" && ev.data.claw_phase) {
        seen.push({ phase: ev.data.claw_phase, status: ev.data.claw_phase_status ?? "" });
      }
    }
    return seen;
  }, [log]);
  const issues = useMemo(
    () =>
      log.filter(
        (ev) =>
          (ev.kind === "tool.end" && ev.data.error) ||
          ev.kind === "claw.issue" ||
          (ev.kind === "claw.task.update" && ev.data.task_status === "skipped"),
      ),
    [log],
  );
  const currentPhase = phases.filter((p) => p.status === "start").at(-1)?.phase;
  const done = (run.plan ?? []).filter((t) => t.status === "done").length;

  return (
    <div className="h-full overflow-y-auto rounded-pixel border-2 border-ink bg-surface p-5 shadow-pixel">
      <div className="mb-3 flex items-baseline justify-between border-b-2 border-ink pb-2">
        <h2 className="font-pixel text-[0.7rem] tracking-wide text-ink">✦ 任务清单 · {run.title || run.prompt.slice(0, 30)}</h2>
        <span className="font-mono text-[11px] tabular-nums text-muted">
          {done}/{run.plan?.length ?? 0}
        </span>
      </div>

      {/* 阶段时间线 */}
      <div className="mb-4 flex flex-wrap items-center gap-1 font-mono text-[11px]">
        {(["clarify", "plan", "debate", "exec", "write", "review", "produce", "video"] as const).map((p) => {
          const started = phases.some((x) => x.phase === p && x.status === "start");
          const ended = phases.some((x) => x.phase === p && x.status === "end");
          const live = started && !ended && currentPhase === p;
          if (!started && !["plan", "exec", "write"].includes(p)) return null; // skipped optional phases stay silent
          return (
            <span
              key={p}
              className={clsx(
                "rounded-[4px] px-1.5 py-[2px] font-bold",
                live ? "animate-pixpulse bg-accent text-white" : ended ? "bg-grass/15 text-grass" : "text-muted",
              )}
            >
              {PHASE_ZH[p]}
              {ended && " ✓"}
            </span>
          );
        })}
      </div>

      {/* 子任务清单 — the polled plan is authoritative (claw.task.update-driven) */}
      <ul className="space-y-2">
        {(run.plan ?? []).map((t, i) => (
          <li key={i} className="flex items-start gap-2 font-mono text-[13px] leading-snug">
            <span
              className={clsx(
                "mt-[1px] flex-none",
                t.status === "done"
                  ? "text-grass"
                  : t.status === "doing"
                    ? "animate-pixpulse text-accent"
                    : t.status === "skipped"
                      ? "text-line-2"
                      : "text-muted",
              )}
            >
              {t.status === "done" ? "■✓" : t.status === "doing" ? "▶" : t.status === "skipped" ? "⨯" : "□"}
            </span>
            <span className={clsx(t.status === "skipped" && "text-line-2 line-through")}>
              {t.title}
              {t.role && <span className="ml-2 text-[11px] text-muted">@{ZH[t.role] ?? t.role}</span>}
            </span>
          </li>
        ))}
      </ul>

      {/* 产物 */}
      {(run.artifact_version ?? 0) > 0 && (
        <p className="mt-4 border-t-2 border-line pt-2 font-mono text-[12px] text-ink-2">
          产物:报告 v{run.artifact_version}
          {(run.figures?.length ?? 0) > 0 && ` · 配图 ${run.figures?.length} 张`}
          {run.deck && " · PPT"}
          {(run.videos?.length ?? 0) > 0 && ` · 视频 ${run.videos?.length}`}
        </p>
      )}

      {/* 问题(与办公室事故板同源) */}
      {issues.length > 0 && (
        <div className="mt-3 border-t-2 border-line pt-2">
          <p className="mb-1 font-mono text-[11px] font-bold" style={{ color: "#c96f1f" }}>
            问题 · {issues.length}
          </p>
          <ul className="space-y-0.5 font-mono text-[11px] text-ink-2">
            {issues.map((ev, i) => (
              <li key={i}>▪ {issueLine(ev)}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

// ── 日志视图 (condition A — the raw trace, LangSmith-style) ──────────────

export function LogView({ run }: { run: ClawRun }) {
  const log = useEventLog();
  return (
    <div className="h-full overflow-y-auto rounded-pixel border-2 border-ink bg-surface shadow-pixel">
      <div className="sticky top-0 border-b-2 border-ink bg-surface-2 px-3 py-1.5 font-pixel text-[0.55rem] tracking-wide text-muted">
        § EVENT LOG · {run.job_id.slice(0, 8)} · {log.length} 条(本次挂载起)
      </div>
      <table className="w-full font-mono text-[11px]">
        <tbody>
          {log.map((ev, i) => (
            <tr key={i} className="border-b border-line/60 align-top">
              <td className="whitespace-nowrap px-2 py-1 tabular-nums text-muted">{fmtTs(ev)}</td>
              <td className="whitespace-nowrap px-2 py-1 text-ink-2">{ev.data.agent ? (ZH[ev.data.agent] ?? ev.data.agent) : "—"}</td>
              <td className="whitespace-nowrap px-2 py-1 font-bold text-ink">{ev.kind}</td>
              <td className="px-2 py-1 text-ink-2">{summarize(ev)}</td>
            </tr>
          ))}
          {log.length === 0 && (
            <tr>
              <td className="px-3 py-6 text-muted" colSpan={4}>
                等待事件…(日志从进入本视图起记录;完整历史将由时光机提供)
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

// ── shared helpers ───────────────────────────────────────────────────────

function fmtTs(ev: AgentEvent): string {
  const t = (ev as { ts?: number | string }).ts;
  const d = t ? new Date(t) : new Date();
  return d.toLocaleTimeString("zh-CN", { hour12: false });
}

function summarize(ev: AgentEvent): string {
  const d = ev.data;
  switch (ev.kind) {
    case "claw.phase":
      return `${PHASE_ZH[d.claw_phase ?? ""] ?? d.claw_phase} ${d.claw_phase_status === "start" ? "开始" : "结束"}`;
    case "claw.plan":
      return (d.task_titles ?? []).join(" / ");
    case "claw.task.update":
      return `任务 #${d.task_index} → ${d.task_status}`;
    case "claw.issue":
      return issueLine(ev);
    case "claw.artifact.updated":
      return `${d.artifact_kind} v${d.artifact_version} (${d.artifact_bytes} bytes)`;
    case "tool.start":
      return `${d.tool_name ?? ""} ${previewInput(d.tool_input)}`;
    case "tool.end":
      return d.error ? `${d.tool_name ?? ""} 失败: ${String(d.error).slice(0, 60)}` : `${d.tool_name ?? ""} ✓ ${d.duration_ms ?? 0}ms`;
    default:
      return "";
  }
}

function issueLine(ev: AgentEvent): string {
  const d = ev.data;
  if (ev.kind === "tool.end") return `${ZH[d.agent ?? ""] ?? d.agent} · ${d.tool_name} 失败`;
  if (ev.kind === "claw.task.update") return `任务 #${d.task_index} 被跳过`;
  switch (d.claw_issue_kind) {
    case "report_revised":
      return (d.claw_issue_count ?? 0) > 0 ? `评审改稿 ${d.claw_issue_count} 处` : "评审通过(零修改)";
    case "guard_backfill":
      return `${d.claw_issue_count} 项任务由守卫补记`;
    case "clarify_fallback":
      return "澄清判断失败,按字面理解开工";
    default:
      return d.claw_issue_kind ?? "";
  }
}

function previewInput(input?: string): string {
  if (!input) return "";
  try {
    const o = JSON.parse(input) as Record<string, unknown>;
    const v = o.query ?? o.q ?? o.prompt ?? o.caption ?? o.title ?? "";
    return typeof v === "string" && v ? `· ${v.slice(0, 48)}` : "";
  } catch {
    return "";
  }
}
