// workers.ts is the frontend half of Claw's extensibility seam — the data
// that drives the WorkerDesk, mirroring the backend roles.go registry. Each
// WorkerDef declares one pixel-worker: its identity (key = backend role key =
// the `agent` field on events), its character (shirt/accessory/desk SVG), and
// its action pool. Adding a worker = add an entry here + a Role in roles.go.

export const INK = "#16140f";
export const ACCENT = "#6a55ff";
export const SKIN = "#ffded1";

// Emote glyphs — mono text, pixel-consistent (no emoji).
export const EMO: Record<string, string> = {
  think: "?",
  idea: "!",
  done: "✓",
  search: "⌕",
  code: "</>",
  coffee: "☕",
  talk: "…",
  err: "✗",
  note: "✎",
  paint: "✦",
  music: "♪",
  zzz: "z²",
  heart: "♥",
  film: "►",
};

// Action pool: each key has a zh status label + an optional emote glyph. The
// CSS classes (.claw-act-<key>) live in globals.css.
export const ACT: Record<string, { zh: string; emo?: string }> = {
  type: { zh: "敲键盘" },
  think: { zh: "思考中", emo: "think" },
  scratch: { zh: "挠头琢磨", emo: "think" },
  eureka: { zh: "有发现!", emo: "idea" },
  look: { zh: "盯屏幕" },
  nod: { zh: "确认", emo: "done" },
  point: { zh: "指派", emo: "talk" },
  write: { zh: "记录", emo: "note" },
  magnify: { zh: "检索资料", emo: "search" },
  coffee: { zh: "喝口咖啡", emo: "coffee" },
  draw: { zh: "作图中", emo: "paint" },
  read: { zh: "翻阅资料" },
  talk: { zh: "沟通中", emo: "talk" },
  facepalm: { zh: "报错抱头", emo: "err" },
  wave: { zh: "打招呼", emo: "heart" },
  cheer: { zh: "欢呼!", emo: "done" },
  dance: { zh: "摸鱼热舞", emo: "music" },
  doze: { zh: "打瞌睡", emo: "zzz" },
  phone: { zh: "电话沟通", emo: "talk" },
  stretch: { zh: "伸个懒腰" },
  film: { zh: "拍摄中", emo: "film" },
};

// TOOL_ACTION maps a live tool to the worker's signature gesture, so the body
// reflects what the worker is actually doing (not a random animation). The
// status/label is driven by the real task detail separately.
export const TOOL_ACTION: Record<string, string> = {
  plan_tasks: "point",
  update_task: "nod",
  web_search: "magnify",
  web_research: "magnify",
  tavily_search: "magnify",
  code_execute: "type",
  generate_image: "draw",
  edit_image: "draw",
  write_document: "write",
  generate_deck: "point",
  generate_video: "film",
};

// Pipeline phase a worker belongs to. The office derives EVERYTHING
// positional/sequential from this + array order (desk layout, the phase
// strip, handoff-flight sources), so adding an agent = adding one entry.
export type WorkerPhase = "plan" | "exec" | "write" | "review" | "produce";

export type WorkerDef = {
  key: string; // == backend role key == event `agent`
  name: string; // English plate
  zh: string; // 中文 plate
  phase: WorkerPhase;
  tools: string[]; // tools that attribute to this worker (fallback when no agent field)
  shirt: string;
  shirtDark?: string; // right-side shading on the shirt (derived look)
  hair: string; // hair colour
  hairKind?: "short" | "long" | "none"; // none = a hat accessory covers it
  skin?: string; // per-character skin tone (defaults to SKIN)
  pants?: string; // trouser colour (defaults to ink)
  badge?: string;
  acc: string; // SVG fragment drawn inside the head group (hat / glasses)
  deskTool: string; // SVG fragment for the desk monitor/prop
  actions: string[]; // ~10 actions cycled for liveliness while working
};

export const WORKERS: WorkerDef[] = [
  {
    key: "coordinator",
    phase: "plan",
    name: "Coordinator",
    zh: "调度员",
    tools: ["plan_tasks", "update_task"],
    shirt: "#6a55ff",
    shirtDark: "#5142cc",
    hair: "#2a2620",
    hairKind: "short",
    pants: "#35405a",
    badge: "#fff",
    acc: `<rect x="8" y="0" width="12" height="1" fill="#2a2620"/><rect x="7" y="1" width="1" height="2" fill="#2a2620"/><rect x="20" y="1" width="1" height="2" fill="#2a2620"/><rect x="6" y="7" width="2" height="4" fill="#2a2620"/><rect x="20" y="7" width="2" height="4" fill="#2a2620"/><rect x="5" y="11" width="1" height="1" fill="#2a2620"/><rect x="5" y="12" width="3" height="1" fill="${ACCENT}"/>`,
    deskTool: `<rect x="9" y="5" width="15" height="12" fill="${INK}"/><rect x="10" y="6" width="13" height="10" class="claw-screen" fill="#4a4439"/><rect x="12" y="8" width="9" height="1" fill="#cdd6e6"/><rect x="12" y="10" width="6" height="1" fill="#cdd6e6"/><rect x="12" y="12" width="8" height="1" fill="#cdd6e6"/><rect x="12" y="14" width="5" height="1" fill="#cdd6e6"/>`,
    actions: ["look", "point", "phone", "talk", "nod", "think", "write", "coffee", "eureka", "wave"],
  },
  {
    key: "researcher",
    phase: "exec",
    name: "Researcher",
    zh: "调研员",
    tools: ["web_search", "web_research", "tavily_search"],
    shirt: "#3a7bd5",
    shirtDark: "#2c61ab",
    hair: "#7a4a23",
    hairKind: "short",
    skin: "#f3c9a8",
    pants: "#3d4a3a",
    acc: `<rect x="9" y="8" width="4" height="3" fill="#cfe9f7" opacity="0.5"/><rect x="15" y="8" width="4" height="3" fill="#cfe9f7" opacity="0.5"/><rect x="13" y="9" width="2" height="1" fill="#2a2620"/><rect x="8" y="9" width="1" height="1" fill="#2a2620"/><rect x="19" y="9" width="1" height="1" fill="#2a2620"/>`,
    deskTool: `<rect x="9" y="5" width="14" height="12" fill="${INK}"/><rect x="10" y="6" width="12" height="10" class="claw-screen" fill="#4a4439"/><rect x="13" y="8" width="5" height="5" fill="none" stroke="#cdd6e6" stroke-width="1"/><rect x="17" y="12" width="3" height="1.4" fill="#cdd6e6" transform="rotate(45 17 12)"/>`,
    actions: ["magnify", "look", "type", "think", "scratch", "read", "eureka", "write", "nod", "coffee"],
  },
  {
    key: "engineer",
    phase: "exec",
    name: "Engineer",
    zh: "工程师",
    tools: ["code_execute"],
    shirt: "#e3a13a",
    shirtDark: "#bd811f",
    hair: "#2a2620",
    hairKind: "none", // hard hat
    skin: "#e8b48c",
    pants: "#4f4636",
    acc: `<rect x="8" y="0" width="12" height="2" fill="#f0b13f"/><rect x="7" y="2" width="14" height="2" fill="#f0b13f"/><rect x="9" y="0" width="4" height="1" fill="#f7cd7a"/><rect x="5" y="4" width="18" height="1" fill="#c98a1d"/>`,
    deskTool: `<rect x="8" y="6" width="16" height="11" fill="${INK}"/><rect x="9" y="7" width="14" height="9" class="claw-screen" fill="#2a2620"/><rect x="11" y="9" width="3" height="1" fill="#74e6a0"/><rect x="15" y="9" width="6" height="1" fill="#b8aaff"/><rect x="11" y="11" width="7" height="1" fill="#74e6a0"/><rect x="11" y="13" width="4" height="1" fill="#74e6a0"/>`,
    actions: ["type", "look", "think", "facepalm", "nod", "eureka", "cheer", "coffee", "point", "scratch"],
  },
  {
    key: "designer",
    phase: "exec",
    name: "Designer",
    zh: "设计师",
    tools: ["generate_image", "edit_image"],
    shirt: "#d9569b",
    shirtDark: "#b03d7c",
    hair: "#16140f",
    hairKind: "long",
    pants: "#5a3550",
    acc: `<rect x="7" y="0" width="12" height="2" fill="#16140f"/><rect x="8" y="2" width="11" height="1" fill="#16140f"/><rect x="17" y="1" width="4" height="1" fill="#16140f"/><rect x="18" y="0" width="1" height="1" fill="#e3a13a"/>`,
    deskTool: `<rect x="10" y="3" width="13" height="11" fill="#fbfaf2" stroke="${INK}" stroke-width="1"/><rect x="12" y="6" width="5" height="5" class="claw-screen" fill="#d9569b"/><rect x="18" y="5" width="3" height="3" fill="#74e6a0"/><rect x="9" y="14" width="2" height="4" fill="var(--desk-d, #84591c)"/><rect x="22" y="14" width="2" height="4" fill="var(--desk-d, #84591c)"/>`,
    actions: ["draw", "look", "think", "eureka", "point", "nod", "draw", "dance", "coffee", "scratch"],
  },
  {
    key: "writer",
    phase: "write",
    name: "Writer",
    zh: "撰稿员",
    tools: ["write_document"],
    shirt: "#b5371e",
    shirtDark: "#8e2a15",
    hair: "#5a4632",
    hairKind: "short",
    pants: "#3a3531",
    acc: `<rect x="8" y="2" width="12" height="1" fill="#6a55ff"/><rect x="20" y="7" width="1" height="4" fill="#e3a13a"/><rect x="20" y="6" width="1" height="1" fill="#d9569b"/>`,
    deskTool: `<rect x="12" y="2" width="9" height="6" fill="#fbfaf2"/><rect x="13" y="4" width="6" height="1" fill="#c9c3b5"/><rect x="13" y="6" width="5" height="1" fill="#c9c3b5"/><rect x="9" y="8" width="15" height="2" fill="#403a30"/><rect x="10" y="10" width="13" height="6" fill="${INK}"/><rect x="11" y="11" width="11" height="4" class="claw-screen" fill="#4a4439"/><rect x="12" y="12" width="2" height="1" fill="#d9d3c5"/><rect x="15" y="12" width="2" height="1" fill="#d9d3c5"/><rect x="18" y="12" width="2" height="1" fill="#d9d3c5"/>`,
    actions: ["type", "think", "scratch", "read", "eureka", "write", "nod", "coffee", "cheer", "talk"],
  },
  {
    key: "critic",
    phase: "review",
    name: "Reviewer",
    zh: "评审员",
    tools: [], // purely agent-attributed (shares write_document with the writer)
    shirt: "#2a9d8f",
    shirtDark: "#1e7a6f",
    hair: "#b9b3a6",
    hairKind: "short",
    skin: "#f3c9a8",
    pants: "#41444b",
    acc: `<rect x="9" y="7" width="4" height="1" fill="#2a2620"/><rect x="15" y="7" width="4" height="1" fill="#2a2620"/><rect x="9" y="8" width="4" height="3" fill="#ffffff" opacity="0.28"/><rect x="15" y="8" width="4" height="3" fill="#ffffff" opacity="0.28"/><rect x="13" y="9" width="2" height="1" fill="#2a2620"/><rect x="8" y="8" width="1" height="1" fill="#2a2620"/><rect x="19" y="8" width="1" height="1" fill="#2a2620"/>`,
    deskTool: `<rect x="10" y="3" width="11" height="13" fill="#fbfaf2" stroke="${INK}" stroke-width="1"/><rect x="12" y="6" width="7" height="1" fill="#c9c3b5"/><rect x="12" y="8" width="6" height="1" fill="#c9c3b5"/><rect x="12" y="10" width="7" height="1" fill="#74c4b5"/><circle cx="19" cy="12" r="3.4" fill="none" stroke="#2a9d8f" stroke-width="1.4"/><rect x="21" y="14" width="3" height="1.5" fill="#2a9d8f" transform="rotate(45 21 14)"/>`,
    actions: ["read", "magnify", "nod", "think", "write", "scratch", "eureka", "point", "coffee", "look"],
  },
  {
    key: "producer",
    phase: "produce",
    name: "Producer",
    zh: "制片",
    tools: ["generate_deck"],
    shirt: "#2aa198",
    shirtDark: "#1f7d76",
    hair: "#16140f",
    hairKind: "none", // flat cap
    skin: "#e8b48c",
    pants: "#2f2b3a",
    acc: `<rect x="7" y="0" width="13" height="2" fill="#1f2430"/><rect x="7" y="2" width="14" height="2" fill="#1f2430"/><rect x="15" y="4" width="7" height="1" fill="#141821"/><rect x="9" y="8" width="4" height="3" fill="#16140f" opacity="0.88"/><rect x="15" y="8" width="4" height="3" fill="#16140f" opacity="0.88"/><rect x="13" y="8" width="2" height="1" fill="#16140f"/>`,
    deskTool: `<rect x="9" y="4" width="14" height="3" fill="${INK}"/><rect x="10" y="4" width="1" height="3" fill="#fff"/><rect x="12" y="4" width="1" height="3" fill="#fff"/><rect x="14" y="4" width="1" height="3" fill="#fff"/><rect x="9" y="7" width="14" height="9" class="claw-screen" fill="#2a2620"/><rect x="11" y="10" width="10" height="1" fill="#cdd6e6"/><rect x="11" y="12" width="7" height="1" fill="#cdd6e6"/>`,
    actions: ["point", "phone", "nod", "look", "type", "eureka", "talk", "coffee", "read", "think"],
  },
  {
    key: "videographer",
    phase: "produce",
    name: "Videographer",
    zh: "视频师",
    tools: ["generate_video"],
    shirt: "#7c5cbf",
    shirtDark: "#5e4494",
    hair: "#2a2620",
    hairKind: "short",
    skin: "#f0cfb0",
    pants: "#2f2b3a",
    // headphones — band over the head + two ear cups
    acc: `<rect x="7" y="1" width="14" height="1" fill="#2a2620"/><rect x="6" y="2" width="1" height="1" fill="#2a2620"/><rect x="21" y="2" width="1" height="1" fill="#2a2620"/><rect x="5" y="7" width="2" height="4" fill="#2a2620"/><rect x="21" y="7" width="2" height="4" fill="#2a2620"/><rect x="5" y="8" width="1" height="2" fill="#b8aaff"/>`,
    // a video monitor: play triangle + a film-strip footer
    deskTool: `<rect x="8" y="5" width="16" height="11" fill="${INK}"/><rect x="9" y="6" width="14" height="8" class="claw-screen" fill="#1f1a2e"/><polygon points="14,8 14,12 18,10" fill="#b8aaff"/><rect x="9" y="14" width="14" height="2" fill="#2a2620"/><rect x="10" y="14.5" width="1" height="1" fill="#fff"/><rect x="13" y="14.5" width="1" height="1" fill="#fff"/><rect x="16" y="14.5" width="1" height="1" fill="#fff"/><rect x="19" y="14.5" width="1" height="1" fill="#fff"/>`,
    actions: ["film", "look", "think", "point", "nod", "eureka", "draw", "coffee", "cheer", "talk"],
  },
];

export function workerForTool(tool: string): string | undefined {
  for (const w of WORKERS) if (w.tools.includes(tool)) return w.key;
  return undefined;
}
