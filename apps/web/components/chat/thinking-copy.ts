// Shared per-tool playful copy + helpers — extracted from ChatThread so
// other surfaces (e.g. DeckPhaseTimeline in the left rail) can show the
// SAME rotating active label as ThinkingRow. Single source of truth
// keeps the editorial voice consistent across the app.

import type { Turn } from "./session";

// TOOL_THINKING — per-tool playful Chinese copy. Each tool gets 2-3
// alternates that rotate every ~3.5s so a long-running tool doesn't
// look frozen. Editorial / 排版 / artisan voice — matches the Dream-
// Waver brand. No emoji, no generic "loading..." style.
export const TOOL_THINKING: Record<string, string[]> = {
  // Outline phase
  plan_outline: ["排兵布阵中…", "搭骨架…", "拟章节…", "勾草稿…"],
  critic_outline: ["挑刺中…", "用红笔批改…", "审章节安排…"],
  revise_outline: ["推敲措辞…", "改章节标题…"],
  // Content phase
  write_content: ["字字斟酌…", "翻译思想为段落…", "敲键盘中…", "组织语言…"],
  critic_content: ["审稿中…", "找逻辑漏洞…", "对照密度规则…"],
  revise_slide: ["改稿中…", "重写这页…", "推敲第 N 页…"],
  // Render
  render_deck: ["开印…", "送进打字机…", "排版定型…", "上墨…"],
  // Reflection
  analyze_deck: ["全景扫描…", "盘点 deck…", "看看整体…"],
  critic_deck: ["通读全文…", "找整体不协调…", "把关版面…"],
  // Bulk edit
  rewrite_for_density: ["把每页填满…", "增加分量…", "削掉冗余…"],
  diversify_layouts: ["打破模板感…", "换花样…", "重新混排…"],
  // Single edit
  style_slide: ["调字号…", "微调排版…"],
  convert_layout: ["换 layout…"],
  merge_slides: ["合页…"],
  split_slide: ["拆页…"],
  add_slide: ["新加一页…", "想这页讲啥…"],
  delete_slide: ["删页…"],
  duplicate_slide: ["复制一份…"],
  reorder_slide: ["调换顺序…"],
  edit_slide_text: ["改文字…"],
  edit_speaker_notes: ["补讲稿…"],
  set_footer: ["改页脚…"],
  change_theme: ["换主题…", "换装中…"],
  apply_brand: ["套品牌色…"],
  generate_image: ["AI 画图中…", "找配图…"],
  web_search: ["上网查…", "搜资料…", "翻新闻…"],
  terminate: ["收工…"],
};

export const PREPARING_THINKING = ["正在准备工具…", "想想该做什么…", "决策中…"];
export const EDITING_THINKING = ["正在改…", "动手中…", "改这页…"];
export const DEFAULT_THINKING = ["thinking…", "动脑中…", "想…"];

export function pickThinkingPool(
  hint?: "preparing" | "editing",
  activeToolName?: string,
): string[] {
  if (activeToolName) {
    const pool = TOOL_THINKING[activeToolName];
    if (pool && pool.length > 0) return pool;
    return [activeToolName.replace(/_/g, " ") + "…"];
  }
  if (hint === "editing") return EDITING_THINKING;
  if (hint === "preparing") return PREPARING_THINKING;
  return DEFAULT_THINKING;
}

// getActiveToolName returns the name of the most recent in-flight tool
// call on a turn. Used by both ThinkingRow and DeckPhaseTimeline to
// surface per-tool playful copy as the active sub-label.
export function getActiveToolName(turn: Turn | undefined): string | undefined {
  if (!turn) return undefined;
  for (let i = turn.steps.length - 1; i >= 0; i--) {
    const step = turn.steps[i];
    for (let j = step.toolCalls.length - 1; j >= 0; j--) {
      if (step.toolCalls[j].status === "running") {
        return step.toolCalls[j].name;
      }
    }
  }
  return undefined;
}
