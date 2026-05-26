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
import { ArrowLeft, ArrowUpRight, ChevronDown, Loader2 } from "lucide-react";
import clsx from "clsx";

import { createSlides } from "@/lib/api";
import { parseTopic } from "@/lib/parseHints";
import { rememberDeck } from "@/lib/recentDecks";
import { DUR, EASE, PREFERS_FULL_MOTION, STAGGER } from "@/lib/motion";
import {
  LAYOUT_EXAMPLES,
  TEMPLATES,
  findTemplate,
  type Template,
} from "@/lib/templates";
import {
  FeaturedTemplateCard,
  TemplateCard,
} from "@/components/slides/TemplateCard";
import { LayoutExampleCard } from "@/components/slides/LayoutExampleCard";

gsap.registerPlugin(useGSAP);

// /slides/new — Sprint T1: galleries restored on top of Sprint R's
// chat-first hero.
//
// Layout:
//   § 01 Brief        — textarea + Begin (Sprint R kept)
//   § 02 Style Atlas  — 11-theme picker (featured card + grid) [Sprint T1]
//   § 03 Composition  — 9 image-led / IA layout examples (read-only) [T1]
//
// Pipeline strip (§ between Brief and Style) is NOT restored — agent
// flow is already self-explanatory via wizard + outline review gate.
//
// Picking a template sets `style` state. Submit then passes
// `force_theme: style` to createSlides so the planner honours the
// user's pick. Default is "minimalist" so first-load Begin has a
// known theme without any clicking required.
//
// Sprint T4 will add tab bar (探索 / 我的模板) on § 02 — placeholder
// state goes in here so Sprint T1 visually matches the eventual UI.

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
      const res = await createSlides({
        topic: cleanTopic,
        slide_count: finalCount,
        // Sprint T1 — force_theme is back. User picks via Style Atlas
        // gallery (default "minimalist"). Agent honours it verbatim;
        // the user can still change_theme mid-chat.
        force_theme: style,
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

        {/* ── § 02 — Style Atlas (theme picker) ───────────────────── */}
        <section className="mb-24">
          <div className="dw-new-template-head mb-8 flex items-baseline gap-4">
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--vermillion)]">
              § 02
            </span>
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              Style Atlas · 选风格
            </span>
            <span className="ml-2 h-px flex-1 bg-[color:var(--rule)]" />
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
              {TEMPLATES.length} 个
            </span>
          </div>

          <p className="mb-8 max-w-2xl font-display text-[16px] italic leading-relaxed text-[color:var(--ink-soft)]">
            点任一卡片切换风格。当前选中
            <span className="not-italic text-[color:var(--ink)]"> {selectedTemplate.label} </span>
            —— Begin 时 agent 按这套色板和字体写所有幻灯片。
          </p>

          <FeaturedTemplateCard template={selectedTemplate} />

          <div className="dw-new-template-grid mt-6 grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {galleryTemplates.map((t) => (
              <TemplateCard
                key={t.name}
                template={t}
                selected={false}
                onClick={() => setStyle(t.name)}
              />
            ))}
          </div>
        </section>

        {/* ── § 03 — Composition (image-led / IA layouts, read-only) */}
        <section className="mb-32">
          <div className="dw-new-layout-head mb-8 flex items-baseline gap-4">
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--vermillion)]">
              § 03
            </span>
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              Composition · 图像化版式
            </span>
            <span className="ml-2 h-px flex-1 bg-[color:var(--rule)]" />
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
              AI 配图 · {LAYOUT_EXAMPLES.length} 种
            </span>
          </div>

          <p className="mb-10 max-w-2xl font-display text-[17px] italic leading-relaxed text-[color:var(--ink-soft)] md:text-[19px]">
            主题涉及摄影、旅行、时尚、美食、产品等视觉内容时，planner
            自动挑下面这几种构图，
            <span className="not-italic text-[color:var(--ink)]"> nano-banana </span>
            直接生成贴合主题的 16:9 配图。你不用挑 —— 选好风格就放手。
          </p>

          <div className="dw-new-layout-grid grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
            {LAYOUT_EXAMPLES.map((l, i) => (
              <LayoutExampleCard key={l.key} example={l} index={i} />
            ))}
          </div>
        </section>
      </div>
    </main>
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
