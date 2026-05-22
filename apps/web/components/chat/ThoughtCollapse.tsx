"use client";

import { useState } from "react";
import clsx from "clsx";
import { ChevronDown, Quote } from "lucide-react";

export type Thought = {
  step: number;
  text: string;
  inputTokens?: number;
  outputTokens?: number;
  cacheRead?: number;
};

// ThoughtCollapse renders the agent's internal reasoning (LLM text content
// emitted before / between tool calls). Default-folded — most users only
// want the tool log; the curious can click to read the marginalia. Keeps
// the editorial language: italic serif, hanging quote glyph, mono caps
// for the token-cost footer.

export function ThoughtCollapse({ thoughts }: { thoughts: Thought[] }) {
  const [open, setOpen] = useState(false);
  // Filter out blank thoughts (the agent often returns empty content when
  // it's purely calling a tool — those would clutter the panel).
  const populated = thoughts.filter((t) => t.text && t.text.trim().length > 0);
  if (populated.length === 0) return null;

  const tokens = populated.reduce(
    (acc, t) => ({
      in: acc.in + (t.inputTokens ?? 0),
      out: acc.out + (t.outputTokens ?? 0),
      cache: acc.cache + (t.cacheRead ?? 0),
    }),
    { in: 0, out: 0, cache: 0 },
  );

  return (
    <details
      className="mt-6 border-t border-[color:var(--rule)] pt-4"
      open={open}
      onToggle={(e) => setOpen((e.currentTarget as HTMLDetailsElement).open)}
    >
      <summary className="group flex cursor-pointer list-none items-center gap-2">
        <Quote
          size={12}
          strokeWidth={1.6}
          className="text-[color:var(--ink-faint)]"
        />
        <span className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-soft)] group-hover:text-[color:var(--ink)]">
          {open ? "Hide reasoning" : `Show reasoning · ${populated.length} note${populated.length === 1 ? "" : "s"}`}
        </span>
        <ChevronDown
          size={12}
          strokeWidth={1.6}
          className={clsx(
            "ml-auto text-[color:var(--ink-faint)] transition-transform duration-200",
            open && "rotate-180",
          )}
        />
      </summary>

      <ol className="mt-4 space-y-5 pl-4">
        {populated.map((t, i) => (
          <li key={i}>
            <p className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
              Note {String(t.step).padStart(2, "0")}
            </p>
            <p className="mt-1 font-display text-[15px] italic leading-relaxed text-[color:var(--ink)]">
              {t.text}
            </p>
          </li>
        ))}
      </ol>

      {(tokens.in || tokens.out) > 0 && (
        <p className="mt-5 font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
          {tokens.in.toLocaleString()} in · {tokens.out.toLocaleString()} out
          {tokens.cache > 0 && (
            <> · {tokens.cache.toLocaleString()} cached</>
          )}
        </p>
      )}
    </details>
  );
}
