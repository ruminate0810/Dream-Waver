// Pixel / retro-terminal UI primitives. Tokenized against tailwind.config.ts
// (paper / ink / accent / hard pixel shadows) so the whole app shares one
// visual language. Import from "@/components/ui/pixel".
"use client";

import clsx from "clsx";
import { Sprite, type SpriteName } from "./Sprite";

export { Sprite };
export type { SpriteName };

/* ── PixelButton ─────────────────────────────────────────────────────
   Hard-shadow button that "presses" into the page on click. */
type ButtonVariant = "default" | "primary" | "accent" | "ghost";

const BTN_BASE =
  "inline-flex items-center justify-center gap-2 font-mono font-semibold text-[0.82rem] leading-none rounded-pixel border-2 px-4 py-2.5 transition-transform duration-100 " +
  "hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover " +
  "active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none " +
  "disabled:opacity-50 disabled:cursor-not-allowed disabled:translate-x-0 disabled:translate-y-0";

const BTN_VARIANT: Record<ButtonVariant, string> = {
  default: "border-ink bg-surface text-ink shadow-pixel-sm",
  primary: "border-ink bg-ink text-paper shadow-pixel-sm",
  accent: "border-ink bg-accent text-white shadow-pixel-sm",
  // Ghost has no shadow and doesn't translate — for secondary actions.
  ghost:
    "border-line-2 bg-surface text-ink shadow-none hover:translate-x-0 hover:translate-y-0 hover:bg-surface-2 hover:!shadow-none active:translate-x-0 active:translate-y-0",
};

export function PixelButton({
  variant = "default",
  className,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant }) {
  return <button className={clsx(BTN_BASE, BTN_VARIANT[variant], className)} {...props} />;
}

/* ── WindowCard ──────────────────────────────────────────────────────
   macOS-style window chrome (three dots + optional title) over a hard
   pixel border + offset shadow. The signature card of the whole system. */
export function WindowCard({
  title,
  right,
  dots = true,
  children,
  className,
  bodyClassName,
  barClassName,
}: {
  title?: React.ReactNode;
  right?: React.ReactNode;
  dots?: boolean;
  children?: React.ReactNode;
  className?: string;
  bodyClassName?: string;
  barClassName?: string;
}) {
  return (
    <div
      className={clsx(
        "overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel",
        className,
      )}
    >
      <div
        className={clsx(
          "flex items-center gap-2 border-b-2 border-ink bg-surface-2 px-3 py-2",
          barClassName,
        )}
      >
        {dots && (
          <span className="flex flex-none gap-1.5">
            <i className="h-[10px] w-[10px] rounded-full border border-ink bg-[#ff8a8a]" />
            <i className="h-[10px] w-[10px] rounded-full border border-ink bg-gold" />
            <i className="h-[10px] w-[10px] rounded-full border border-ink bg-grass" />
          </span>
        )}
        {title && (
          <span className="truncate text-[0.72rem] tracking-wide text-muted">{title}</span>
        )}
        {right && <span className="ml-auto flex-none">{right}</span>}
      </div>
      <div className={clsx("p-4", bodyClassName)}>{children}</div>
    </div>
  );
}

/* ── StatusChip ──────────────────────────────────────────────────────
   Agent/job state pill with a leading dot. `working` pulses. */
export type PixelStatus = "working" | "queued" | "idle" | "done" | "error";

const CHIP: Record<PixelStatus, { box: string; dot: string; label: string }> = {
  working: { box: "bg-accent text-white", dot: "bg-white animate-pixpulse", label: "Working" },
  queued: { box: "bg-[#fff7e8] text-[#9a6b15]", dot: "bg-gold", label: "Queued" },
  idle: { box: "bg-surface-2 text-muted", dot: "bg-muted", label: "Idle" },
  done: { box: "bg-[#eaf7ef] text-[#1f7a4d]", dot: "bg-grass", label: "Done" },
  error: { box: "bg-[#fdece9] text-[#a23a2a]", dot: "bg-[#d4503a]", label: "Error" },
};

export function StatusChip({
  status,
  children,
  className,
}: {
  status: PixelStatus;
  children?: React.ReactNode;
  className?: string;
}) {
  const c = CHIP[status];
  return (
    <span
      className={clsx(
        "inline-flex items-center gap-2 rounded-[4px] border-[1.5px] border-ink px-2.5 py-1 text-[0.64rem] font-semibold",
        c.box,
        className,
      )}
    >
      <span className={clsx("h-[7px] w-[7px] flex-none rounded-full", c.dot)} />
      {children ?? c.label}
    </span>
  );
}

/* ── Eyebrow ─────────────────────────────────────────────────────────
   Pixel-font kicker pill (the "✦ AGENT-NATIVE CREATIVE OS" chip). */
export function Eyebrow({
  children,
  className,
  tone = "accent",
}: {
  children: React.ReactNode;
  className?: string;
  tone?: "accent" | "plain";
}) {
  return (
    <span
      className={clsx(
        "inline-flex items-center gap-2 rounded-full border-2 border-ink px-3 py-1.5 font-pixel text-[0.6rem] tracking-wide shadow-pixel-sm",
        tone === "accent" ? "bg-accent-soft" : "bg-surface",
        className,
      )}
    >
      {children}
    </span>
  );
}

/* ── SectionKicker ───────────────────────────────────────────────────
   The "// WHAT IT DOES" code-comment label above section titles. */
export function SectionKicker({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={clsx("font-pixel text-[0.6rem] tracking-wide text-accent", className)}>
      {children}
    </div>
  );
}
