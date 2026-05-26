"use client";

import {
  Suspense,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { gsap } from "gsap";
import { useGSAP } from "@gsap/react";
import { ArrowLeft, ArrowUpRight, Loader2 } from "lucide-react";
import clsx from "clsx";

import { createSlides } from "@/lib/api";
import { parseTopic } from "@/lib/parseHints";
import { rememberDeck } from "@/lib/recentDecks";
import { DUR, EASE, PREFERS_FULL_MOTION } from "@/lib/motion";

gsap.registerPlugin(useGSAP);

// /slides/new — Sprint R: gutted from 1052 LOC to ~250.
//
// Pre-R the page front-loaded everything: theme gallery (§ 02), layout
// schematic gallery (§ 03), 3-step pipeline strip — about a thousand
// lines of marketing the user had to scroll past before hitting Begin.
//
// Now that Sprint Q makes the wizard agent-driven (the LLM asks
// whatever it needs to know per topic) and Sprint O's critic loop
// picks layouts dynamically, all of that pre-flight chrome was
// duplicating decisions the agent will make better.
//
// This page is now:  hero text → one textarea → Begin → starter chips.
// Everything else (theme, layout, audience refinement) happens inside
// the agent's chat-driven execution. User can change_theme / regenerate /
// apply_brand mid-conversation if they don't like the agent's picks.
//
// Page count remains a tiny opt-in fine-tune since the planner needs
// SOME hint (defaults to 8 if not specified).

// Starter prompts — six worked examples that double as a topology
// hint (pitch / technical / retro / lesson / launch / photo-essay).
// Click → fills the textarea so the user can tweak before sending.
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

  // Lightweight entrance — header settles, then brief, then form, then
  // starters. No scroll-triggered gallery reveals (no gallery any more).
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
      });
      mm.add("(prefers-reduced-motion: reduce)", () => {
        gsap.from(
          ".dw-new-header, .dw-new-caption, .dw-new-title, .dw-new-dek, .dw-new-form, .dw-new-helper, .dw-new-starters",
          { opacity: 0, duration: 0.2, stagger: 0.04 },
        );
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
      // Honour "10 页" / "10 pages" in topic text — extract slide_count
      // hint, strip the fragment, so the planner sees the cleaner request.
      const { cleanTopic, hints } = parseTopic(t);
      const finalCount =
        hints.slideCount && slideCount === 8 ? hints.slideCount : slideCount;
      const res = await createSlides({
        topic: cleanTopic,
        slide_count: finalCount,
        // Sprint R — no force_theme; the planner picks theme from the
        // topic + audience signals. User can change_theme mid-chat.
      });
      // Sprint I0.5 — remember the deck so it appears in the
      // "Recent decks" list on the home page. Title gets backfilled
      // from /slides/{id} once the job finishes.
      rememberDeck({
        jobId: res.job_id,
        sessionId: res.session_id,
        topic: cleanTopic.slice(0, 80),
        theme: "auto", // agent picks
      });
      router.push(`/slides/${res.job_id}?session=${res.session_id}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Unknown error");
      setSubmitting(false);
    }
  }

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

      {/* Single-column, centred — the page IS the brief. No marketing,
          no gallery, no pipeline diagram. Type and go. */}
      <div className="relative z-10 mx-auto flex min-h-[calc(100dvh-57px)] max-w-3xl flex-col justify-center px-6 py-16 md:px-12">
        <section className="max-w-4xl">
          <div className="dw-new-caption mb-10 flex items-baseline gap-4">
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--vermillion)]">
              §
            </span>
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              Brief · 一句话开始
            </span>
            <span className="ml-2 h-px flex-1 bg-[color:var(--rule)]" />
          </div>

          <h1 className="dw-new-title font-display text-[48px] leading-[0.98] tracking-tight text-[color:var(--ink)] md:text-[72px]">
            想做<span className="italic text-[color:var(--vermillion)]">什么样</span>
            <br className="hidden md:inline" />
            的演讲？
          </h1>

          <p className="dw-new-dek mt-6 max-w-[640px] font-display text-[16px] italic leading-relaxed text-[color:var(--ink-soft)] md:text-[18px]">
            写一句话给 agent ——
            <span className="not-italic text-[color:var(--ink)]"> 它会自己判断要问你什么 </span>
            （受众、风格、关键点都由 agent 决定），然后规划、写、画图、渲染。中途任何一页都能改。
          </p>

          {/* Sprint I0.4 — error banner with explicit 重试 button. */}
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

          <form onSubmit={submit} className="dw-new-form mt-10 flex flex-col gap-5">
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
                className="w-full resize-none bg-transparent font-display text-[20px] leading-snug text-[color:var(--ink)] placeholder:font-display placeholder:text-[18px] placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
              />
            </div>

            <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
              <button
                type="button"
                onClick={() => setShowOptions((v) => !v)}
                className="group inline-flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
              >
                <span className="tabular-nums">{slideCount} pages</span>
                <span className="text-[color:var(--ink-faint)]">·</span>
                <span>fine-tune</span>
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

          {/* Starter prompts — small chips below the form, only thing
              between the brief and the bottom of the viewport. */}
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

// Page-level paper grain — fixed, pointer-events-none so it never
// re-paints during scroll.
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

// Suspense wrapper required by Next.js for useSearchParams in client
// components. Renders nothing during SSR; the page hydrates on the
// client and the GSAP timeline reveals the hero.
export default function Page() {
  return (
    <Suspense>
      <NewSlidesChat />
    </Suspense>
  );
}
