"use client";

import {
  Suspense,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { gsap } from "gsap";
import { useGSAP } from "@gsap/react";
import { ArrowLeft, ArrowUpRight, ChevronDown, Loader2, Plus, Trash2 } from "lucide-react";
import clsx from "clsx";

import {
  createSlides,
  deleteUserTemplate,
  listUserTemplates,
  type UserTemplate,
} from "@/lib/api";
import { parseTopic } from "@/lib/parseHints";
import { rememberDeck } from "@/lib/recentDecks";
import { DUR, EASE, PREFERS_FULL_MOTION, STAGGER } from "@/lib/motion";
import {
  TEMPLATES,
  findTemplate,
  type Template,
} from "@/lib/templates";
import {
  FeaturedTemplateCard,
  TemplateCard,
} from "@/components/slides/TemplateCard";
import { TemplateCreator } from "@/components/slides/TemplateCreator";

gsap.registerPlugin(useGSAP);

// /slides/new — Sprint AB layout (Manus-style 2-section).
//
// Layout:
//   § 01 Brief         — textarea + Begin (kept)
//   § 02 我的模板       — saved templates grid (hoisted from former T4 tab)
//   § 03 主题风格        — collapsible disclosure ("AI 帮你挑，想手动调点这里")
//                          contains the 11-theme picker. Default closed.
//
// Removed in Sprint AB:
//   - § 03 Composition (9 layout schematics) — was read-only education;
//     layout is agent-decided, so the gallery was inert.
//   - 探索 / 我的模板 tab bar — both got their own sections instead.
//
// Picking a theme still sets `style` state; submit passes
// `force_theme: style` to createSlides. Default "minimalist" so
// first-load Begin has a known theme without any clicking required.

// Starter prompts — six worked examples that double as a topology
// hint (pitch / technical / retro / lesson / launch / photo-essay).
const STARTER_PROMPTS = [
  "做一份 10 页的 Series A 投资路演 PPT，介绍 DeepSeek V4，目标听众是 USV / a16z",
  "8 页技术分享：transformer attention 是什么 + 为什么 KV cache 改变游戏规则",
  "团队季度回顾 6 页：本季交付了什么、踩了什么坑、下季要做什么",
  "iPhone 17 风格的新品发布会 12 页，深色底 + 大数据卡",
  "5 节课程大纲：从零开始学 SwiftUI，每节 30 分钟，针对前端转 iOS 的开发者",
  "8 页摄影集风格 deck：京都樱花季的一周，photo-essay + split-image 配图",
] as const;

function NewSlidesChat() {
  const router = useRouter();
  const search = useSearchParams();
  const [topic, setTopic] = useState("");
  const [slideCount, setSlideCount] = useState(8);
  const [style, setStyle] = useState("minimalist");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [showOptions, setShowOptions] = useState(false);
  // Sprint AB — Style Atlas is now a collapsible disclosure (default
  // closed). The user can flip it open to manually pick a theme; AI
  // picks one for them if they leave it closed.
  const [themeExpanded, setThemeExpanded] = useState(false);
  const [myTemplates, setMyTemplates] = useState<UserTemplate[]>([]);
  const [templatesLoading, setTemplatesLoading] = useState(false);
  const [templatesError, setTemplatesError] = useState<string | null>(null);
  // When non-null, the user picked a saved template — submit will pass
  // the brand alongside force_theme.
  const [selectedMyTemplate, setSelectedMyTemplate] = useState<UserTemplate | null>(null);
  // Creator modal open / closed.
  const [showCreator, setShowCreator] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const mainRef = useRef<HTMLElement | null>(null);

  // Prefill from ?topic= so the homepage hero hand-off still works.
  useEffect(() => {
    const t = search?.get("topic");
    if (t && !topic) setTopic(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search]);

  useEffect(() => {
    textareaRef.current?.focus();
  }, []);

  // Sprint T4 — fetch saved templates on mount. We always fetch (cheap,
  // workspace-scoped via X-Dev-User-Id header) so the tab badge reflects
  // the actual count without the user having to click "我的模板" first.
  useEffect(() => {
    let cancelled = false;
    setTemplatesLoading(true);
    listUserTemplates()
      .then((rows) => {
        if (!cancelled) {
          setMyTemplates(rows);
          setTemplatesError(null);
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setTemplatesError(e instanceof Error ? e.message : "load failed");
        }
      })
      .finally(() => {
        if (!cancelled) setTemplatesLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Entrance choreography. Header settles first, then the brief settles,
  // then the form, then the helper. Style Atlas + Composition galleries
  // reveal via ScrollTrigger when they approach the viewport — first-fold
  // visitors see the brief settle, scrolling reveals the galleries.
  useGSAP(
    () => {
      const mm = gsap.matchMedia();

      mm.add(PREFERS_FULL_MOTION, () => {
        const tl = gsap.timeline({
          defaults: { ease: EASE.entrance, duration: DUR.entrance },
        });
        tl.from(".dw-new-header", { y: -8, opacity: 0, duration: DUR.secondary })
          .from(".dw-new-caption", { y: 8, opacity: 0, duration: DUR.micro }, "-=0.3")
          .from(".dw-new-title", { y: 28, opacity: 0 }, "-=0.2")
          .from(".dw-new-dek", { y: 14, opacity: 0, duration: DUR.secondary }, "-=0.4")
          .from(".dw-new-form", { y: 18, opacity: 0, duration: DUR.secondary }, "-=0.35")
          .from(".dw-new-helper", { y: 8, opacity: 0, duration: DUR.micro }, "-=0.25")
          .from(".dw-new-starters", { y: 12, opacity: 0, duration: DUR.secondary }, "-=0.15");

        // Style Atlas — section header settles first, cards stagger up.
        gsap.from(".dw-new-template-head", {
          y: 12,
          opacity: 0,
          duration: DUR.reveal,
          ease: EASE.entrance,
          scrollTrigger: {
            trigger: ".dw-new-template-head",
            start: "top 90%",
            once: true,
          },
        });
        gsap.from(".dw-new-template-card", {
          y: 16,
          opacity: 0,
          stagger: STAGGER.card,
          duration: DUR.reveal,
          ease: EASE.entrance,
          scrollTrigger: {
            trigger: ".dw-new-template-grid",
            start: "top 88%",
            once: true,
          },
        });

        // Composition — separate trigger so the two galleries don't share
        // a fate; user can scroll past one and the other still animates.
        gsap.from(".dw-new-layout-head", {
          y: 12,
          opacity: 0,
          duration: DUR.reveal,
          ease: EASE.entrance,
          scrollTrigger: {
            trigger: ".dw-new-layout-head",
            start: "top 90%",
            once: true,
          },
        });
        gsap.from(".dw-new-layout-card", {
          y: 16,
          opacity: 0,
          stagger: STAGGER.card,
          duration: DUR.reveal,
          ease: EASE.entrance,
          scrollTrigger: {
            trigger: ".dw-new-layout-grid",
            start: "top 88%",
            once: true,
          },
        });
      });

      mm.add("(prefers-reduced-motion: reduce)", () => {
        gsap.from(
          ".dw-new-header, .dw-new-caption, .dw-new-title, .dw-new-dek, .dw-new-form, .dw-new-helper, .dw-new-starters",
          { opacity: 0, duration: 0.2, stagger: 0.04 },
        );
        gsap.from(".dw-new-template-card, .dw-new-layout-card", {
          opacity: 0,
          duration: 0.2,
          stagger: 0.02,
          scrollTrigger: {
            trigger: ".dw-new-template-grid",
            start: "top 95%",
            once: true,
          },
        });
      });
    },
    { scope: mainRef },
  );

  async function submit(e?: FormEvent | KeyboardEvent) {
    e?.preventDefault();
    const t = topic.trim();
    if (!t || submitting) return;
    setSubmitting(true);
    setErr(null);
    try {
      const { cleanTopic, hints } = parseTopic(t);
      const finalCount =
        hints.slideCount && slideCount === 8 ? hints.slideCount : slideCount;
      // Sprint T4 — when the user picked a saved "我的模板", attach
      // its brand so the backend applies it after content generation.
      // Empty-brand payload is dropped server-side so a Style-Atlas-
      // only pick (no brand) still works.
      const brand =
        selectedMyTemplate &&
        (selectedMyTemplate.brand_primary ||
          selectedMyTemplate.brand_accent ||
          selectedMyTemplate.font_family)
          ? {
              primary_color: selectedMyTemplate.brand_primary,
              accent_color: selectedMyTemplate.brand_accent,
              font_family: selectedMyTemplate.font_family,
            }
          : undefined;
      const res = await createSlides({
        topic: cleanTopic,
        slide_count: finalCount,
        force_theme: style,
        brand,
      });
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

  // Sprint T4 — helpers for "我的模板" pick + delete + create.
  function applyUserTemplate(t: UserTemplate) {
    setStyle(t.theme);
    setSelectedMyTemplate(t);
  }
  async function handleDeleteTemplate(t: UserTemplate) {
    const ok = window.confirm(`删除模板「${t.name}」？此操作不可撤销。`);
    if (!ok) return;
    try {
      await deleteUserTemplate(t.id);
      setMyTemplates((prev) => prev.filter((x) => x.id !== t.id));
      if (selectedMyTemplate?.id === t.id) setSelectedMyTemplate(null);
    } catch (e) {
      setTemplatesError(e instanceof Error ? e.message : "delete failed");
    }
  }
  function handleTemplateCreated(created: UserTemplate) {
    // Sprint AB — 我的模板 is now its own section (no tab to switch
    // to). The new template appears in-place at the top of the grid;
    // applying it sets the brand + theme for submit.
    setMyTemplates((prev) => [created, ...prev]);
    setShowCreator(false);
    applyUserTemplate(created);
  }

  const selectedTemplate: Template =
    findTemplate(style) ?? TEMPLATES[0];

  // Hide the featured card from the grid (it lives separately above) so
  // the user doesn't see it twice.
  const galleryTemplates = useMemo(
    () => TEMPLATES.filter((t) => t.name !== selectedTemplate.name),
    [selectedTemplate],
  );

  return (
    <main
      ref={mainRef}
      className="relative min-h-[100dvh] bg-[color:var(--paper)] text-[color:var(--ink)] antialiased"
    >
      <Grain />

      <header className="dw-new-header relative z-10 border-b border-[color:var(--rule)] bg-[color:var(--paper)]/85 backdrop-blur-[2px]">
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

      <div className="relative z-10 mx-auto max-w-6xl px-6 pt-20 md:px-12 md:pt-24">
        {/* ── § 01 — Brief / hero ───────────────────────────────────
            Editorial hero: chapter mark + oversized title + italic
            dek. Sprint T1 keeps Sprint R's minimal hero — gallery
            sections sit below, scroll-triggered. */}
        <section className="mb-24 max-w-4xl">
          <div className="dw-new-caption mb-10 flex items-baseline gap-4">
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--vermillion)]">
              § 01
            </span>
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              Brief · 一句话开始
            </span>
            <span className="ml-2 h-px flex-1 bg-[color:var(--rule)]" />
          </div>

          <h1 className="dw-new-title font-display text-[56px] leading-[0.98] tracking-tight text-[color:var(--ink)] md:text-[88px]">
            想做<span className="italic text-[color:var(--vermillion)]">什么样</span>
            <br className="hidden md:inline" />
            的演讲？
          </h1>

          <p className="dw-new-dek mt-8 max-w-[640px] font-display text-[17px] italic leading-relaxed text-[color:var(--ink-soft)] md:text-[19px]">
            写一句话给 agent —— 它会先
            <span className="not-italic text-[color:var(--ink)]"> 规划大纲 </span>
            让你审阅，再
            <span className="not-italic text-[color:var(--ink)]"> 写完每张幻灯片 </span>
            ，最后
            <span className="not-italic text-[color:var(--ink)]"> 渲染成 .pptx </span>
            。下面挑一个风格或者直接 Begin。
          </p>

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

          <form onSubmit={submit} className="dw-new-form mt-12 flex flex-col gap-6">
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
                <ChevronDown
                  size={11}
                  strokeWidth={1.6}
                  className={clsx(
                    "translate-y-[1px] transition-transform",
                    showOptions && "rotate-180",
                  )}
                />
                <span>Fine-tune</span>
                <span className="text-[color:var(--ink-faint)]">·</span>
                <span className="tabular-nums">{slideCount} pages</span>
                <span className="text-[color:var(--ink-faint)]">·</span>
                <span>{selectedTemplate.label}</span>
              </button>

              <button
                type="submit"
                disabled={!topic.trim() || submitting}
                className={clsx(
                  "ml-auto group inline-flex h-11 items-center gap-2 px-5 transition-all duration-200",
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

            {showOptions ? (
              <PagesPicker slideCount={slideCount} setSlideCount={setSlideCount} />
            ) : null}

            <p className="dw-new-helper font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
              ⌘ / Ctrl + Enter 提交 · agent 会按需要问你几个问题再开工
            </p>
          </form>

          <div className="dw-new-starters mt-10">
            <p className="mb-3 font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
              没灵感？从这些开始
            </p>
            <div className="flex flex-wrap gap-2">
              {STARTER_PROMPTS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => {
                    setTopic(s);
                    textareaRef.current?.focus();
                  }}
                  className="border border-[color:var(--rule)] bg-white/40 px-3 py-1.5 text-left font-display text-[13px] italic text-[color:var(--ink-soft)] transition-all hover:-translate-y-[1px] hover:border-[color:var(--ink)]/30 hover:bg-white hover:text-[color:var(--ink)]"
                >
                  {s}
                </button>
              ))}
            </div>
          </div>
        </section>

        {/* ── § 02 — 我的模板 (saved user templates) ────────────────
            Sprint AB — hoisted out of the former Style Atlas tab bar
            into its own top-level section, mirroring Manus's "我的技能"
            block. First slot is always "+ 新建模板" (dashed card). */}
        <section className="mb-20">
          <div className="dw-new-template-head mb-6 flex items-baseline gap-4">
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--vermillion)]">
              § 02
            </span>
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              我的模板
            </span>
            <span className="ml-2 h-px flex-1 bg-[color:var(--rule)]" />
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
              {templatesLoading
                ? "loading…"
                : myTemplates.length > 0
                ? `${myTemplates.length} saved`
                : "0 saved"}
            </span>
          </div>

          <p className="mb-8 max-w-2xl font-display text-[16px] italic leading-relaxed text-[color:var(--ink-soft)]">
            保存你常用的「主题 + 品牌色 + 字体」组合，下次生成 deck 一键
            套用。点一个 → Begin 时按这套设定写所有幻灯片。
          </p>

          <MyTemplatesGrid
            templates={myTemplates}
            loading={templatesLoading}
            error={templatesError}
            selectedID={selectedMyTemplate?.id ?? null}
            onPick={applyUserTemplate}
            onDelete={handleDeleteTemplate}
            onCreate={() => setShowCreator(true)}
          />
        </section>

        {/* ── § 03 — 主题风格 (Style Atlas, collapsed by default) ──
            Sprint AB — was the always-visible § 02 in Sprint T1.
            Now hidden behind a disclosure so the main flow is
            "type → begin"; advanced users who want manual theme
            control can expand. AI's default theme pick is honoured
            when this stays closed (force_theme=minimalist fallback). */}
        <section className="mb-32">
          <div className="dw-new-template-head mb-6 flex items-baseline gap-4">
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--vermillion)]">
              § 03
            </span>
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              主题风格 · 高级
            </span>
            <span className="ml-2 h-px flex-1 bg-[color:var(--rule)]" />
            <button
              type="button"
              onClick={() => setThemeExpanded((v) => !v)}
              className="group inline-flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
            >
              <ChevronDown
                size={11}
                strokeWidth={1.6}
                className={clsx(
                  "translate-y-[1px] transition-transform",
                  themeExpanded && "rotate-180",
                )}
              />
              <span>{themeExpanded ? "收起" : "展开 · 手动挑"}</span>
            </button>
          </div>

          <p className="mb-6 max-w-2xl font-display text-[16px] italic leading-relaxed text-[color:var(--ink-soft)]">
            {themeExpanded ? (
              <>
                当前选中
                <span className="not-italic text-[color:var(--ink)]"> {selectedTemplate.label} </span>
                —— Begin 时 agent 按这套色板和字体写所有幻灯片。
              </>
            ) : (
              <>
                AI 会根据你的 brief 自动挑一个合适的主题。想自己挑？点
                <button
                  type="button"
                  onClick={() => setThemeExpanded(true)}
                  className="not-italic underline decoration-[color:var(--vermillion)]/40 underline-offset-2 transition-colors hover:text-[color:var(--vermillion)]"
                >
                  右上角展开
                </button>
                {" "}看全部 {TEMPLATES.length} 个。
              </>
            )}
          </p>

          {themeExpanded ? (
            <>
              <FeaturedTemplateCard template={selectedTemplate} />

              <div className="dw-new-template-grid mt-6 grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
                {galleryTemplates.map((t) => (
                  <TemplateCard
                    key={t.name}
                    template={t}
                    selected={false}
                    onClick={() => {
                      setStyle(t.name);
                      setSelectedMyTemplate(null);
                    }}
                  />
                ))}
              </div>
            </>
          ) : null}
        </section>
      </div>

      {/* Sprint T4 — TemplateCreator modal. Mounted at the page root
          (portal anyway) so the gallery sections' overflow-hidden
          don't clip it. Default theme prefills from the currently-
          picked Style Atlas card. */}
      {showCreator ? (
        <TemplateCreator
          defaultTheme={style}
          onClose={() => setShowCreator(false)}
          onCreated={handleTemplateCreated}
        />
      ) : null}
    </main>
  );
}

// (StyleTabButton removed in Sprint AB — 我的模板 is its own section
// now; no tabs needed.)

function MyTemplatesGrid({
  templates,
  loading,
  error,
  selectedID,
  onPick,
  onDelete,
  onCreate,
}: {
  templates: UserTemplate[];
  loading: boolean;
  error: string | null;
  selectedID: string | null;
  onPick: (t: UserTemplate) => void;
  onDelete: (t: UserTemplate) => void;
  onCreate: () => void;
}) {
  return (
    <>
      <p className="mb-8 max-w-2xl font-display text-[16px] italic leading-relaxed text-[color:var(--ink-soft)]">
        保存你常用的
        <span className="not-italic text-[color:var(--ink)]"> 主题 + 品牌色 + 字体 </span>
        组合，下次新建演讲一键复用。
      </p>

      {error ? (
        <div
          role="alert"
          className="mb-4 border-l-2 border-[color:var(--vermillion)] bg-[color:var(--vermillion)]/[0.05] px-3 py-2 font-display text-[14px] italic text-[color:var(--ink)]"
        >
          {error}
        </div>
      ) : null}

      <div className="dw-new-template-grid grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
        {/* + Add-my-template card — always at position 0 */}
        <button
          type="button"
          onClick={onCreate}
          className="dw-new-template-card group flex aspect-[16/12] w-full flex-col items-center justify-center gap-2 border border-dashed border-[color:var(--ink)]/30 bg-white/40 px-4 py-3 text-center transition-all hover:-translate-y-[2px] hover:border-[color:var(--vermillion)] hover:bg-white"
        >
          <Plus
            size={24}
            strokeWidth={1.4}
            className="text-[color:var(--ink-faint)] transition-colors group-hover:text-[color:var(--vermillion)]"
          />
          <span className="font-display text-[14px] italic leading-snug text-[color:var(--ink-soft)] transition-colors group-hover:text-[color:var(--ink)]">
            添加我的模板
          </span>
          <span className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
            theme + brand
          </span>
        </button>

        {loading && templates.length === 0
          ? null
          : templates.map((t) => (
              <UserTemplateCard
                key={t.id}
                template={t}
                selected={t.id === selectedID}
                onClick={() => onPick(t)}
                onDelete={() => onDelete(t)}
              />
            ))}
      </div>

      {!loading && templates.length === 0 ? (
        <p className="mt-6 font-display text-[14px] italic text-[color:var(--ink-faint)]">
          还没有保存的模板。点上方
          <span className="not-italic text-[color:var(--ink)]"> 添加我的模板 </span>
          开始 —— 几秒钟搞定。
        </p>
      ) : null}
    </>
  );
}

function UserTemplateCard({
  template,
  selected,
  onClick,
  onDelete,
}: {
  template: UserTemplate;
  selected: boolean;
  onClick: () => void;
  onDelete: () => void;
}) {
  // Find the base theme so we can pull its thumbnail + label.
  const base = findTemplate(template.theme);
  const primary = template.brand_primary || base?.primary_color || "#1A1614";
  const accent = template.brand_accent || base?.accent_color || "#B5371E";
  return (
    <div className="dw-new-template-card group relative">
      <button
        type="button"
        onClick={onClick}
        className={clsx(
          "relative flex w-full flex-col overflow-hidden border bg-white text-left transition-all duration-200",
          selected
            ? "border-[color:var(--vermillion)] shadow-[0_0_0_3px_rgba(181,55,30,0.15),0_18px_36px_-22px_rgba(181,55,30,0.35)]"
            : "border-[color:var(--rule)] shadow-[0_1px_0_rgba(26,22,20,0.04)] hover:-translate-y-[2px] hover:border-[color:var(--ink)]/30 hover:shadow-[0_18px_36px_-22px_rgba(26,22,20,0.18)]",
        )}
      >
        {/* Thumbnail — use base theme preview with the brand colours
            painted as corner ribbons to communicate "branded variant". */}
        <div className="relative">
          {base ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src={base.thumbnail}
              alt={`${template.name} — ${base.label} base`}
              loading="lazy"
              draggable={false}
              className="aspect-[16/9] w-full object-cover"
            />
          ) : (
            <div className="aspect-[16/9] w-full bg-[color:var(--paper)]" />
          )}
          {/* Brand stripe — primary + accent slashes top-right corner */}
          <div className="pointer-events-none absolute right-0 top-0 flex h-8 items-stretch overflow-hidden">
            <span
              className="block w-6 origin-top-right -skew-x-12"
              style={{ backgroundColor: primary }}
              aria-hidden
            />
            <span
              className="block w-3 origin-top-right -skew-x-12"
              style={{ backgroundColor: accent }}
              aria-hidden
            />
          </div>
        </div>

        <div className="border-t border-[color:var(--rule)] px-3 py-2.5">
          <p className="truncate font-display text-[15px] leading-tight text-[color:var(--ink)]">
            {template.name}
          </p>
          <p className="mt-0.5 truncate font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
            {base?.label ?? template.theme} · brand
          </p>
        </div>
      </button>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
          onDelete();
        }}
        aria-label={`Delete template ${template.name}`}
        className="absolute left-2 top-2 z-10 inline-flex h-7 w-7 items-center justify-center rounded-full border border-[color:var(--ink)]/15 bg-white/90 text-[color:var(--ink-faint)] opacity-0 shadow-sm transition-all hover:border-[color:var(--vermillion)] hover:text-[color:var(--vermillion)] group-hover:opacity-100"
      >
        <Trash2 size={13} strokeWidth={1.8} />
      </button>
    </div>
  );
}

// ─── Helpers ─────────────────────────────────────────────────────────

function PagesPicker({
  slideCount,
  setSlideCount,
}: {
  slideCount: number;
  setSlideCount: (n: number) => void;
}) {
  return (
    <div className="flex items-baseline gap-3 border-l-2 border-[color:var(--rule)] pl-4">
      <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
        Pages
      </span>
      <input
        type="number"
        min={3}
        max={40}
        value={slideCount}
        onChange={(e) => setSlideCount(Math.max(3, Math.min(40, Number(e.target.value) || 8)))}
        className="w-20 border-b border-[color:var(--ink)]/30 bg-transparent pb-1 font-display text-[22px] text-[color:var(--ink)] focus:border-[color:var(--vermillion)] focus:outline-none"
      />
      <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
        3 – 40
      </span>
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
          "url(\"data:image/svg+xml;utf8,%3Csvg viewBox='0 0 240 240' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix values='0 0 0 0 0.1 0 0 0 0 0.07 0 0 0 0 0.06 0 0 0 0 0.06 0 0 0 0.8 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
        backgroundSize: "240px 240px",
      }}
    />
  );
}

export default function Page() {
  return (
    <Suspense>
      <NewSlidesChat />
    </Suspense>
  );
}
