"use client";

import clsx from "clsx";
import { Sparkles, Wrench } from "lucide-react";

import type { SlideMode } from "@/lib/slideMode";

// ModeToggle — pixel segmented control choosing the generation path.
// SVG (flagship bespoke vector slides) ↔ Agent (chat-editable templated).
// A proper radiogroup: ←/→ move the selection, the title carries a hint.
const OPTIONS: {
  value: SlideMode;
  label: string;
  icon: React.ComponentType<{ size?: number; strokeWidth?: number }>;
  hint: string;
}[] = [
  { value: "svg", label: "SVG", icon: Sparkles, hint: "每页一张定制矢量 <svg> — 视觉天花板最高（默认）" },
  { value: "agent", label: "Agent", icon: Wrench, hint: "模板化 HTML + 对话编辑 / 向导 — 可聊天逐页改" },
];

export function ModeToggle({
  value,
  onChange,
  className,
}: {
  value: SlideMode;
  onChange: (m: SlideMode) => void;
  className?: string;
}) {
  return (
    <div
      role="radiogroup"
      aria-label="生成模式"
      className={clsx("inline-flex overflow-hidden rounded-pixel border-2 border-ink", className)}
    >
      {OPTIONS.map((o, i) => {
        const Icon = o.icon;
        const active = value === o.value;
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={active}
            tabIndex={active ? 0 : -1}
            title={o.hint}
            onClick={() => onChange(o.value)}
            onKeyDown={(e) => {
              if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
                e.preventDefault();
                onChange(OPTIONS[(i + 1) % OPTIONS.length].value);
              }
            }}
            className={clsx(
              "inline-flex items-center gap-1.5 px-2.5 py-1 font-mono text-[11px] font-semibold transition-colors",
              i > 0 && "border-l-2 border-ink",
              active
                ? "bg-accent text-white"
                : "bg-surface text-ink-2 hover:bg-surface-2 hover:text-ink",
            )}
          >
            <Icon size={12} strokeWidth={2.2} />
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
