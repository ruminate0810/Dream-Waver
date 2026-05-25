"use client";

import { useMemo, useState } from "react";
import { ArrowUpRight, Loader2, Trash2 } from "lucide-react";
import clsx from "clsx";

import type { OutlineForReview, OutlineEditsPayload } from "./session";

// OutlineReviewCard — Sprint L1.H1 gate UI.
//
// Shown after plan_outline finishes; lets the user approve the outline
// as-is OR make MVP edits before content writing starts:
//   - rename any slide's title (per-row inline input)
//   - change the deck-wide theme (theme select)
//   - delete a single slide (trash icon per row)
//
// Out of scope for MVP (deferred to post-render edit tools):
//   - drag-reorder slides
//   - add a new slide
//   - change a slide's type
//   - edit speaker_notes / key_points
//
// Submit posts an `OutlineEditsPayload` to the backend's
// ResumeFromOutlineApproval which merges the diff onto state.Outline
// before running Phase 3.

// Themes mirrored from manifest.json. Keep in sync when adding a new
// theme. Used to populate the theme select; the wire value is the
// theme key (lowercase).
const THEME_OPTIONS: Array<{ value: string; label: string }> = [
  { value: "minimalist", label: "Minimalist" },
  { value: "corporate", label: "Corporate" },
  { value: "pitch-deck", label: "Pitch Deck" },
  { value: "academic", label: "Academic" },
  { value: "playful", label: "Playful" },
  { value: "editorial", label: "Editorial" },
  { value: "retro", label: "Retro" },
  { value: "tech", label: "Tech" },
  { value: "zen", label: "Zen" },
  { value: "warm", label: "Warm" },
  { value: "noir", label: "Noir" },
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
  // Local-edit state. The card mutates `slides` (titles + deleted) and
  // `theme` here, then diffs against the original `outline` prop at
  // submit time to produce the minimal OutlineEditsPayload.
  const [titles, setTitles] = useState<string[]>(() =>
    outline.slides.map((s) => s.headline ?? ""),
  );
  const [deleted, setDeleted] = useState<Set<number>>(() => new Set());
  const [theme, setTheme] = useState<string>(outline.theme || "minimalist");

  // Compute the diff every render — cheap, deterministic, no need for
  // a memo gymnastics.
  const edits: OutlineEditsPayload | undefined = useMemo(() => {
    const renames: Array<{ index: number; title: string }> = [];
    outline.slides.forEach((s, i) => {
      const newTitle = titles[i] ?? s.headline ?? "";
      if (newTitle.trim() !== (s.headline ?? "").trim()) {
        renames.push({ index: i, title: newTitle.trim() });
      }
    });
    const delete_indices = Array.from(deleted);
    const themeChanged = theme && theme !== outline.theme;
    if (renames.length === 0 && delete_indices.length === 0 && !themeChanged) {
      return undefined; // approve as-is
    }
    return {
      theme: themeChanged ? theme : undefined,
      renames: renames.length > 0 ? renames : undefined,
      delete_indices: delete_indices.length > 0 ? delete_indices : undefined,
    };
  }, [titles, deleted, theme, outline]);

  const handleApprove = async () => {
    if (busy) return;
    await onApprove(edits);
  };

  const remaining = outline.slides.length - deleted.size;

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
            <span>{remaining} slide{remaining !== 1 ? "s" : ""}</span>
          </div>
          {edits ? (
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--vermillion)]">
              {(edits.renames?.length ?? 0) + (edits.delete_indices?.length ?? 0) + (edits.theme ? 1 : 0)} change{((edits.renames?.length ?? 0) + (edits.delete_indices?.length ?? 0) + (edits.theme ? 1 : 0)) > 1 ? "s" : ""}
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

        {/* Theme picker */}
        <div className="mb-5 flex items-center gap-3 border-y border-[color:var(--ink)]/10 py-3">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
            Theme
          </span>
          <select
            value={theme}
            onChange={(e) => setTheme(e.target.value)}
            disabled={busy}
            className="border-b border-[color:var(--ink)]/40 bg-transparent pb-0.5 font-display text-[15px] text-[color:var(--ink)] focus:border-[color:var(--vermillion)] focus:outline-none disabled:opacity-50"
          >
            {THEME_OPTIONS.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
          {theme !== outline.theme ? (
            <span className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--vermillion)]">
              changed
            </span>
          ) : null}
        </div>

        {/* Slide rows */}
        <ol className="flex flex-col gap-2">
          {outline.slides.map((s, i) => {
            const isDeleted = deleted.has(i);
            return (
              <li
                key={i}
                className={clsx(
                  "flex items-start gap-3 rounded-sm border px-3 py-2 transition-all",
                  isDeleted
                    ? "border-dashed border-[color:var(--ink)]/15 bg-transparent opacity-50"
                    : "border-[color:var(--ink)]/10 bg-white/60",
                )}
              >
                <span className="mt-2 w-7 shrink-0 font-mono-jb text-[10px] uppercase tracking-[0.18em] text-[color:var(--vermillion)]">
                  {String(i + 1).padStart(2, "0")}
                </span>
                <span className="mt-2 w-20 shrink-0 font-mono-jb text-[9px] uppercase tracking-[0.18em] text-[color:var(--ink-faint)]">
                  {s.type}
                </span>
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
                    "min-w-0 flex-1 border-b border-transparent bg-transparent pb-0.5 font-display text-[15px] leading-snug text-[color:var(--ink)] focus:border-[color:var(--vermillion)] focus:outline-none",
                    isDeleted && "line-through",
                  )}
                />
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
                  className="ml-1 inline-flex h-7 w-7 shrink-0 items-center justify-center text-[color:var(--ink-faint)] transition-colors hover:text-[color:var(--vermillion)] disabled:opacity-50"
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
              <ArrowUpRight size={12} strokeWidth={1.8} className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]" />
            )}
            <span>{edits ? "保存并继续" : "看起来不错，继续"}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
