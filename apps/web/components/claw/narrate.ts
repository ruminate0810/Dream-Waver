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

// first-person intro a worker says when it picks up its first task —
// in-character per persona (调研员=情报猎手, 工程师=沉默键盘侠, 设计师=像素
// 洁癖, 撰稿员=咬文嚼字, 制片=赶档期, 视频师=运镜狂魔)
const INTRO: Record<string, (detail: string) => string> = {
  web_search: (d) => `交给我去刨 — ${d || "联网核实事实与来源"}。出处不硬我不收工。`,
  code_execute: () => "(推了推墨镜)数据给我,跑段代码就知道了。",
  generate_image: (d) => `我来画${d ? `「${d}」` : "张配图"} — 今天配色必须完美。`,
  edit_image: (d) => `这张图交给我精修 — ${d || "抠图、高清,安排"}。`,
  generate_poster: (d) => `海报我来 — ${d ? `主标题「${d}」` : "排版交给我"},一张就够抓眼球。`,
  generate_storybook: () => "这个适合做成绘本,我一格一格画,风格统一。",
  generate_variants: () => "方向没定?我出几版不同的,你挑。",
  write_document: () => "材料齐了,我开写 — 这稿起码润八遍。",
  generate_deck: () => "报告给我,今晚必须出片!幻灯片这就排。",
  generate_video: () => "上三脚架!我把这张图推、拉、摇、移起来。",
};

// a worker's closing line on a meaningful success
const DONE: Record<string, string> = {
  code_execute: "跑完了,数字不会骗人 ✓",
  generate_image: "配图出炉,一个像素都没歪 ✦",
  edit_image: "修好了,放大看也经得起 ✦",
  generate_poster: "海报出稿!标题够大、留白够透 ✦",
  generate_storybook: "绘本画好了,一页页连得上 ✦",
  generate_variants: "几个方案都摆出来了,挑一个 ✦",
  generate_deck: "片出了!档期保住了 ✓",
  generate_video: "成片!这条最稳 ►",
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
        ? { worker: w, text: "审完了。挑出几处毛病,都给你们改好了 — 终稿。" }
        : { worker: w, text: "初稿写好了。评审员,尽管来挑。" };
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
  // 多样产出 — the designer can turn this into more than a report
  out.push({ label: "做张海报", text: "让设计师把这个主题做成一张吸睛的宣传海报,主标题醒目、风格现代。" });
  out.push({ label: "做成绘本", text: "让设计师把核心内容改编成一本 4 页的插画绘本,画风温暖统一。" });
  if (!run.deck) {
    out.push({ label: "做成 PPT", text: "把这份报告做成一份幻灯片 deck。" });
  }
  out.push({ label: "换成人民币", text: "把报告里的金额/价格换算成人民币,保留原币种备注。" });
  return out;
}
