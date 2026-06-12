import type { AgentEvent } from "@/components/chat/transport";
import type { ClawRun } from "@/lib/api";
import { WORKERS } from "./workers";

// narrate.ts — turns raw agent events into a readable, in-character team
// conversation (角色发声) and offers context-aware next steps (下一步 chips),
// so the chat guides the user instead of dumping a tool log.

const ZH: Record<string, string> = Object.fromEntries(WORKERS.map((w) => [w.key, w.zh]));
export function workerColor(key: string): string {
  return WORKERS.find((w) => w.key === key)?.shirt ?? "#6a55ff";
}

// first-person intro a worker says when it picks up its first task
const INTRO: Record<string, (detail: string) => string> = {
  web_search: (d) => `这块我去查 — ${d || "联网核实事实与来源"}。`,
  code_execute: () => "数据交给我,我跑段代码算清楚。",
  generate_image: (d) => `我来画${d ? `「${d}」` : "张配图"}。`,
  write_document: () => "材料齐了,我开始整合成报告。",
  generate_deck: () => "报告我拿去做成幻灯片。",
  generate_video: () => "我把这张图动起来,做成一段短片。",
};

// a worker's closing line on a meaningful success
const DONE: Record<string, string> = {
  code_execute: "算出来了 ✓",
  generate_image: "配图出炉啦 ✦",
  generate_deck: "幻灯片做好了 ✓",
  generate_video: "短片剪好了 ►",
};

export type SayLine = { worker: string; text: string };

// narrationFor decides whether an event produces a spoken line. `said` dedupes
// (one intro per worker, one done-line per worker+tool, one error gripe per
// worker) so the feed reads like a conversation, not a stream.
export function narrationFor(ev: AgentEvent, said: Set<string>): SayLine | null {
  const d = ev.data;

  if (ev.kind === "claw.plan") {
    if (said.has("plan")) return null;
    said.add("plan");
    const titles = d.task_titles ?? [];
    const roles = [...new Set((d.task_roles ?? []).filter(Boolean))]
      .map((r) => ZH[r] ?? r)
      .filter((z) => z !== "撰稿员");
    const who = roles.length ? `,先让${roles.slice(0, 3).join("、")}并行动起来` : "";
    return { worker: "coordinator", text: `这活我拆成了 ${titles.length} 步${who}。` };
  }

  if (ev.kind === "tool.start") {
    const w = d.agent;
    if (!w || !ZH[w]) return null;
    const verb = INTRO[d.tool_name ?? ""];
    if (!verb) return null;
    const key = `intro:${w}`;
    if (said.has(key)) return null;
    said.add(key);
    return { worker: w, text: verb(detailOf(d)) };
  }

  if (ev.kind === "tool.end") {
    const w = d.agent;
    if (!w || !ZH[w]) return null;
    if (d.error) {
      const key = `err:${w}`;
      if (said.has(key)) return null;
      said.add(key);
      return { worker: w, text: "诶,卡了一下,我换个法子再来。" };
    }
    if (d.tool_name === "write_document") {
      const key = `wd:${w}`;
      if (said.has(key)) return null;
      said.add(key);
      return w === "critic"
        ? { worker: w, text: "审完了,挑出几处改好,交终稿。" }
        : { worker: w, text: "初稿写好了,交给评审把关。" };
    }
    const line = DONE[d.tool_name ?? ""];
    if (line) {
      const key = `done:${w}:${d.tool_name}`;
      if (said.has(key)) return null;
      said.add(key);
      return { worker: w, text: line };
    }
  }
  return null;
}

export function workerZh(key: string): string {
  return ZH[key] ?? key;
}

function detailOf(d: AgentEvent["data"]): string {
  const obj = tryParse(d.tool_input);
  return (
    str(obj?.query) ||
    str(obj?.q) ||
    str(obj?.caption) ||
    str(obj?.prompt) ||
    ""
  ).slice(0, 28);
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

// ── 下一步 chips — context-aware follow-ups offered once a report exists ──
export type NextStep = { label: string; text: string };

export function nextSteps(run: ClawRun): NextStep[] {
  const out: NextStep[] = [
    { label: "一句话结论", text: "在报告开头加一段 50 字以内的核心结论摘要。" },
    { label: "补风险/局限", text: "在报告末尾补一段「风险与局限」,说明结论的前提和不确定性。" },
    { label: "再深入重点", text: "挑报告里最关键的一节,再深入展开、补充细节与数据。" },
  ];
  if ((run.figures?.length ?? 0) === 0) {
    out.push({ label: "加配图", text: "给报告配 1–2 张说明性示意图。" });
  }
  // 修图 (edit_image): only once there's a figure to work on. op is natural-
  // language — "把第 2 张图抠图"/"扩成 16:9" work too.
  if ((run.figures?.length ?? 0) > 0) {
    out.push({ label: "高清化配图", text: "让设计师把最新那张配图高清化(enhance),让细节更清晰。" });
    out.push({ label: "抠图去背景", text: "让设计师把最新那张配图抠图(remove_bg),留下透明背景的主体。" });
  }
  // i2v: only once there's a figure to animate, and no clip yet. Resolution /
  // duration are natural-language — "做成 1080p 10 秒" works too.
  if ((run.figures?.length ?? 0) > 0 && (run.videos?.length ?? 0) === 0) {
    out.push({ label: "做成短视频", text: "让视频师把最关键的那张配图做成一段 720p、5 秒的短视频,加点运镜和光影动态。" });
  }
  if (!run.deck) {
    out.push({ label: "做成 PPT", text: "把这份报告做成一份幻灯片 deck。" });
  }
  out.push({ label: "换成人民币", text: "把报告里的金额/价格换算成人民币,保留原币种备注。" });
  return out;
}
