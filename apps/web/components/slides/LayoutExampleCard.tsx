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
    <article className="dw-new-layout-card group flex flex-col overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm transition-all duration-150 hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-pixel">
      <div className="relative">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={example.thumb}
          alt={`${example.label} layout schematic`}
          loading="lazy"
          draggable={false}
          className="aspect-[16/9] w-full bg-paper"
        />
        {/* Folio numeral — top-left, gives the grid a magazine cadence
            rather than a card wall. */}
        <span className="pointer-events-none absolute left-3 top-3 font-pixel text-[0.55rem] tracking-wide text-muted mix-blend-multiply">
          Fig. {String(index + 1).padStart(2, "0")}
        </span>
      </div>
      <div className="flex flex-col gap-2 border-t border-line px-5 py-4">
        <div className="flex items-baseline justify-between gap-3">
          <p className="font-mono text-[18px] font-bold leading-tight tracking-tight text-ink">
            {example.label}
          </p>
          <p className="font-mono text-[10px] font-semibold tracking-wide text-accent">
            {example.when}
          </p>
        </div>
        <p className="font-mono text-[14px] leading-relaxed text-muted">
          {example.tagline}
        </p>
      </div>
    </article>
  );
}
