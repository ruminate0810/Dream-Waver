// Template + layout catalogues. Shared by /slides/new (gallery picker)
// and the OutlineReviewCard chip rail (Sprint S). Sprint T centralises
// what was previously inlined in apps/web/app/slides/new/page.tsx so
// both surfaces stay in sync without copy-paste drift.
//
// These are static, hand-curated lists. The 11 theme keys must match
// services/orchestrator/internal/schema/slides.go (schema.Theme enum)
// and the 9 layouts are a subset of schema.SlideLayout — chosen because
// they're the visually-distinctive layouts worth marketing on the
// landing page. Sprint T deliberately does NOT show all 25 layouts;
// the boring ones (title/section/content/bullets) live in the
// OutlineReviewCard per-slide picker instead.

export type Template = {
  /** schema.Theme key — must match a CSS file in packages/slide-templates/_themes */
  name: string;
  label: string;
  description: string;
  best_for: string[];
  /** Primary brand colour, hex. Surfaced as a swatch on the card. */
  primary_color: string;
  /** Accent brand colour, hex. */
  accent_color: string;
  /** Path to the 16:9 PNG (or SVG, for noir) thumbnail under public/. */
  thumbnail: string;
  /** Three 16:9 previews surfaced in the preview modal (Sprint Y3). */
  previews: string[];
};

export const TEMPLATES: Template[] = [
  {
    name: "minimalist",
    label: "Minimalist",
    description: "Clean white canvas, single blue accent, lots of whitespace.",
    best_for: ["product updates", "internal memos"],
    primary_color: "#0F172A",
    accent_color: "#2563EB",
    thumbnail: "/theme-previews/minimalist-1.png",
    previews: [
      "/theme-previews/minimalist-1.png",
      "/theme-previews/minimalist-2.png",
      "/theme-previews/minimalist-3.png",
    ],
  },
  {
    name: "corporate",
    label: "Corporate",
    description:
      "Structured business deck with navy header bar and amber accent rule.",
    best_for: ["consulting decks", "sales proposals"],
    primary_color: "#1E3A8A",
    accent_color: "#F59E0B",
    thumbnail: "/theme-previews/corporate-1.png",
    previews: [
      "/theme-previews/corporate-1.png",
      "/theme-previews/corporate-2.png",
      "/theme-previews/corporate-3.png",
    ],
  },
  {
    name: "pitch-deck",
    label: "Pitch Deck",
    description:
      "Linear/Vercel-style dark canvas with orange-to-amber gradient metrics.",
    best_for: ["investor pitches", "fundraising"],
    primary_color: "#050505",
    accent_color: "#FF6B35",
    thumbnail: "/theme-previews/pitch-deck-1.png",
    previews: [
      "/theme-previews/pitch-deck-1.png",
      "/theme-previews/pitch-deck-2.png",
      "/theme-previews/pitch-deck-3.png",
    ],
  },
  {
    name: "academic",
    label: "Academic",
    description:
      "Scholarly journal aesthetic with IBM Plex Serif and footnote-style bullets.",
    best_for: ["research talks", "lectures"],
    primary_color: "#18181B",
    accent_color: "#B91C1C",
    thumbnail: "/theme-previews/academic-1.png",
    previews: [
      "/theme-previews/academic-1.png",
      "/theme-previews/academic-2.png",
      "/theme-previews/academic-3.png",
    ],
  },
  {
    name: "playful",
    label: "Playful",
    description:
      "Dark canvas with multi-color radial accents and rounded badges. Built for creator content.",
    best_for: ["小红书 explainers", "B 站 课程"],
    primary_color: "#0A0A0F",
    accent_color: "#F472B6",
    thumbnail: "/theme-previews/playful-1.png",
    previews: [
      "/theme-previews/playful-1.png",
      "/theme-previews/playful-2.png",
      "/theme-previews/playful-3.png",
    ],
  },
  {
    name: "editorial",
    label: "Editorial",
    description:
      "NYT/New Yorker magazine register. Cream paper, vermillion accent, Fraunces serif.",
    best_for: ["long-read talks", "thought leadership"],
    primary_color: "#1A1614",
    accent_color: "#B5371E",
    thumbnail: "/theme-previews/editorial-1.png",
    previews: [
      "/theme-previews/editorial-1.png",
      "/theme-previews/editorial-2.png",
      "/theme-previews/editorial-3.png",
    ],
  },
  {
    name: "retro",
    label: "Retro (80s)",
    description:
      "Synthwave neon. Magenta-purple gradient, CRT scanlines, neon-glow text shadow.",
    best_for: ["game dev launches", "hackathons"],
    primary_color: "#0F0524",
    accent_color: "#FF2D95",
    thumbnail: "/theme-previews/retro-1.png",
    previews: [
      "/theme-previews/retro-1.png",
      "/theme-previews/retro-2.png",
      "/theme-previews/retro-3.png",
    ],
  },
  {
    name: "tech",
    label: "Tech / Terminal",
    description:
      "IDE-aesthetic deck. JetBrains Mono, $ prompt prefix, macOS traffic-light dots.",
    best_for: ["open-source releases", "dev tools"],
    primary_color: "#0B0E13",
    accent_color: "#4ADE80",
    thumbnail: "/theme-previews/tech-1.png",
    previews: [
      "/theme-previews/tech-1.png",
      "/theme-previews/tech-2.png",
      "/theme-previews/tech-3.png",
    ],
  },
  {
    name: "zen",
    label: "Zen",
    description:
      "Japanese minimalism. Washi paper, sumi ink, 朱印 accent, asymmetric padding.",
    best_for: ["wellness & meditation", "design philosophy"],
    primary_color: "#1A1815",
    accent_color: "#B5371E",
    thumbnail: "/theme-previews/zen-1.png",
    previews: [
      "/theme-previews/zen-1.png",
      "/theme-previews/zen-2.png",
      "/theme-previews/zen-3.png",
    ],
  },
  {
    name: "warm",
    label: "Warm Paper",
    description:
      "Vintage kraft notebook. Cream-tan with paper grain, EB Garamond + Caveat handwritten.",
    best_for: ["personal blog talks", "手帐风课件"],
    primary_color: "#3D2E1F",
    accent_color: "#C84B1A",
    thumbnail: "/theme-previews/warm-1.png",
    previews: [
      "/theme-previews/warm-1.png",
      "/theme-previews/warm-2.png",
      "/theme-previews/warm-3.png",
    ],
  },
  {
    name: "noir",
    label: "Noir",
    description:
      "High-contrast cinematic. Pure black canvas, Bodoni Moda thin display, single acid cadmium-yellow accent. Designed to pair with B&W AI photography on photo-essay / split-image.",
    best_for: ["fashion editorial", "film & photography portfolios"],
    primary_color: "#0A0A0B",
    accent_color: "#E8FF3C",
    thumbnail: "/theme-previews/noir-1.svg",
    previews: [
      "/theme-previews/noir-1.svg",
      "/theme-previews/noir-2.svg",
      "/theme-previews/noir-3.svg",
    ],
  },
];

export function findTemplate(name: string): Template | undefined {
  return TEMPLATES.find((t) => t.name === name);
}

// ─── Image-led / IA layouts ──────────────────────────────────────────
//
// Informational thumbnails so users see what "photo-essay" /
// "split-image" / "process-flow" actually look like before they type
// the topic. NOT pickers — the planner picks layout per-slide; this is
// pure marketing for the AI-imagery + Sprint K1 information-architecture
// capabilities. The read-only Composition gallery on /slides/new
// consumes this list.

export type LayoutExample = {
  /** Matches schema.SlideLayout const value. */
  key: string;
  label: string;
  /** Long-form pitch sentence. */
  tagline: string;
  /** SVG path under public/, vector so it scales crisp at any size. */
  thumb: string;
  /** One-line use-case hint, surfaced as a top-right vermillion mono caption. */
  when: string;
};

export const LAYOUT_EXAMPLES: LayoutExample[] = [
  // Sprint H8 — image-led
  {
    key: "photo-essay",
    label: "Photo essay",
    tagline:
      "全屏 AI 图 + 一行编辑式标题。摄影集 / 旅行日记 / 章节过场。",
    thumb: "/layout-previews/photo-essay.svg",
    when: "摄影 · 旅行 · 视觉",
  },
  {
    key: "split-image",
    label: "Split image",
    tagline:
      "50:50 杂志大跨页。图一半 + 标题段落要点一半。产品 / 案例 / 人物。",
    thumb: "/layout-previews/split-image.svg",
    when: "产品 · 案例 · 美食",
  },
  {
    key: "image-grid",
    label: "Image grid",
    tagline:
      "3-4 张 AI 图组合 moodboard，一行斜体说明。Look book / 范例集合。",
    thumb: "/layout-previews/image-grid.svg",
    when: "时尚 · 范例 · 概念集",
  },
  // Sprint K1 — information architectures
  {
    key: "process-flow",
    label: "Process flow",
    tagline:
      "3-5 步骤水平排列，编号+箭头。流程图 / 步骤指南 / how-to。",
    thumb: "/layout-previews/process-flow.svg",
    when: "流程 · 步骤 · 实施",
  },
  {
    key: "bento-grid",
    label: "Bento grid",
    tagline:
      "苹果风混排：1 大卡 + 3-4 小卡，文字/数字/图自由混。产品全貌。",
    thumb: "/layout-previews/bento-grid.svg",
    when: "综合介绍 · overview",
  },
  {
    key: "pull-quote",
    label: "Pull quote",
    tagline: "上下文 + 巨大引语 + 出处。重磅观点的「段落高潮」。",
    thumb: "/layout-previews/pull-quote.svg",
    when: "关键观点 · 重磅论断",
  },
  {
    key: "before-after",
    label: "Before / after",
    tagline:
      "左右两张 AI 图对比 + 「Before」「After」标签。改造 / 蜕变。",
    thumb: "/layout-previews/before-after.svg",
    when: "改造 · 蜕变 · makeover",
  },
  {
    key: "icon-grid",
    label: "Icon grid",
    tagline:
      "3-4 列 emoji icon + 标题 + 短描述。功能介绍 / capabilities。",
    thumb: "/layout-previews/icon-grid.svg",
    when: "功能 · 特性 · 能力",
  },
  {
    key: "team-roster",
    label: "Team roster",
    tagline:
      "横向头像 + 名字 + 头衔卡片。团队 / 创始人 / 演讲嘉宾。",
    thumb: "/layout-previews/team-roster.svg",
    when: "团队 · founders · 嘉宾",
  },
];
