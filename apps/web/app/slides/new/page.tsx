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
import { ArrowLeft, ArrowUpRight, ChevronDown, FileText, Loader2, Paperclip, Plus, Trash2, X } from "lucide-react";
import clsx from "clsx";

import {
  createSlides,
  deleteUserTemplate,
  extractDocument,
  listUserTemplates,
  type UserTemplate,
} from "@/lib/api";
import { parseTopic } from "@/lib/parseHints";
import { rememberDeck } from "@/lib/recentDecks";
import { useSlideMode } from "@/lib/slideMode";
import { ModeToggle } from "@/components/slides/ModeToggle";
import {
  TEMPLATES,
  findTemplate,
  type Template,
} from "@/lib/templates";
import { ErrorCallout } from "@/components/ui/ErrorCallout";
import {
  FeaturedTemplateCard,
  TemplateCard,
} from "@/components/slides/TemplateCard";
import { TemplateCreator } from "@/components/slides/TemplateCreator";
import { PixelButton, WindowCard } from "@/components/ui/pixel";

// /slides/new — Sprint AC layout (tab bar restored), pixel re-skin.
//
// Layout:
//   § 01 Brief         — textarea + Begin (kept)
//   § 02 Style Atlas   — [ 探索 | 我的模板 ] tab bar over a single grid.
//                        探索 = 11 official themes; 我的模板 = saved
//                        user templates + 「+ 添加我的模板」 card.
//
// Why tabs (vs Sprint AB's two stacked sections): when 我的模板 is empty
// (almost always for a new user) the section reads as dead space. A tab
// bar shares the same patch of viewport — exploration UX is the same,
// but empty-state is one tab click away instead of a half-page of
// "还没有保存的模板…" prose.
//
// Picking a theme sets `style` state; submit passes
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
  const [mode, setMode] = useSlideMode();
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [showOptions, setShowOptions] = useState(false);
  // Sprint AC — Style Atlas tab bar. "explore" lists the 11 official
  // themes; "mine" lists user-saved templates. Default "explore" so
  // first-load users see real choices, not an empty state.
  const [styleTab, setStyleTab] = useState<"explore" | "mine">("explore");
  const [myTemplates, setMyTemplates] = useState<UserTemplate[]>([]);
  const [templatesLoading, setTemplatesLoading] = useState(false);
  const [templatesError, setTemplatesError] = useState<string | null>(null);
  // When non-null, the user picked a saved template — submit will pass
  // the brand alongside force_theme.
  const [selectedMyTemplate, setSelectedMyTemplate] = useState<UserTemplate | null>(null);
  // Creator modal open / closed.
  const [showCreator, setShowCreator] = useState(false);
  // PMQ C1 — an uploaded PDF / Markdown / txt, extracted to text and passed
  // as reference_text so the outline is grounded in the real document.
  const [docText, setDocText] = useState("");
  const [docMeta, setDocMeta] = useState<{
    filename: string;
    chars: number;
    truncated: boolean;
  } | null>(null);
  const [docBusy, setDocBusy] = useState(false);
  const [docErr, setDocErr] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
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

  // Entrance — CSS-driven (`animate-rise`, transform-only) so nothing can
  // strand at opacity:0 under React Strict Mode or a backgrounded tab.
  // The old GSAP timeline + ScrollTrigger choreography is gone with the
  // pixel re-skin.

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
        mode,
        brand,
        // PMQ C1 — ground the outline in the uploaded document when present.
        reference_text: docText || undefined,
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

  // PMQ C1 — read an uploaded document into reference_text.
  async function handleFilePick(file: File | undefined | null) {
    if (!file) return;
    setDocErr(null);
    setDocBusy(true);
    try {
      const doc = await extractDocument(file);
      setDocText(doc.text);
      setDocMeta({
        filename: doc.filename,
        chars: doc.chars,
        truncated: doc.truncated,
      });
    } catch (e) {
      setDocText("");
      setDocMeta(null);
      setDocErr(e instanceof Error ? e.message : "文档解析失败");
    } finally {
      setDocBusy(false);
    }
  }
  function clearDoc() {
    setDocText("");
    setDocMeta(null);
    setDocErr(null);
    if (fileInputRef.current) fileInputRef.current.value = "";
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
    // Sprint AC — 我的模板 is a tab again; the modal can be opened
    // from either tab (TemplateCreator floats over the page), so make
    // sure we land on the 我的模板 tab after create so the user
    // actually sees the new card slot in at the top of the grid.
    setMyTemplates((prev) => [created, ...prev]);
    setShowCreator(false);
    setStyleTab("mine");
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
    <main className="dot-grid relative min-h-[100dvh] bg-paper text-ink antialiased">
      <header className="relative z-10 border-b-2 border-ink bg-paper/85 backdrop-blur-[2px]">
        <div className="mx-auto flex max-w-[1480px] items-center justify-between px-6 py-3.5 md:px-10">
          <a
            href="/"
            className="group inline-flex items-center gap-2 font-mono text-[11px] font-semibold uppercase tracking-wide text-ink-2 transition-colors hover:text-ink"
          >
            <span className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm transition-transform group-hover:-translate-x-0.5">
              <ArrowLeft size={11} strokeWidth={2} />
            </span>
            <span>Dream-Waver / Index</span>
          </a>
          <span className="hidden font-pixel text-[0.55rem] uppercase tracking-wide text-muted md:inline">
            New Conversation
          </span>
        </div>
      </header>

      <div className="relative z-10 mx-auto max-w-6xl px-6 pt-20 md:px-12 md:pt-24">
        {/* ── § 01 — Brief / hero ───────────────────────────────────
            Pixel hero: chapter mark + bold mono title + the form in a
            terminal WindowCard. Gallery sections sit below. */}
        <section className="animate-rise mb-24 max-w-4xl">
          <div className="mb-10 flex items-baseline gap-4">
            <span className="font-pixel text-[0.6rem] tracking-wide text-accent">
              § 01
            </span>
            <span className="font-mono text-[10px] font-semibold uppercase tracking-wide text-muted">
              Brief · 一句话开始
            </span>
            <span className="ml-2 h-px flex-1 bg-line-2" />
          </div>

          <h1 className="font-mono text-[42px] font-extrabold leading-[1.04] tracking-tight text-ink md:text-[64px]">
            想做
            <span className="relative whitespace-nowrap text-accent">
              什么样
              <span className="absolute -bottom-1 left-0 right-0 -z-10 h-3 bg-accent-soft" aria-hidden />
            </span>
            <br className="hidden md:inline" />
            的演讲？
          </h1>

          <p className="mt-8 max-w-[640px] font-mono text-[15px] leading-relaxed text-ink-2 md:text-[16px]">
            写一句话给 agent —— 它会先
            <span className="font-semibold text-ink"> 规划大纲 </span>
            让你审阅，再
            <span className="font-semibold text-ink"> 写完每张幻灯片 </span>
            ，最后
            <span className="font-semibold text-ink"> 渲染成 .pptx </span>
            。下面挑一个风格或者直接 Begin。
          </p>

          {err && !submitting ? (
            <div className="mt-6">
              <ErrorCallout
                message={err}
                action={
                  topic.trim()
                    ? { label: "重试", onClick: () => submit() }
                    : undefined
                }
              />
            </div>
          ) : null}

          <form onSubmit={submit} className="mt-12">
            {/* Terminal composer — same WindowCard language as the home
                hero (~/new-deck). Textarea on top, controls in a chrome
                bar under a hard hairline. */}
            <WindowCard
              title="~/slides/new — 描述你想做什么"
              className="shadow-pixel-lg"
              bodyClassName="!p-0"
            >
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
                className="w-full resize-none bg-transparent px-5 pb-3 pt-4 font-mono text-[16px] leading-relaxed text-ink placeholder:text-muted focus:outline-none disabled:opacity-50 md:text-[17px]"
              />

              {/* PMQ C1 — attached document chip + parse error + hidden picker */}
              {docMeta ? (
                <div className="mx-5 mb-3 flex items-center gap-3 rounded-pixel border-2 border-line-2 bg-surface-2 px-3 py-2">
                  <FileText
                    size={14}
                    strokeWidth={1.8}
                    className="shrink-0 text-accent"
                  />
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-mono text-[12px] font-semibold text-ink">
                      {docMeta.filename}
                    </p>
                    <p className="font-mono text-[10px] text-muted">
                      已抽取 {docMeta.chars.toLocaleString()} 字 · 作为参考资料
                      {docMeta.truncated ? " · 已截断" : ""}
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={clearDoc}
                    aria-label="移除文档"
                    className="shrink-0 text-muted transition-colors hover:text-ink"
                  >
                    <X size={14} strokeWidth={1.8} />
                  </button>
                </div>
              ) : null}
              {docErr ? (
                <p className="mx-5 mb-3 rounded-pixel border-2 border-[#d4503a]/60 bg-[#fdece9] px-3 py-2 font-mono text-[11px] leading-relaxed text-[#a23a2a]">
                  {docErr}
                </p>
              ) : null}
              <input
                ref={fileInputRef}
                type="file"
                accept=".pdf,.md,.markdown,.txt,.text,.csv,application/pdf,text/markdown,text/plain"
                className="hidden"
                onChange={(e) => handleFilePick(e.target.files?.[0])}
              />

              <div className="flex flex-wrap items-center gap-x-4 gap-y-3 border-t-2 border-line px-4 py-3">
                <button
                  type="button"
                  onClick={() => setShowOptions((v) => !v)}
                  className="group inline-flex items-center gap-1.5 rounded-pixel px-1.5 py-1 font-mono text-[11px] font-semibold tracking-wide text-ink-2 transition-colors hover:text-ink"
                >
                  <ChevronDown
                    size={12}
                    strokeWidth={2}
                    className={clsx(
                      "transition-transform",
                      showOptions && "rotate-180",
                    )}
                  />
                  <span>Fine-tune</span>
                  <span className="text-muted">·</span>
                  <span className="tabular-nums">{slideCount} pages</span>
                  <span className="text-muted">·</span>
                  <span>{selectedTemplate.label}</span>
                </button>

                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={submitting || docBusy}
                  title="上传 PDF / Markdown / txt，agent 会基于它生成"
                  className="group inline-flex items-center gap-1.5 rounded-pixel px-1.5 py-1 font-mono text-[11px] font-semibold tracking-wide text-ink-2 transition-colors hover:text-ink disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {docBusy ? (
                    <Loader2 size={12} strokeWidth={2} className="animate-spin text-accent" />
                  ) : (
                    <Paperclip size={12} strokeWidth={2} />
                  )}
                  <span>{docBusy ? "Reading" : docMeta ? "换文档" : "附文档"}</span>
                </button>

                <ModeToggle value={mode} onChange={setMode} />

                <PixelButton
                  type="submit"
                  variant={topic.trim() && !submitting ? "accent" : "default"}
                  disabled={!topic.trim() || submitting}
                  className="ml-auto"
                >
                  {submitting ? (
                    <Loader2 size={14} strokeWidth={2.2} className="animate-spin" />
                  ) : (
                    <ArrowUpRight size={14} strokeWidth={2.2} />
                  )}
                  <span>{submitting ? "Composing" : "Begin"}</span>
                </PixelButton>
              </div>

              {showOptions ? (
                <div className="border-t-2 border-line bg-surface-2 px-4 py-3">
                  <PagesPicker slideCount={slideCount} setSlideCount={setSlideCount} />
                </div>
              ) : null}
            </WindowCard>

            <p className="mt-4 font-mono text-[11px] leading-relaxed text-muted">
              ⌘ / Ctrl + Enter 提交 · 可「附文档」让 agent 基于 PDF / Markdown 生成 · 会按需问你几个问题再开工
            </p>
          </form>

          <div className="mt-10">
            <p className="mb-3 font-mono text-[10px] font-semibold tracking-wide text-muted">
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
                  className="rounded-pixel border-2 border-line-2 bg-surface px-3 py-1.5 text-left font-mono text-[12px] text-ink-2 transition-all duration-150 hover:-translate-x-[1px] hover:-translate-y-[1px] hover:border-ink hover:text-ink hover:shadow-pixel-sm"
                >
                  {s}
                </button>
              ))}
            </div>
          </div>
        </section>

        {/* ── § 02 — Style Atlas (tab bar: 探索 | 我的模板) ─────────
            Sprint AC — one section, one viewport patch, two grids
            switched by tab. Default "explore" so first-load users
            see real choices instead of an empty "我的模板" state. */}
        <section className="animate-rise mb-32" style={{ animationDelay: "0.1s" }}>
          <div className="mb-6 flex flex-wrap items-baseline gap-4">
            <span className="font-pixel text-[0.6rem] tracking-wide text-accent">
              § 02
            </span>
            <span className="font-mono text-[10px] font-semibold uppercase tracking-wide text-muted">
              Style Atlas · 主题与品牌
            </span>
            <span className="mx-2 hidden h-px flex-1 bg-line-2 md:block" />
            {/* Tab bar — pixel chip tabs; active gets ink border + violet wash */}
            <div className="ml-auto flex items-center gap-2">
              <StyleTabButton
                active={styleTab === "explore"}
                onClick={() => setStyleTab("explore")}
                label="探索"
                badge={`${TEMPLATES.length}`}
              />
              <StyleTabButton
                active={styleTab === "mine"}
                onClick={() => setStyleTab("mine")}
                label="我的模板"
                badge={
                  templatesLoading
                    ? "…"
                    : myTemplates.length > 0
                    ? `${myTemplates.length}`
                    : "0"
                }
              />
            </div>
          </div>

          {styleTab === "explore" ? (
            <>
              <p className="mb-8 max-w-2xl font-mono text-[13px] leading-relaxed text-ink-2">
                挑一个主题，Begin 时 agent 按这套色板和字体写所有幻灯片。当前选中
                <span className="font-semibold text-ink"> {selectedTemplate.label}</span>
                。不挑也行 —— AI 会按 brief 自动选。
              </p>

              <FeaturedTemplateCard template={selectedTemplate} />

              <div className="mt-6 grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
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
          ) : (
            <MyTemplatesGrid
              templates={myTemplates}
              loading={templatesLoading}
              error={templatesError}
              selectedID={selectedMyTemplate?.id ?? null}
              onPick={applyUserTemplate}
              onDelete={handleDeleteTemplate}
              onCreate={() => setShowCreator(true)}
            />
          )}
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

// StyleTabButton — single tab in the § 02 Style Atlas tab bar.
// Pixel chip tabs: the active tab gets an ink border + violet wash +
// hard pixel shadow (same selected-state language as option cards);
// idle tabs are quiet bordered chips that sharpen on hover.
function StyleTabButton({
  active,
  onClick,
  label,
  badge,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  badge: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={clsx(
        "inline-flex items-baseline gap-2 rounded-pixel border-2 px-3 py-1.5 font-mono text-[11px] font-semibold tracking-wide transition-all duration-150",
        active
          ? "border-ink bg-accent-soft text-ink shadow-pixel-sm"
          : "border-line-2 bg-surface text-muted hover:border-ink hover:text-ink",
      )}
    >
      <span>{label}</span>
      <span
        className={clsx(
          "tabular-nums text-[9px]",
          active ? "text-accent" : "text-muted",
        )}
      >
        {badge}
      </span>
    </button>
  );
}

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
      <p className="mb-8 max-w-2xl font-mono text-[13px] leading-relaxed text-ink-2">
        把常用的
        <span className="font-semibold text-ink"> 主题 + 品牌色 + 字体 </span>
        组合存下来，下次一键复用 —— 不用每次重新挑色板。
      </p>

      {error ? (
        <div
          role="alert"
          className="mb-4 rounded-pixel border-2 border-[#d4503a]/60 bg-[#fdece9] px-3 py-2 font-mono text-[12px] leading-relaxed text-[#a23a2a]"
        >
          {error}
        </div>
      ) : null}

      <div className="dw-new-template-grid grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
        {/* + Add-my-template card — always at position 0 */}
        <button
          type="button"
          onClick={onCreate}
          className="dw-new-template-card group flex aspect-[16/12] w-full flex-col items-center justify-center gap-2 rounded-pixel border-2 border-dashed border-line-2 bg-surface-2 px-4 py-3 text-center transition-all hover:border-ink hover:bg-surface hover:shadow-pixel-sm"
        >
          <Plus
            size={24}
            strokeWidth={1.6}
            className="text-muted transition-colors group-hover:text-accent"
          />
          <span className="font-mono text-[13px] font-semibold leading-snug text-ink-2 transition-colors group-hover:text-ink">
            添加我的模板
          </span>
          <span className="font-pixel text-[0.5rem] tracking-wide text-muted">
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
        <p className="mt-6 font-mono text-[12px] text-muted">
          还没有保存的模板。点上方
          <span className="font-semibold text-ink"> 添加我的模板 </span>
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
          "relative flex w-full flex-col overflow-hidden rounded-pixel border-2 bg-surface text-left transition-all duration-150",
          selected
            ? "border-ink bg-accent-soft shadow-pixel-sm"
            : "border-ink shadow-pixel-sm hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-pixel",
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
            <div className="aspect-[16/9] w-full bg-paper-2" />
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

        <div className="border-t border-line px-3 py-2.5">
          <p className="truncate font-mono text-[14px] font-bold leading-tight text-ink">
            {template.name}
          </p>
          <p className="mt-0.5 truncate font-pixel text-[0.5rem] tracking-wide text-muted">
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
        className="absolute left-2 top-2 z-10 inline-flex h-7 w-7 items-center justify-center rounded-pixel border-2 border-ink bg-surface text-muted opacity-0 shadow-pixel-sm transition-all hover:text-[#a23a2a] group-hover:opacity-100"
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
    <div className="flex items-center gap-3 border-l-2 border-line pl-4">
      <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
        Pages
      </span>
      <input
        type="number"
        min={3}
        max={40}
        value={slideCount}
        onChange={(e) => setSlideCount(Math.max(3, Math.min(40, Number(e.target.value) || 8)))}
        className="w-20 rounded-pixel border-2 border-ink bg-surface px-2 py-1 font-mono text-[18px] font-bold tabular-nums text-ink focus:shadow-pixel-sm focus:outline-none"
      />
      <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
        3 – 40
      </span>
    </div>
  );
}

export default function Page() {
  return (
    <Suspense>
      <NewSlidesChat />
    </Suspense>
  );
}
