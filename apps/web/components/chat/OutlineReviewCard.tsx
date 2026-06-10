"use client";

import { useMemo, useState } from "react";
import { ArrowUpRight, Loader2, Trash2 } from "lucide-react";
import clsx from "clsx";

import type { OutlineForReview, OutlineEditsPayload } from "./session";
import { WindowCard } from "@/components/ui/pixel";

// OutlineReviewCard — Sprint L1.H1 gate, Sprint S selection upgrade.
//
// Shown after plan_outline (+ critic loop) finishes; lets the user
// approve the outline as-is OR make MVP edits before content writing
// starts:
//   - rename any slide's title (per-row inline input)
//   - relayout any slide (per-row layout select — forces the LLM's hand)
//   - delete a single slide (trash icon per row)
//   - pick a deck-wide theme visually (chip thumbnail rail)
//
// Submit posts an `OutlineEditsPayload` to the backend's
// ResumeFromOutlineApproval which merges the diff onto state.Outline
// before running Phase 3.

// ─── Theme palette ─────────────────────────────────────────────────
//
// Mirrors manifest.json. The .ext distinction is because noir's
// preview is shipped as SVG (vector chrome) while the other 10 are
// PNGs from real renders. The path is whitelisted in middleware.ts.

type ThemeOption = {
  value: string;
  label: string;
  ext: "png" | "svg";
  blurb: string;
};

const THEME_OPTIONS: ThemeOption[] = [
  { value: "minimalist", label: "Minimalist", ext: "png", blurb: "Quiet · disciplined" },
  { value: "editorial", label: "Editorial", ext: "png", blurb: "Magazine · serif" },
  { value: "corporate", label: "Corporate", ext: "png", blurb: "Formal · sober" },
  { value: "pitch-deck", label: "Pitch", ext: "png", blurb: "Bold · investor-grade" },
  { value: "academic", label: "Academic", ext: "png", blurb: "Studied · citation-ready" },
  { value: "playful", label: "Playful", ext: "png", blurb: "Warm · colorful" },
  { value: "retro", label: "Retro", ext: "png", blurb: "80s · synthwave" },
  { value: "tech", label: "Tech", ext: "png", blurb: "Terminal · mono" },
  { value: "zen", label: "Zen", ext: "png", blurb: "Washi · sumi" },
  { value: "warm", label: "Warm", ext: "png", blurb: "Kraft · vintage" },
  { value: "noir", label: "Noir", ext: "svg", blurb: "Cinema · high-contrast" },
];

// ─── Layout palette ────────────────────────────────────────────────
//
// Mirrors schema.AllSlideLayouts(). Grouped by purpose so the
// dropdown is scannable — the wire value is still the bare slug.

type LayoutGroup = {
  label: string;
  options: Array<{ value: string; label: string }>;
};

const LAYOUT_GROUPS: LayoutGroup[] = [
  {
    label: "Core",
    options: [
      { value: "title", label: "title — cover" },
      { value: "section", label: "section — divider" },
      { value: "content", label: "content — body prose" },
      { value: "bullets", label: "bullets — list" },
      { value: "closing", label: "closing — wrap" },
    ],
  },
  {
    label: "Editorial",
    options: [
      { value: "quote", label: "quote — single line" },
      { value: "pull-quote", label: "pull-quote — context + quote" },
      { value: "two-column", label: "two-column — body + bullets" },
    ],
  },
  {
    label: "Data",
    options: [
      { value: "data", label: "data — one big metric" },
      { value: "multi-metric", label: "multi-metric — 2-4 KPIs" },
      { value: "comparison", label: "comparison — A vs B" },
      { value: "comparison-table", label: "comparison-table — grid" },
      { value: "swot", label: "swot — 2x2 grid" },
    ],
  },
  {
    label: "Structure",
    options: [
      { value: "timeline", label: "timeline — dated events" },
      { value: "process-flow", label: "process-flow — steps" },
      { value: "toc", label: "toc — table of contents" },
      { value: "checklist", label: "checklist — task items" },
      { value: "icon-grid", label: "icon-grid — features" },
    ],
  },
  {
    label: "Visual",
    options: [
      { value: "photo-essay", label: "photo-essay — full bleed" },
      { value: "split-image", label: "split-image — image + text" },
      { value: "image-grid", label: "image-grid — moodboard" },
      { value: "bento-grid", label: "bento-grid — mixed cards" },
      { value: "before-after", label: "before-after — two images" },
      { value: "team-roster", label: "team-roster — avatars" },
    ],
  },
  {
    label: "Specialty",
    options: [
      { value: "code", label: "code — snippet" },
    ],
  },
];

export function OutlineReviewCard({
  outline,
  onApprove,
  busy,
}: {
  outline: OutlineForReview;
  onApprove: (edits?: OutlineEditsPayload) => void | Promise<void>;
  busy?: boolean;
}) {
  // Local-edit state. The card mutates `titles` / `layouts` / `deleted`
  // / `theme` here, then diffs against the original `outline` prop at
  // submit time to produce the minimal OutlineEditsPayload.
  const [titles, setTitles] = useState<string[]>(() =>
    outline.slides.map((s) => s.headline ?? ""),
  );
  const [layouts, setLayouts] = useState<string[]>(() =>
    outline.slides.map((s) => s.type ?? "content"),
  );
  const [deleted, setDeleted] = useState<Set<number>>(() => new Set());
  const [theme, setTheme] = useState<string>(outline.theme || "minimalist");

  // Compute the diff every render — cheap, deterministic, no need for
  // memo gymnastics. Empty payload returns undefined so the parent
  // can treat "approve as-is" specially.
  const edits: OutlineEditsPayload | undefined = useMemo(() => {
    const renames: Array<{ index: number; title: string }> = [];
    const relayouts: Array<{ index: number; layout: string }> = [];
    outline.slides.forEach((s, i) => {
      const newTitle = titles[i] ?? s.headline ?? "";
      if (newTitle.trim() !== (s.headline ?? "").trim()) {
        renames.push({ index: i, title: newTitle.trim() });
      }
      const newLayout = layouts[i] ?? s.type ?? "";
      if (newLayout && newLayout !== (s.type ?? "")) {
        relayouts.push({ index: i, layout: newLayout });
      }
    });
    const delete_indices = Array.from(deleted);
    const themeChanged = theme && theme !== outline.theme;
    if (
      renames.length === 0 &&
      relayouts.length === 0 &&
      delete_indices.length === 0 &&
      !themeChanged
    ) {
      return undefined; // approve as-is
    }
    return {
      theme: themeChanged ? theme : undefined,
      renames: renames.length > 0 ? renames : undefined,
      relayouts: relayouts.length > 0 ? relayouts : undefined,
      delete_indices: delete_indices.length > 0 ? delete_indices : undefined,
    };
  }, [titles, layouts, deleted, theme, outline]);

  const handleApprove = async () => {
    if (busy) return;
    await onApprove(edits);
  };

  const remaining = outline.slides.length - deleted.size;
  const changeCount =
    (edits?.renames?.length ?? 0) +
    (edits?.relayouts?.length ?? 0) +
    (edits?.delete_indices?.length ?? 0) +
    (edits?.theme ? 1 : 0);

  return (
    <div className="mx-auto my-4 w-full max-w-3xl animate-rise">
      <WindowCard
        title={
          <span className="font-pixel text-[0.55rem] uppercase tracking-wide text-muted">
            <span className="text-accent">§</span> Outline Review{" "}
            <span className="text-line-2">/</span>{" "}
            {remaining} slide{remaining !== 1 ? "s" : ""}
          </span>
        }
        right={
          changeCount > 0 ? (
            <span className="font-pixel text-[0.55rem] uppercase tracking-wide text-accent">
              {changeCount} change{changeCount > 1 ? "s" : ""}
            </span>
          ) : (
            <span className="font-pixel text-[0.55rem] uppercase tracking-wide text-muted">
              No changes
            </span>
          )
        }
        bodyClassName="p-6"
      >
        {/* Deck title (read-only display) */}
        <h3 className="mb-1 font-mono text-[22px] font-extrabold leading-tight tracking-tight text-ink">
          {outline.title}
        </h3>
        {outline.subtitle ? (
          <p className="mb-3 font-mono text-[13px] leading-snug text-ink-2">
            {outline.subtitle}
          </p>
        ) : (
          <div className="mb-3" />
        )}

        {/* Sprint BR.5 — attribution row. Shows which blueprint
            structured the deck and which references (if any) inspired
            its density/voice. Hidden entirely when neither is set, so
            free-form decks read unchanged. */}
        {(outline.blueprint_id || (outline.reference_slugs && outline.reference_slugs.length > 0)) ? (
          <div className="mb-5 flex flex-wrap items-baseline gap-x-3 gap-y-1 font-mono text-[10px] font-semibold tracking-wide text-muted">
            {outline.blueprint_id ? (
              <span>
                框架 <span className="font-normal text-ink-2">· {outline.blueprint_id}</span>
              </span>
            ) : null}
            {outline.reference_slugs && outline.reference_slugs.length > 0 ? (
              <span>
                灵感来自 <span className="font-normal text-ink-2">· {outline.reference_slugs.join(' · ')}</span>
              </span>
            ) : null}
          </div>
        ) : null}

        {/* ─── Theme picker — visual chips ─────────────────────────── */}
        <div className="mb-5 border-y border-line py-3">
          <div className="mb-2 flex items-baseline gap-2">
            <span className="font-pixel text-[0.55rem] uppercase tracking-wide text-muted">
              Theme
            </span>
            <span className="font-mono text-[12px] text-ink-2">
              {THEME_OPTIONS.find((t) => t.value === theme)?.blurb ?? ""}
            </span>
            {theme !== outline.theme ? (
              <span className="ml-auto font-pixel text-[0.5rem] uppercase tracking-wide text-accent">
                changed
              </span>
            ) : null}
          </div>
          <div className="-mx-1 flex gap-2 overflow-x-auto pb-1 pl-1 pr-1">
            {THEME_OPTIONS.map((t) => {
              const isPicked = theme === t.value;
              return (
                <button
                  key={t.value}
                  type="button"
                  disabled={busy}
                  onClick={() => setTheme(t.value)}
                  aria-pressed={isPicked}
                  aria-label={`Pick theme ${t.label}`}
                  className={clsx(
                    "group relative flex w-[112px] shrink-0 flex-col gap-1 transition-all",
                    busy ? "cursor-not-allowed opacity-60" : "cursor-pointer",
                  )}
                >
                  <div
                    className={clsx(
                      "relative aspect-[16/10] w-full overflow-hidden rounded-pixel border-2 bg-surface transition-all",
                      isPicked
                        ? "border-ink shadow-pixel-sm"
                        : "border-line-2 group-hover:border-ink",
                    )}
                  >
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img
                      src={`/theme-previews/${t.value}-1.${t.ext}`}
                      alt={`${t.label} preview`}
                      className="h-full w-full object-cover"
                      loading="lazy"
                      draggable={false}
                    />
                  </div>
                  <span
                    className={clsx(
                      "font-pixel text-[0.5rem] uppercase tracking-wide transition-colors",
                      isPicked
                        ? "text-accent"
                        : "text-muted group-hover:text-ink",
                    )}
                  >
                    {t.label}
                  </span>
                </button>
              );
            })}
          </div>
        </div>

        {/* ─── Slide rows ─────────────────────────────────────────── */}
        <ol className="flex flex-col gap-2">
          {outline.slides.map((s, i) => {
            const isDeleted = deleted.has(i);
            const currentLayout = layouts[i] ?? s.type ?? "content";
            const layoutChanged = currentLayout !== (s.type ?? "");
            return (
              <li
                key={i}
                className={clsx(
                  "grid grid-cols-[28px_minmax(0,1fr)_auto] items-center gap-3 rounded-pixel border px-3 py-2 transition-all",
                  isDeleted
                    ? "border-dashed border-line-2 bg-transparent opacity-50"
                    : "border-line bg-surface-2/60",
                )}
              >
                <span className="font-pixel text-[0.55rem] tracking-wide text-accent">
                  {String(i + 1).padStart(2, "0")}
                </span>

                <div className="flex min-w-0 flex-col gap-1">
                  <input
                    type="text"
                    value={titles[i] ?? ""}
                    onChange={(e) => {
                      if (isDeleted) return;
                      const next = titles.slice();
                      next[i] = e.target.value;
                      setTitles(next);
                    }}
                    disabled={busy || isDeleted}
                    className={clsx(
                      "min-w-0 border-b border-line-2 bg-transparent pb-0.5 font-mono text-[14px] font-semibold leading-snug text-ink focus:border-accent focus:outline-none",
                      isDeleted && "line-through",
                    )}
                  />
                  <div className="flex items-center gap-2">
                    <span className="font-pixel text-[0.5rem] uppercase tracking-wide text-muted">
                      Layout
                    </span>
                    <div className="relative">
                      <select
                        value={currentLayout}
                        onChange={(e) => {
                          if (isDeleted) return;
                          const next = layouts.slice();
                          next[i] = e.target.value;
                          setLayouts(next);
                        }}
                        disabled={busy || isDeleted}
                        className={clsx(
                          "appearance-none border-b border-transparent bg-transparent pb-0.5 pr-4 font-mono text-[10px] uppercase tracking-wide focus:border-accent focus:outline-none disabled:opacity-50",
                          layoutChanged
                            ? "text-accent"
                            : "text-ink-2",
                        )}
                      >
                        {LAYOUT_GROUPS.map((g) => (
                          <optgroup key={g.label} label={g.label}>
                            {g.options.map((o) => (
                              <option key={o.value} value={o.value}>
                                {o.label}
                              </option>
                            ))}
                          </optgroup>
                        ))}
                      </select>
                      {/* chevron tick */}
                      <span
                        aria-hidden
                        className="pointer-events-none absolute right-0 top-1/2 -translate-y-1/2 font-mono text-[10px] text-muted"
                      >
                        ▾
                      </span>
                    </div>
                    {layoutChanged ? (
                      <span className="font-pixel text-[0.5rem] uppercase tracking-wide text-accent">
                        forced
                      </span>
                    ) : null}
                  </div>
                </div>

                <button
                  type="button"
                  onClick={() => {
                    if (busy) return;
                    const next = new Set(deleted);
                    if (isDeleted) next.delete(i);
                    else next.add(i);
                    setDeleted(next);
                  }}
                  aria-label={isDeleted ? "Restore slide" : "Delete slide"}
                  className="inline-flex h-7 w-7 shrink-0 items-center justify-center text-muted transition-colors hover:text-[#d4503a] disabled:opacity-50"
                  disabled={busy}
                >
                  <Trash2 size={13} strokeWidth={1.6} />
                </button>
              </li>
            );
          })}
        </ol>

        {/* Footer */}
        <div className="mt-6 flex items-center justify-between border-t border-line pt-4">
          <span className="font-mono text-[11px] text-muted">
            {busy
              ? "agent 即将撰写内容..."
              : "点继续后 agent 开始写内容 + 渲染"}
          </span>
          <button
            type="button"
            onClick={handleApprove}
            disabled={busy}
            className={clsx(
              "group inline-flex items-center gap-2 rounded-pixel border-2 px-4 py-2 font-mono text-[12px] font-semibold transition-all duration-150",
              busy
                ? "cursor-not-allowed border-line-2 bg-surface-2 text-muted"
                : "border-ink bg-accent text-white shadow-pixel-sm hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none",
            )}
          >
            {busy ? (
              <Loader2 size={12} strokeWidth={1.8} className="animate-spin text-accent" />
            ) : (
              <ArrowUpRight
                size={12}
                strokeWidth={1.8}
                className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]"
              />
            )}
            <span>
              {busy ? "处理中" : edits ? "保存并继续" : "看起来不错，继续"}
            </span>
          </button>
        </div>

        {/* Sprint U3.2 — caption below the footer when busy. The
            content phase takes 1-2 minutes (write_content + critic +
            render); without this caption the user sees the spinner
            on the button but doesn't know how long to wait. */}
        {busy ? (
          <p className="mt-3 font-mono text-[12px] text-ink-2">
            撰写 + 渲染通常 1-2 分钟。下方的进度条会逐张点亮。
          </p>
        ) : null}
      </WindowCard>
    </div>
  );
}
