"use client";

import { Check, Trash2 } from "lucide-react";
import clsx from "clsx";

import type { Template } from "@/lib/templates";

// TemplateCard — small grid cell shown in the Style Atlas section of
// /slides/new (both the 探索 and 我的模板 tabs reuse it).
//
// Sprint T1: clicking does NOT open a modal (the Sprint Y3 preview
// modal is gone). Instead it sets the currently-selected style state
// directly — pixel re-skin: selected cards get an ink border + violet
// wash + check badge. Submit then passes `force_theme` to the
// orchestrator.
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
    <div className="group relative">
      <button
        type="button"
        onClick={onClick}
        className={clsx(
          "relative flex w-full flex-col overflow-hidden rounded-pixel border-2 text-left transition-all duration-150",
          selected
            ? "border-ink bg-accent-soft shadow-pixel-sm"
            : "border-line-2 bg-surface hover:-translate-x-[1px] hover:-translate-y-[1px] hover:border-ink hover:shadow-pixel",
        )}
      >
        {/* Selected check badge — top-right, pixel square */}
        {selected ? (
          <span className="absolute right-2 top-2 z-10 inline-flex h-6 w-6 items-center justify-center rounded-pixel border-2 border-ink bg-accent text-white shadow-pixel-sm">
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

        <div
          className={clsx(
            "border-t px-3 py-2.5",
            selected ? "border-ink" : "border-line",
          )}
        >
          <p className="font-mono text-[13px] font-bold leading-tight tracking-tight text-ink">
            {template.label}
          </p>
          <p className="mt-0.5 truncate font-mono text-[9px] font-semibold uppercase tracking-wide text-muted">
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
          className="absolute left-2 top-2 z-10 inline-flex h-7 w-7 items-center justify-center rounded-pixel border-2 border-ink bg-surface text-muted opacity-0 shadow-pixel-sm transition-all hover:text-[#a23a2a] group-hover:opacity-100"
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
    <div className="relative grid w-full grid-cols-1 overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel-accent md:grid-cols-[1.4fr_1fr]">
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={template.thumbnail}
        alt={`${template.label} sample slide`}
        loading="lazy"
        draggable={false}
        className="aspect-[16/9] w-full object-cover md:aspect-auto md:h-full"
      />
      <div className="flex flex-col gap-5 border-t-2 border-ink p-6 md:border-l-2 md:border-t-0">
        <div className="flex items-baseline justify-between gap-3">
          <span className="font-pixel text-[0.55rem] tracking-wide text-accent">
            ✓ Current pick
          </span>
          <span className="font-mono text-[10px] font-semibold tracking-wide text-muted">
            Begin 用这个风格
          </span>
        </div>

        <p className="font-mono text-[26px] font-extrabold leading-none tracking-tight text-ink">
          {template.label}
        </p>

        <p className="font-mono text-[13px] leading-relaxed text-ink-2">
          {template.description}
        </p>

        <div className="flex items-center gap-2">
          <span
            className="h-4 w-4 rounded-[3px] border-2 border-ink"
            style={{ backgroundColor: template.primary_color }}
            aria-hidden
          />
          <span
            className="h-4 w-4 rounded-[3px] border-2 border-ink"
            style={{ backgroundColor: template.accent_color }}
            aria-hidden
          />
          <span className="ml-2 font-pixel text-[0.55rem] tracking-wide text-muted">
            Palette
          </span>
        </div>

        <div className="mt-auto flex flex-wrap gap-2">
          {template.best_for.map((b) => (
            <span
              key={b}
              className="rounded-pixel border-[1.5px] border-line-2 bg-surface-2 px-2 py-1 font-mono text-[9px] font-semibold uppercase tracking-wide text-ink-2"
            >
              {b}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}
