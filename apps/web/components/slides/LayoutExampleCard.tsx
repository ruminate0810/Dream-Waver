"use client";

import type { LayoutExample } from "@/lib/templates";

// LayoutExampleCard — read-only display card for the Composition
// gallery on /slides/new. Unlike TemplateCard, this is NOT a picker —
// the planner picks layout per-slide based on the topic and the agent
// loop's per-audience palette (Sprint P1.b). This gallery is pure
// marketing: "see what the agent CAN do with your topic".
//
// SVG thumbnail (Sprint H10) + label + when-to-use mono caption +
// tagline. No selected state, no click handler — pointer cursor is
// default since hovering doesn't promise interaction.

export function LayoutExampleCard({
  example,
  index,
}: {
  example: LayoutExample;
  /** 0-based index — surfaced as Fig. 01 / Fig. 02 folio numeral. */
  index: number;
}) {
  return (
    <article className="dw-new-layout-card group flex flex-col overflow-hidden border border-[color:var(--rule)] bg-white shadow-[0_1px_0_rgba(26,22,20,0.04)] transition-all duration-200 hover:-translate-y-[2px] hover:border-[color:var(--ink)]/30 hover:shadow-[0_18px_36px_-22px_rgba(26,22,20,0.18)]">
      <div className="relative">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={example.thumb}
          alt={`${example.label} layout schematic`}
          loading="lazy"
          draggable={false}
          className="aspect-[16/9] w-full bg-[color:var(--paper)]"
        />
        {/* Folio numeral — top-left, gives the grid a magazine cadence
            rather than a card wall. */}
        <span className="pointer-events-none absolute left-3 top-3 font-mono-jb text-[10px] uppercase tracking-[0.32em] text-[color:var(--ink-faint)] mix-blend-multiply">
          Fig. {String(index + 1).padStart(2, "0")}
        </span>
      </div>
      <div className="flex flex-col gap-2 border-t border-[color:var(--rule)] px-5 py-4">
        <div className="flex items-baseline justify-between gap-3">
          <p className="font-display text-[20px] leading-tight text-[color:var(--ink)]">
            {example.label}
          </p>
          <p className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--vermillion)]">
            {example.when}
          </p>
        </div>
        <p className="font-display text-[14px] italic leading-relaxed text-[color:var(--ink-soft)]">
          {example.tagline}
        </p>
      </div>
    </article>
  );
}
