"use client";

import { Check, Trash2 } from "lucide-react";
import clsx from "clsx";

import type { Template } from "@/lib/templates";

// TemplateCard — small grid cell shown in the Style Atlas section of
// /slides/new (both the 探索 and 我的模板 tabs reuse it).
//
// Sprint T1: clicking does NOT open a modal (the Sprint Y3 preview
// modal is gone). Instead it sets the currently-selected style state
// directly — vermillion ring + check badge reflect the selection.
// Submit then passes `force_theme` to the orchestrator.
//
// Sprint T4: when used inside the 我的模板 tab, a hover-revealed trash
// icon is shown so users can delete their saved templates. Pass
// `onDelete` to enable it; pass nothing for read-only / explore mode.

export function TemplateCard({
  template,
  selected,
  onClick,
  onDelete,
}: {
  template: Template;
  selected: boolean;
  onClick: () => void;
  /** When supplied, renders a hover-only trash button (top-left). */
  onDelete?: () => void;
}) {
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
        {/* Selected check badge — top-right, vermillion disk */}
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
          draggable={false}
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

      {/* Sprint T4 — delete trigger for "我的模板" tab. Hover-only so
          it doesn't clutter the explore view. Stops propagation so the
          card's onClick doesn't fire. */}
      {onDelete ? (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            e.preventDefault();
            onDelete();
          }}
          aria-label={`Delete template ${template.label}`}
          className="absolute left-2 top-2 z-10 inline-flex h-7 w-7 items-center justify-center rounded-full border border-[color:var(--ink)]/15 bg-white/90 text-[color:var(--ink-faint)] opacity-0 shadow-sm transition-all hover:border-[color:var(--vermillion)] hover:text-[color:var(--vermillion)] group-hover:opacity-100"
        >
          <Trash2 size={13} strokeWidth={1.8} />
        </button>
      ) : null}
    </div>
  );
}

// FeaturedTemplateCard — the currently-selected style gets a hero
// card spanning 2 cols on lg+. Both communicates "this is what you're
// about to use" AND gives the gallery visual hierarchy.
//
// Clicking is a no-op since the card already represents the selected
// state — Sprint T1 changed this from "open preview modal" to
// pure-display. If the user wants to change picks, they click a
// sibling TemplateCard.
export function FeaturedTemplateCard({ template }: { template: Template }) {
  return (
    <div className="dw-new-template-card relative grid w-full grid-cols-1 overflow-hidden border-2 border-[color:var(--vermillion)] bg-white shadow-[0_0_0_4px_rgba(181,55,30,0.10),0_24px_48px_-28px_rgba(181,55,30,0.40)] md:grid-cols-[1.4fr_1fr]">
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={template.thumbnail}
        alt={`${template.label} sample slide`}
        loading="lazy"
        draggable={false}
        className="aspect-[16/9] w-full object-cover md:aspect-auto md:h-full"
      />
      <div className="flex flex-col gap-5 border-t border-[color:var(--rule)] p-6 md:border-l md:border-t-0">
        <div className="flex items-baseline justify-between">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--vermillion)]">
            ✓ Current pick
          </span>
          <span className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
            Begin 用这个风格
          </span>
        </div>

        <p className="font-display text-[32px] leading-none tracking-tight text-[color:var(--ink)]">
          {template.label}
        </p>

        <p className="font-display text-[15px] italic leading-relaxed text-[color:var(--ink-soft)]">
          {template.description}
        </p>

        <div className="flex items-center gap-2">
          <span
            className="h-4 w-4 rounded-sm border border-[color:var(--ink)]/10"
            style={{ backgroundColor: template.primary_color }}
            aria-hidden
          />
          <span
            className="h-4 w-4 rounded-sm border border-[color:var(--ink)]/10"
            style={{ backgroundColor: template.accent_color }}
            aria-hidden
          />
          <span className="ml-2 font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
            Palette
          </span>
        </div>

        <div className="mt-auto flex flex-wrap gap-2">
          {template.best_for.map((b) => (
            <span
              key={b}
              className="border border-[color:var(--rule)] px-2 py-1 font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-soft)]"
            >
              {b}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}
