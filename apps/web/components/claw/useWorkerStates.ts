"use client";

import { useEffect, useMemo, useReducer } from "react";

import { useAgentEventStream, type AgentEvent } from "@/components/chat/transport";
import type { ClawTask } from "@/lib/api";
import { WORKERS, TOOL_ACTION, workerForTool } from "./workers";

// useWorkerStates folds the live agent events (attributed by `agent`) plus the
// polled plan into one per-worker view: status / live task detail / signature
// gesture / call stats. Shared by the office scene (and previously the desk
// grid). The status LABEL is the real task — the gesture is liveliness.

export type WorkerStatus = "idle" | "working" | "done";

export type WorkerView = {
  status: WorkerStatus;
  detail?: string; // live task detail (query / sub-task / …)
  gesture?: string; // current tool's signature action
  /** v22 诚实语法 — tool failures this run (rework proxy). Drives the ↻×n
   *  desk badge; derived purely from tool.end error events. */
  errors?: number;
  calls: number;
  totalMs: number;
  // 结果反应 — a transient gesture played in REACTION to a real outcome
  // (tool error → facepalm, key-tool success → eureka). Expires by time so
  // the office tick loop naturally clears it without a new event.
  reactionGesture?: string;
  reactionUntil?: number;
};

// tools whose success is worth a little celebration (not every tool.end)
const CHEER_TOOLS = new Set(["code_execute", "generate_image", "edit_image", "generate_poster", "generate_storybook", "write_document", "generate_deck", "generate_video"]);

type Acc = {
  roleByIndex: Record<number, string>;
  taskStatus: Record<number, string>;
  running: Record<string, Set<string>>;
  currentTool: Record<string, string>;
  detail: Record<string, string>;
  calls: Record<string, number>;
  totalMs: Record<string, number>;
  errors: Record<string, number>; // v22: tool.end failures per worker
  reactions: Record<string, { g: string; until: number }>;
  lastActivity: string; // ticker line: "工程师 · 执行 python 代码"
};

function emptyAcc(): Acc {
  return {
    roleByIndex: {},
    taskStatus: {},
    running: {},
    currentTool: {},
    detail: {},
    calls: {},
    totalMs: {},
    errors: {},
    reactions: {},
    lastActivity: "",
  };
}

function byKey(agent: string): string | undefined {
  return WORKERS.find((w) => w.key === agent)?.key;
}

function reduce(acc: Acc, ev: AgentEvent): Acc {
  const d = ev.data;
  switch (ev.kind) {
    case "claw.plan": {
      const roles = d.task_roles ?? [];
      const roleByIndex = { ...acc.roleByIndex };
      roles.forEach((r, i) => (roleByIndex[i + 1] = r));
      return { ...acc, roleByIndex };
    }
    case "claw.task.update": {
      if (!d.task_index) return acc;
      return { ...acc, taskStatus: { ...acc.taskStatus, [d.task_index]: d.task_status ?? "" } };
    }
    case "tool.start": {
      const key = (d.agent && byKey(d.agent)) || (d.tool_name && workerForTool(d.tool_name));
      if (!key || !d.tool_id) return acc;
      const running = { ...acc.running, [key]: new Set(acc.running[key]) };
      running[key].add(d.tool_id);
      const detail = detailFor(d.tool_name ?? "", d.tool_input);
      const zh = WORKERS.find((w) => w.key === key)?.zh ?? key;
      return {
        ...acc,
        running,
        currentTool: { ...acc.currentTool, [key]: d.tool_name ?? "" },
        detail: { ...acc.detail, [key]: detail },
        lastActivity: detail ? `${zh} · ${detail}` : zh,
      };
    }
    case "tool.end": {
      const key = (d.agent && byKey(d.agent)) || (d.tool_name && workerForTool(d.tool_name));
      if (!key || !d.tool_id) return acc;
      const running = { ...acc.running, [key]: new Set(acc.running[key]) };
      running[key].delete(d.tool_id);
      const currentTool = { ...acc.currentTool };
      if (running[key].size === 0) delete currentTool[key];
      // 结果反应: error → facepalm; meaningful success → eureka.
      const reactions = { ...acc.reactions };
      const errors = { ...acc.errors };
      if (d.error) {
        reactions[key] = { g: "facepalm", until: Date.now() + 2400 };
        errors[key] = (errors[key] ?? 0) + 1;
      } else if (d.tool_name && CHEER_TOOLS.has(d.tool_name)) {
        reactions[key] = { g: "eureka", until: Date.now() + 1700 };
      }
      return {
        ...acc,
        running,
        currentTool,
        reactions,
        errors,
        calls: { ...acc.calls, [key]: (acc.calls[key] ?? 0) + 1 },
        totalMs: { ...acc.totalMs, [key]: (acc.totalMs[key] ?? 0) + (d.duration_ms ?? 0) },
      };
    }
    default:
      return acc;
  }
}

export function useWorkerStates(plan: ClawTask[]): {
  views: Record<string, WorkerView>;
  lastActivity: string;
} {
  const stream = useAgentEventStream();
  const [acc, dispatch] = useReducer(reduce, undefined, emptyAcc);

  useEffect(() => stream.subscribe((ev) => dispatch(ev)), [stream]);

  return useMemo(() => {
    // Merge the polled plan (authoritative on cold load) under the live state.
    const roleByIndex: Record<number, string> = {};
    const taskStatus: Record<number, string> = {};
    plan.forEach((t, i) => {
      if (t.role) roleByIndex[i + 1] = t.role;
      taskStatus[i + 1] = t.status;
    });
    Object.assign(roleByIndex, acc.roleByIndex);
    Object.assign(taskStatus, acc.taskStatus);

    const views: Record<string, WorkerView> = {};
    for (const def of WORKERS) {
      const running = (acc.running[def.key]?.size ?? 0) > 0;
      const myTasks = Object.entries(roleByIndex)
        .filter(([, role]) => role === def.key)
        .map(([idx]) => Number(idx));
      const anyDoing = myTasks.some((i) => taskStatus[i] === "doing");
      const allResolved =
        myTasks.length > 0
          ? myTasks.every((i) => taskStatus[i] === "done" || taskStatus[i] === "skipped")
          : (acc.calls[def.key] ?? 0) > 0;
      const hadWork = (acc.calls[def.key] ?? 0) > 0 || myTasks.some((i) => taskStatus[i] === "done");

      let status: WorkerStatus = "idle";
      if (running || anyDoing) status = "working";
      else if (hadWork && allResolved) status = "done";

      const tool = acc.currentTool[def.key];
      const reaction = acc.reactions[def.key];
      views[def.key] = {
        status,
        detail: acc.detail[def.key],
        gesture: tool ? TOOL_ACTION[tool] : undefined,
        calls: acc.calls[def.key] ?? 0,
        totalMs: acc.totalMs[def.key] ?? 0,
        errors: acc.errors[def.key] ?? 0,
        reactionGesture: reaction?.g,
        reactionUntil: reaction?.until,
      };
    }
    return { views, lastActivity: acc.lastActivity };
  }, [acc, plan]);
}

// detailFor extracts the specific task a tool is working on from its JSON
// input preview, so the office reads "联网搜索 · 2026 模型报价" not a generic
// spinner. Best-effort.
function detailFor(tool: string, input?: string): string {
  const obj = tryParse(input);
  switch (tool) {
    case "web_search":
    case "web_research":
    case "tavily_search":
      return str(obj?.query) || str(obj?.q) || trunc(input);
    case "code_execute":
      return str(obj?.language) ? `执行 ${str(obj?.language)} 代码` : "执行代码";
    case "generate_image":
      return str(obj?.caption) || str(obj?.prompt) || "生成配图";
    case "edit_image": {
      const opZh: Record<string, string> = {
        remove_bg: "抠图",
        enhance: "高清化",
        colorize: "上色",
        outpaint: "扩图",
        img2img: "图生图",
      };
      return opZh[str(obj?.op)] || "修图";
    }
    case "generate_poster":
      return str(obj?.title) ? `设计海报「${str(obj?.title)}」` : "设计海报";
    case "generate_storybook":
      return "绘制绘本";
    case "write_document":
      return "撰写报告";
    case "generate_deck":
      return "生成幻灯片";
    case "generate_video":
      return str(obj?.caption) || str(obj?.prompt) || "制作短视频";
    case "plan_tasks":
      return "规划任务";
    case "update_task":
      return "勾选进度";
    case "terminate":
      return "收尾";
    default:
      return trunc(input);
  }
}

function tryParse(s?: string): Record<string, unknown> | null {
  if (!s) return null;
  try {
    const v = JSON.parse(s);
    return v && typeof v === "object" ? (v as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}
function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}
function trunc(s?: string): string {
  if (!s) return "";
  const t = s.trim();
  return t.length > 32 ? t.slice(0, 32) + "…" : t;
}
