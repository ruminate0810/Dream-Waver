"use client";

import {
  Suspense,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { createPortal } from "react-dom";
import { useRouter, useSearchParams } from "next/navigation";
import { ArrowLeft, ArrowUpRight, Check, ChevronDown, Loader2, X } from "lucide-react";
import clsx from "clsx";

import { createSlides } from "@/lib/api";
import { parseTopic } from "@/lib/parseHints";
import { rememberDeck } from "@/lib/recentDecks";

// /slides/new is the chat-first entrance to a new deck.
//
// Top half: one big textarea (Enter = submit; Shift+Enter = newline)
// plus a collapsible Fine-tune row for page count.
//
// Bottom half: template gallery — a 5-column grid of REAL deck
// thumbnails (page-1 PNG of each theme). Click any card → modal
// preview with 3 sample slides + "Use this style" CTA that pins the
// theme and closes the modal. Submission still happens via the
// textarea.

// Templates inlined from packages/slide-templates/manifest.json. Keep
// in sync if you add a new theme.
type Template = {
  name: string;
  label: string;
  description: string;
  best_for: string[];
  primary_color: string;
  accent_color: string;
  thumbnail: string;
  previews: string[];
};

// H8 image-led layouts — informational thumbnails so users see what
// "photo-essay" / "split-image" / "image-grid" actually look like
// before they type the topic. Not pickers (the planner picks layout
// per-slide); just visual marketing for the AI-imagery capability.
type LayoutExample = {
  key: string;
  label: string;
  tagline: string;
  thumb: string; // SVG path in /public — vector, scales crisp at any size
  when: string;
};
const LAYOUT_EXAMPLES: LayoutExample[] = [
  // ── H8: image-led ──
  {
    key: "photo-essay",
    label: "Photo essay",
    tagline: "全屏 AI 图 + 一行编辑式标题。摄影集 / 旅行日记 / 章节过场。",
    thumb: "/layout-previews/photo-essay.svg",
    when: "摄影 · 旅行 · 视觉",
  },
  {
    key: "split-image",
    label: "Split image",
    tagline: "50:50 杂志大跨页。图一半 + 标题段落要点一半。产品 / 案例 / 人物。",
    thumb: "/layout-previews/split-image.svg",
    when: "产品 · 案例 · 美食",
  },
  {
    key: "image-grid",
    label: "Image grid",
    tagline: "3-4 张 AI 图组合 moodboard，一行斜体说明。Look book / 范例集合。",
    thumb: "/layout-previews/image-grid.svg",
    when: "时尚 · 范例 · 概念集",
  },
  // ── K1: information architectures ──
  {
    key: "process-flow",
    label: "Process flow",
    tagline: "3-5 步骤水平排列，编号+箭头。流程图 / 步骤指南 / how-to。",
    thumb: "/layout-previews/process-flow.svg",
    when: "流程 · 步骤 · 实施",
  },
  {
    key: "bento-grid",
    label: "Bento grid",
    tagline: "苹果风混排：1 大卡 + 3-4 小卡，文字/数字/图自由混。产品全貌。",
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
    tagline: "左右两张 AI 图对比 + 「Before」「After」标签。改造 / 蜕变。",
    thumb: "/layout-previews/before-after.svg",
    when: "改造 · 蜕变 · makeover",
  },
  {
    key: "icon-grid",
    label: "Icon grid",
    tagline: "3-4 列 emoji icon + 标题 + 短描述。功能介绍 / capabilities。",
    thumb: "/layout-previews/icon-grid.svg",
    when: "功能 · 特性 · 能力",
  },
  {
    key: "team-roster",
    label: "Team roster",
    tagline: "横向头像 + 名字 + 头衔卡片。团队 / 创始人 / 演讲嘉宾。",
    thumb: "/layout-previews/team-roster.svg",
    when: "团队 · founders · 嘉宾",
  },
];

const TEMPLATES: Template[] = [
  { name: "minimalist", label: "Minimalist",  description: "Clean white canvas, single blue accent, lots of whitespace.",                 best_for: ["product updates", "internal memos"],     primary_color: "#0F172A", accent_color: "#2563EB", thumbnail: "/theme-previews/minimalist-1.png", previews: ["/theme-previews/minimalist-1.png","/theme-previews/minimalist-2.png","/theme-previews/minimalist-3.png"] },
  { name: "corporate",  label: "Corporate",   description: "Structured business deck with navy header bar and amber accent rule.",       best_for: ["consulting decks", "sales proposals"],   primary_color: "#1E3A8A", accent_color: "#F59E0B", thumbnail: "/theme-previews/corporate-1.png",  previews: ["/theme-previews/corporate-1.png","/theme-previews/corporate-2.png","/theme-previews/corporate-3.png"] },
  { name: "pitch-deck", label: "Pitch Deck",  description: "Linear/Vercel-style dark canvas with orange-to-amber gradient metrics.",     best_for: ["investor pitches", "fundraising"],       primary_color: "#050505", accent_color: "#FF6B35", thumbnail: "/theme-previews/pitch-deck-1.png", previews: ["/theme-previews/pitch-deck-1.png","/theme-previews/pitch-deck-2.png","/theme-previews/pitch-deck-3.png"] },
  { name: "academic",   label: "Academic",    description: "Scholarly journal aesthetic with IBM Plex Serif and footnote-style bullets.", best_for: ["research talks", "lectures"],            primary_color: "#18181B", accent_color: "#B91C1C", thumbnail: "/theme-previews/academic-1.png",   previews: ["/theme-previews/academic-1.png","/theme-previews/academic-2.png","/theme-previews/academic-3.png"] },
  { name: "playful",    label: "Playful",     description: "Dark canvas with multi-color radial accents and rounded badges. Built for creator content.", best_for: ["小红书 explainers", "B 站 课程"],     primary_color: "#0A0A0F", accent_color: "#F472B6", thumbnail: "/theme-previews/playful-1.png",    previews: ["/theme-previews/playful-1.png","/theme-previews/playful-2.png","/theme-previews/playful-3.png"] },
  { name: "editorial",  label: "Editorial",   description: "NYT/New Yorker magazine register. Cream paper, vermillion accent, Fraunces serif.",         best_for: ["long-read talks", "thought leadership"], primary_color: "#1A1614", accent_color: "#B5371E", thumbnail: "/theme-previews/editorial-1.png",  previews: ["/theme-previews/editorial-1.png","/theme-previews/editorial-2.png","/theme-previews/editorial-3.png"] },
  { name: "retro",      label: "Retro (80s)", description: "Synthwave neon. Magenta-purple gradient, CRT scanlines, neon-glow text shadow.",            best_for: ["game dev launches", "hackathons"],       primary_color: "#0F0524", accent_color: "#FF2D95", thumbnail: "/theme-previews/retro-1.png",      previews: ["/theme-previews/retro-1.png","/theme-previews/retro-2.png","/theme-previews/retro-3.png"] },
  { name: "tech",       label: "Tech / Terminal", description: "IDE-aesthetic deck. JetBrains Mono, $ prompt prefix, macOS traffic-light dots.",       best_for: ["open-source releases", "dev tools"],     primary_color: "#0B0E13", accent_color: "#4ADE80", thumbnail: "/theme-previews/tech-1.png",       previews: ["/theme-previews/tech-1.png","/theme-previews/tech-2.png","/theme-previews/tech-3.png"] },
  { name: "zen",        label: "Zen",         description: "Japanese minimalism. Washi paper, sumi ink, 朱印 accent, asymmetric padding.",              best_for: ["wellness & meditation", "design philosophy"], primary_color: "#1A1815", accent_color: "#B5371E", thumbnail: "/theme-previews/zen-1.png",        previews: ["/theme-previews/zen-1.png","/theme-previews/zen-2.png","/theme-previews/zen-3.png"] },
  { name: "warm",       label: "Warm Paper",  description: "Vintage kraft notebook. Cream-tan with paper grain, EB Garamond + Caveat handwritten.",     best_for: ["personal blog talks", "手帐风课件"],     primary_color: "#3D2E1F", accent_color: "#C84B1A", thumbnail: "/theme-previews/warm-1.png",       previews: ["/theme-previews/warm-1.png","/theme-previews/warm-2.png","/theme-previews/warm-3.png"] },
  { name: "noir",       label: "Noir",        description: "High-contrast cinematic. Pure black canvas, Bodoni Moda thin display, single acid cadmium-yellow accent. Designed to pair with B&W AI photography on photo-essay / split-image.", best_for: ["fashion editorial", "film & photography portfolios"], primary_color: "#0A0A0B", accent_color: "#E8FF3C", thumbnail: "/theme-previews/noir-1.svg", previews: ["/theme-previews/noir-1.svg","/theme-previews/noir-2.svg","/theme-previews/noir-3.svg"] },
];

export default function NewSlidesPage() {
  return (
    <Suspense fallback={null}>
      <NewSlidesChat />
    </Suspense>
  );
}

function NewSlidesChat() {
  const router = useRouter();
  const search = useSearchParams();
  const [topic, setTopic] = useState("");
  const [slideCount, setSlideCount] = useState(8);
  const [style, setStyle] = useState("minimalist");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [showOptions, setShowOptions] = useState(false);
  // Currently-open preview modal target. null = closed.
  const [previewTemplate, setPreviewTemplate] = useState<Template | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  // Prefill from ?topic= so the homepage hero hand-off still works.
  useEffect(() => {
    const t = search?.get("topic");
    if (t && !topic) setTopic(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search]);

  useEffect(() => {
    textareaRef.current?.focus();
  }, []);

  async function submit(e?: FormEvent | KeyboardEvent) {
    e?.preventDefault();
    const t = topic.trim();
    if (!t || submitting) return;
    setSubmitting(true);
    setErr(null);
    try {
      // Honour "10 页" / "10 pages" in topic text — extract slide_count
      // hint, strip the fragment from the topic, so the planner sees
      // the cleaner request.
      const { cleanTopic, hints } = parseTopic(t);
      const finalCount =
        hints.slideCount && slideCount === 8 ? hints.slideCount : slideCount;
      const res = await createSlides({
        topic: cleanTopic,
        slide_count: finalCount,
        force_theme: style,
      });
      // Sprint I0.5 — remember the deck so it appears in the
      // "Recent decks" list on the home page. Title gets backfilled
      // from /slides/{id} once the job finishes.
      rememberDeck({
        jobId: res.job_id,
        sessionId: res.session_id,
        topic: cleanTopic.slice(0, 80),
        theme: style,
      });
      router.push(`/slides/${res.job_id}?session=${res.session_id}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Unknown error");
      setSubmitting(false);
    }
  }

  const handlePickFromModal = (name: string) => {
    setStyle(name);
    setPreviewTemplate(null);
    // After picking, jump focus back to the textarea so the user can
    // immediately start typing their topic.
    setTimeout(() => textareaRef.current?.focus(), 50);
  };

  const selectedTemplate = TEMPLATES.find((t) => t.name === style) ?? TEMPLATES[0];

  return (
    <main className="relative min-h-[100dvh] bg-[color:var(--paper)] text-[color:var(--ink)] antialiased">
      <Grain />

      <header className="relative z-10 border-b border-[color:var(--rule)] bg-[color:var(--paper)]/85 backdrop-blur-[2px]">
        <div className="mx-auto flex max-w-[1480px] items-baseline justify-between px-6 py-4 md:px-10">
          <a
            href="/"
            className="group inline-flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
          >
            <ArrowLeft size={11} strokeWidth={1.8} className="translate-y-[1px] transition-transform group-hover:-translate-x-0.5" />
            <span>Dream-Waver / Index</span>
          </a>
          <span className="hidden font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)] md:inline">
            New Conversation
          </span>
        </div>
      </header>

      <div className="relative z-10 mx-auto max-w-6xl px-6 pt-16 md:px-12">
        {/* ── Prompt area ───────────────────────────────────────── */}
        <section className="mb-16 max-w-3xl">
          <p className="mb-3 font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
            Brief · 一句话开始
          </p>
          <h1 className="font-display text-[44px] leading-[1.05] tracking-tight text-[color:var(--ink)] md:text-[56px]">
            想做<span className="text-[color:var(--vermillion)]">什么样</span>的演讲？
          </h1>

          {/* Sprint I0.4 — error banner with explicit 重试 button. Replaces
              the previous tiny red italic that was easy to miss. The retry
              re-uses the same submit() handler so the topic/style/page-count
              don't get lost. */}
          {err && !submitting ? (
            <div
              role="alert"
              className="mt-6 flex flex-wrap items-start gap-3 border-l-2 border-[color:var(--vermillion)] bg-[color:var(--vermillion)]/[0.05] px-4 py-3"
            >
              <div className="flex flex-1 items-start gap-2">
                <span className="mt-[2px] font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--vermillion)]">
                  Err
                </span>
                <p className="font-display text-[14px] italic leading-snug text-[color:var(--ink)]">
                  {err}
                </p>
              </div>
              <button
                type="button"
                onClick={() => submit()}
                disabled={!topic.trim() || submitting}
                className="group inline-flex items-center gap-1.5 border border-[color:var(--ink)] bg-[color:var(--ink)] px-3 py-1.5 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--paper)] transition-all hover:bg-[color:var(--vermillion)] active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <ArrowUpRight size={11} strokeWidth={1.8} className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]" />
                <span>重试</span>
              </button>
            </div>
          ) : null}

          <form onSubmit={submit} className="mt-8 flex flex-col gap-6">
            <div className="border-b border-[color:var(--ink)]/40 pb-3 transition-colors focus-within:border-[color:var(--vermillion)]">
              <textarea
                ref={textareaRef}
                value={topic}
                onChange={(e) => setTopic(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit(e);
                }}
                rows={3}
                disabled={submitting}
                placeholder='例: "做一份介绍 DeepSeek V4 的 10 页投资路演 PPT，目标是 Series A 投资人"'
                className="w-full resize-none bg-transparent font-display text-[22px] leading-snug text-[color:var(--ink)] placeholder:font-display placeholder:text-[20px] placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
              />
            </div>

            <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
              <button
                type="button"
                onClick={() => setShowOptions((v) => !v)}
                className="group inline-flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
              >
                <ChevronDown size={11} strokeWidth={1.6} className={clsx("translate-y-[1px] transition-transform", showOptions && "rotate-180")} />
                <span>Fine-tune</span>
                <span className="text-[color:var(--ink-faint)]">·</span>
                <span className="tabular-nums">{slideCount} pages</span>
                <span className="text-[color:var(--ink-faint)]">·</span>
                <span>{selectedTemplate.label}</span>
              </button>

              <div className="ml-auto flex items-center gap-3">
                {/* Error display moved to the banner above the form (I0.4). */}
                <button
                  type="submit"
                  disabled={!topic.trim() || submitting}
                  className={clsx(
                    "group inline-flex h-11 items-center gap-2 px-5 transition-all duration-200",
                    !topic.trim() || submitting
                      ? "cursor-not-allowed border border-[color:var(--rule)] text-[color:var(--ink-faint)]"
                      : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)] active:translate-y-[1px]",
                  )}
                >
                  {submitting ? (
                    <Loader2 size={13} strokeWidth={1.8} className="animate-spin" />
                  ) : (
                    <ArrowUpRight size={14} strokeWidth={1.8} className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]" />
                  )}
                  <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em]">
                    {submitting ? "Composing" : "Begin"}
                  </span>
                </button>
              </div>
            </div>

            {showOptions ? (
              <PagesPicker slideCount={slideCount} setSlideCount={setSlideCount} />
            ) : null}

            <p className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
              ⌘ / Ctrl + Enter 提交 · agent 会一边规划一边跟你 chat
            </p>
          </form>
        </section>

        {/* ── Template gallery ──────────────────────────────────── */}
        <section className="mb-24">
          <div className="mb-6 flex items-baseline justify-between border-b border-[color:var(--rule)] pb-4">
            <h2 className="font-display text-[28px] tracking-tight text-[color:var(--ink)]">
              模板风格 <span className="font-mono-jb text-[12px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">· Templates</span>
            </h2>
            <p className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              点击预览 · {TEMPLATES.length} 个风格
            </p>
          </div>

          <div className="grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {TEMPLATES.map((t) => (
              <TemplateCard
                key={t.name}
                template={t}
                selected={style === t.name}
                onClick={() => setPreviewTemplate(t)}
              />
            ))}
          </div>
        </section>

        {/* ── H8 image-led layouts gallery ──────────────────────── */}
        <section className="mb-24">
          <div className="mb-6 flex items-baseline justify-between border-b border-[color:var(--rule)] pb-4">
            <h2 className="font-display text-[28px] tracking-tight text-[color:var(--ink)]">
              图像化版式 <span className="font-mono-jb text-[12px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">· Image-led layouts</span>
            </h2>
            <p className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              AI 自动配图 · {LAYOUT_EXAMPLES.length} 种构图
            </p>
          </div>

          <p className="mb-6 max-w-3xl font-display text-[15px] italic leading-snug text-[color:var(--ink-soft)]">
            当 topic 涉及摄影、旅行、时尚、美食、产品等视觉内容时，planner 会自动挑选下面这几种构图，
            nano-banana 直接生成贴合主题的 16:9 配图。
          </p>

          <div className="grid grid-cols-1 gap-5 md:grid-cols-3">
            {LAYOUT_EXAMPLES.map((l) => (
              <article
                key={l.key}
                className="group flex flex-col overflow-hidden border border-[color:var(--rule)] bg-white shadow-[0_1px_0_rgba(26,22,20,0.04)] transition-all duration-200 hover:-translate-y-[2px] hover:border-[color:var(--ink)]/30 hover:shadow-[0_18px_36px_-22px_rgba(26,22,20,0.18)]"
              >
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={l.thumb}
                  alt={`${l.label} layout schematic`}
                  loading="lazy"
                  className="aspect-[16/9] w-full bg-[color:var(--paper)]"
                />
                <div className="flex flex-col gap-2 border-t border-[color:var(--rule)] px-4 py-3">
                  <div className="flex items-baseline justify-between gap-3">
                    <p className="font-display text-[17px] leading-tight text-[color:var(--ink)]">
                      {l.label}
                    </p>
                    <p className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--vermillion)]">
                      {l.when}
                    </p>
                  </div>
                  <p className="font-display text-[13px] italic leading-snug text-[color:var(--ink-soft)]">
                    {l.tagline}
                  </p>
                </div>
              </article>
            ))}
          </div>
        </section>
      </div>

      {previewTemplate ? (
        <TemplatePreviewModal
          template={previewTemplate}
          selected={style === previewTemplate.name}
          onClose={() => setPreviewTemplate(null)}
          onPick={handlePickFromModal}
        />
      ) : null}
    </main>
  );
}

// ─── Template card (grid cell) ───────────────────────────────────

function TemplateCard({
  template,
  selected,
  onClick,
}: {
  template: Template;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={clsx(
        "group relative flex flex-col overflow-hidden border bg-white text-left transition-all duration-200",
        selected
          ? "border-[color:var(--vermillion)] shadow-[0_0_0_3px_rgba(181,55,30,0.15),0_18px_36px_-22px_rgba(181,55,30,0.35)]"
          : "border-[color:var(--rule)] shadow-[0_1px_0_rgba(26,22,20,0.04)] hover:border-[color:var(--ink)]/30 hover:shadow-[0_18px_36px_-22px_rgba(26,22,20,0.18)] hover:-translate-y-[2px]",
      )}
    >
      {/* Selected check badge */}
      {selected ? (
        <span className="absolute right-2 top-2 z-10 inline-flex h-6 w-6 items-center justify-center rounded-full bg-[color:var(--vermillion)] text-[color:var(--paper)] shadow-sm">
          <Check size={13} strokeWidth={2.4} />
        </span>
      ) : null}

      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={template.thumbnail}
        alt={`${template.label} sample slide`}
        loading="lazy"
        className="aspect-[16/9] w-full object-cover"
      />

      <div className="border-t border-[color:var(--rule)] px-3 py-2.5">
        <p className="font-display text-[15px] leading-tight text-[color:var(--ink)]">
          {template.label}
        </p>
        <p className="mt-0.5 truncate font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
          {template.best_for[0]}
        </p>
      </div>
    </button>
  );
}

// ─── Preview modal ───────────────────────────────────────────────

function TemplatePreviewModal({
  template,
  selected,
  onClose,
  onPick,
}: {
  template: Template;
  selected: boolean;
  onClose: () => void;
  onPick: (name: string) => void;
}) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  // Esc closes the modal — keyboard-accessible.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  if (!mounted) return null;

  return createPortal(
    <div
      role="dialog"
      aria-modal
      onClick={onClose}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-6 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="relative flex max-h-[92vh] w-full max-w-5xl flex-col overflow-hidden border border-[color:var(--ink)]/15 bg-[color:var(--paper)] shadow-[0_60px_80px_-30px_rgba(0,0,0,0.5)]"
      >
        {/* Top vermillion bleed strip — same press-mark motif as EditPopover */}
        <div className="h-[3px] w-[64px] bg-[color:var(--vermillion)]" />

        <header className="flex items-start justify-between border-b border-[color:var(--rule)] px-6 py-5 md:px-8">
          <div className="min-w-0 flex-1">
            <p className="mb-1 font-mono-jb text-[10px] uppercase tracking-[0.28em] text-[color:var(--ink-faint)]">
              Template · {template.name}
            </p>
            <h3 className="font-display text-[34px] leading-tight tracking-tight text-[color:var(--ink)]">
              {template.label}
            </h3>
            <p className="mt-2 max-w-2xl font-display text-[16px] italic leading-snug text-[color:var(--ink-soft)]">
              {template.description}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close preview"
            className="ml-4 -mr-2 inline-flex h-9 w-9 shrink-0 items-center justify-center text-[color:var(--ink-faint)] transition-colors hover:text-[color:var(--ink)]"
          >
            <X size={18} strokeWidth={1.8} />
          </button>
        </header>

        {/* Sample slides carousel — horizontal scroll. Each preview
            stays at a real 16:9 aspect so they look like actual decks. */}
        <div className="flex-1 overflow-x-auto overflow-y-hidden bg-[color:var(--paper)]/40 px-6 py-6 md:px-8">
          <div className="flex gap-5">
            {template.previews.map((src, i) => (
              <figure
                key={src}
                className="relative w-[640px] shrink-0 overflow-hidden border border-[color:var(--rule)] bg-white shadow-[0_18px_28px_-20px_rgba(0,0,0,0.25)]"
              >
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={src}
                  alt={`${template.label} slide ${i + 1}`}
                  className="block w-full"
                  style={{ aspectRatio: "16 / 9" }}
                />
                <figcaption className="absolute bottom-2 left-2 rounded-sm bg-black/55 px-2 py-0.5 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-white backdrop-blur-sm">
                  P · {String(i + 1).padStart(2, "0")}
                </figcaption>
              </figure>
            ))}
          </div>
        </div>

        <footer className="flex flex-wrap items-center justify-between gap-3 border-t border-[color:var(--rule)] px-6 py-4 md:px-8">
          <div className="flex flex-wrap items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
            <span className="text-[color:var(--ink-faint)]">Best for:</span>
            {template.best_for.map((tag) => (
              <span
                key={tag}
                className="rounded-sm border border-[color:var(--rule)] px-2 py-0.5 text-[color:var(--ink)]"
              >
                {tag}
              </span>
            ))}
          </div>

          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => onPick(template.name)}
              className={clsx(
                "group inline-flex items-center gap-2 px-4 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] transition-all",
                selected
                  ? "cursor-default border border-[color:var(--vermillion)] text-[color:var(--vermillion)]"
                  : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)] active:translate-y-[1px]",
              )}
            >
              {selected ? (
                <>
                  <Check size={11} strokeWidth={2.4} />
                  <span>已选中</span>
                </>
              ) : (
                <>
                  <span>用这个风格</span>
                  <ArrowUpRight size={11} strokeWidth={1.8} className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]" />
                </>
              )}
            </button>
          </div>
        </footer>
      </div>
    </div>,
    document.body,
  );
}

// ─── Pages picker (page count number only — style picker is the gallery) ───

function PagesPicker({
  slideCount,
  setSlideCount,
}: {
  slideCount: number;
  setSlideCount: (n: number) => void;
}) {
  return (
    <div className="grid gap-6 border-t border-[color:var(--rule)] pt-5">
      <div>
        <p className="mb-2 font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
          Pages
        </p>
        <input
          type="number"
          min={3}
          max={40}
          value={slideCount}
          onChange={(e) => setSlideCount(Math.max(3, Math.min(40, Number(e.target.value) || 8)))}
          className="w-32 border-b border-[color:var(--ink)]/30 bg-transparent pb-1 font-display text-[28px] text-[color:var(--ink)] focus:border-[color:var(--vermillion)] focus:outline-none"
        />
        <p className="mt-2 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
          选模板风格请用下方画廊
        </p>
      </div>
    </div>
  );
}

function Grain() {
  return (
    <div
      aria-hidden
      className="pointer-events-none fixed inset-0 z-0 opacity-[0.05] mix-blend-multiply"
      style={{
        backgroundImage:
          "url(\"data:image/svg+xml;utf8,%3Csvg viewBox='0 0 240 240' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix values='0 0 0 0 0.1 0 0 0 0 0.07 0 0 0 0 0.06 0 0 0 0.8 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
        backgroundSize: "240px 240px",
      }}
    />
  );
}
