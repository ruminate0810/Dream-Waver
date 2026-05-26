"use client";

import { useMemo, useState } from "react";
import { ArrowUpRight, Loader2, Trash2 } from "lucide-react";
import clsx from "clsx";

import type { OutlineForReview, OutlineEditsPayload } from "./session";

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
    <div className="mx-auto my-4 w-full max-w-3xl">
      {/* Top vermillion bleed strip */}
      <div className="h-[3px] w-[42px] bg-[color:var(--vermillion)]" />

      <div
        className="relative border border-[color:var(--ink)]/15 bg-[#FBF9F2] p-6"
        style={{
          boxShadow:
            "0 2px 0 rgba(26,22,20,0.04), 0 18px 32px -20px rgba(50,40,32,0.32)",
        }}
      >
        {/* Header */}
        <div className="mb-1 flex items-baseline justify-between gap-3">
          <div className="flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
            <span className="text-[color:var(--vermillion)]">§</span>
            <span>Outline Review</span>
            <span className="text-[color:var(--ink-faint)]">/</span>
            <span>
              {remaining} slide{remaining !== 1 ? "s" : ""}
            </span>
          </div>
          {changeCount > 0 ? (
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--vermillion)]">
              {changeCount} change{changeCount > 1 ? "s" : ""}
            </span>
          ) : (
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              No changes
            </span>
          )}
        </div>

        {/* Deck title (read-only display) */}
        <h3 className="mb-1 font-display text-[24px] leading-tight tracking-tight text-[color:var(--ink)]">
          {outline.title}
        </h3>
        {outline.subtitle ? (
          <p className="mb-5 font-display text-[14px] italic leading-snug text-[color:var(--ink-soft)]">
            {outline.subtitle}
          </p>
        ) : (
          <div className="mb-5" />
        )}

        {/* ─── Theme picker — visual chips ─────────────────────────── */}
        <div className="mb-5 border-y border-[color:var(--ink)]/10 py-3">
          <div className="mb-2 flex items-baseline gap-2">
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              Theme
            </span>
            <span className="font-display text-[13px] italic text-[color:var(--ink-soft)]">
              {THEME_OPTIONS.find((t) => t.value === theme)?.blurb ?? ""}
            </span>
            {theme !== outline.theme ? (
              <span className="ml-auto font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--vermillion)]">
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
                      "relative aspect-[16/10] w-full overflow-hidden border bg-white transition-all",
                      isPicked
                        ? "border-[color:var(--vermillion)] shadow-[0_0_0_2px_var(--vermillion)]"
                        : "border-[color:var(--ink)]/15 group-hover:border-[color:var(--ink)]/40",
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
                      "font-mono-jb text-[9px] uppercase tracking-[0.18em] transition-colors",
                      isPicked
                        ? "text-[color:var(--vermillion)]"
                        : "text-[color:var(--ink-soft)] group-hover:text-[color:var(--ink)]",
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
                  "grid grid-cols-[28px_minmax(0,1fr)_auto] items-center gap-3 rounded-sm border px-3 py-2 transition-all",
                  isDeleted
                    ? "border-dashed border-[color:var(--ink)]/15 bg-transparent opacity-50"
                    : "border-[color:var(--ink)]/10 bg-white/60",
                )}
              >
                <span className="font-mono-jb text-[10px] uppercase tracking-[0.18em] text-[color:var(--vermillion)]">
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
                      "min-w-0 border-b border-transparent bg-transparent pb-0.5 font-display text-[15px] leading-snug text-[color:var(--ink)] focus:border-[color:var(--vermillion)] focus:outline-none",
                      isDeleted && "line-through",
                    )}
                  />
                  <div className="flex items-center gap-2">
                    <span className="font-mono-jb text-[9px] uppercase tracking-[0.18em] text-[color:var(--ink-faint)]">
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
                          "appearance-none border-b border-transparent bg-transparent pb-0.5 pr-4 font-mono-jb text-[10px] uppercase tracking-[0.18em] focus:border-[color:var(--vermillion)] focus:outline-none disabled:opacity-50",
                          layoutChanged
                            ? "text-[color:var(--vermillion)]"
                            : "text-[color:var(--ink-soft)]",
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
                        className="pointer-events-none absolute right-0 top-1/2 -translate-y-1/2 font-mono-jb text-[10px] text-[color:var(--ink-faint)]"
                      >
                        ▾
                      </span>
                    </div>
                    {layoutChanged ? (
                      <span className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--vermillion)]">
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
                  className="inline-flex h-7 w-7 shrink-0 items-center justify-center text-[color:var(--ink-faint)] transition-colors hover:text-[color:var(--vermillion)] disabled:opacity-50"
                  disabled={busy}
                >
                  <Trash2 size={13} strokeWidth={1.6} />
                </button>
              </li>
            );
          })}
        </ol>

        {/* Footer */}
        <div className="mt-6 flex items-center justify-between border-t border-[color:var(--ink)]/10 pt-4">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
            点继续后 agent 开始写内容 + 渲染
          </span>
          <button
            type="button"
            onClick={handleApprove}
            disabled={busy}
            className={clsx(
              "group inline-flex items-center gap-2 px-4 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] transition-all duration-200",
              busy
                ? "cursor-not-allowed text-[color:var(--ink-faint)]"
                : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)] active:translate-y-[1px]",
            )}
          >
            {busy ? (
              <Loader2 size={12} strokeWidth={1.8} className="animate-spin" />
            ) : (
              <ArrowUpRight
                size={12}
                strokeWidth={1.8}
                className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]"
              />
            )}
            <span>{edits ? "保存并继续" : "看起来不错，继续"}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
