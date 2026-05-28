"use client";

import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { createPortal } from "react-dom";
import { ArrowUpRight, Loader2, Sparkles, X } from "lucide-react";
import clsx from "clsx";

// EditPopover is the floating editor that opens when the user clicks any
// text inside a SlideFrame iframe. It feels — by design — like a margin
// annotation pinned to a printed magazine: warm paper, hairline rules,
// a top vermillion bleed strip, file-folder tabs that "lift" into the
// body, italic serif marginalia quoting the captured text, and a single
// ink-on-paper rectangle action button.
//
// Two surfaces:
//   - 直接改 (DirectEdit)  — single-line input pre-filled with the
//     clicked text. Submits a deterministic Chinese instruction so the
//     agent calls edit_slide_text and skips the LLM rewrite path.
//   - 让 AI 重写 (AIRewrite) — textarea for a natural-language
//     instruction. The agent picks regenerate_slide and rewrites the
//     whole page through the worker LLM.

export type EditTarget = {
  slideIndex: number;
  text: string;
  role: string;
};

export type EditSubmit =
  | { mode: "direct"; slideIndex: number; role: string; oldText: string; newText: string }
  | { mode: "rewrite"; slideIndex: number; instruction: string };

export function EditPopover({
  target,
  anchor,
  busy,
  error,
  onSubmit,
  onClose,
}: {
  target: EditTarget | null;
  anchor: DOMRect | null;
  busy: boolean;
  /** Sprint I0.3 — last submission error. When non-null the popover
   *  stays open and renders this above the Footer with a retry hint;
   *  the existing submit button doubles as the retry action. */
  error?: string | null;
  onSubmit: (s: EditSubmit) => void;
  onClose: () => void;
}) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  if (!mounted || !target || !anchor) return null;
  return createPortal(
    <PopoverBody
      target={target}
      anchor={anchor}
      busy={busy}
      error={error}
      onSubmit={onSubmit}
      onClose={onClose}
    />,
    document.body,
  );
}

type Tab = "direct" | "rewrite";

const POPOVER_WIDTH = 380;
const POPOVER_MARGIN = 16;

function PopoverBody({
  target,
  anchor,
  busy,
  error,
  onSubmit,
  onClose,
}: {
  target: EditTarget;
  anchor: DOMRect;
  busy: boolean;
  error?: string | null;
  onSubmit: (s: EditSubmit) => void;
  onClose: () => void;
}) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  const [tab, setTab] = useState<Tab>("direct");
  const [direct, setDirect] = useState(target.text);
  const [rewrite, setRewrite] = useState("");
  const [enter, setEnter] = useState(false);

  // Recompute the on-screen position. We try below first, fall back to
  // above when there's no room — like a Wikipedia footnote that prefers
  // to live under the cited word but climbs above if the page ends.
  const [pos, setPos] = useState<{ left: number; top: number; placement: "below" | "above" }>(() =>
    computePosition(anchor),
  );
  useLayoutEffect(() => {
    setPos(computePosition(anchor));
    const onResize = () => setPos(computePosition(anchor));
    window.addEventListener("resize", onResize);
    window.addEventListener("scroll", onResize, true);
    return () => {
      window.removeEventListener("resize", onResize);
      window.removeEventListener("scroll", onResize, true);
    };
  }, [anchor]);

  // Mount-in transition: paper card slides ~6px from its origin while
  // fading in. Single rAF tick is enough for the browser to register the
  // initial state before we promote it.
  useEffect(() => {
    const id = requestAnimationFrame(() => setEnter(true));
    return () => cancelAnimationFrame(id);
  }, []);

  // Refocus when switching tabs so keyboard flow stays smooth.
  useEffect(() => {
    const t = setTimeout(() => {
      if (tab === "direct") inputRef.current?.focus();
      else textareaRef.current?.focus();
    }, 40);
    return () => clearTimeout(t);
  }, [tab]);

  // Click-outside + Escape close. The check uses composedPath because
  // the popover lives in a portal — a plain contains() can miss nested
  // portal trees.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    const onClick = (e: MouseEvent) => {
      const node = rootRef.current;
      if (!node) return;
      const path = e.composedPath();
      if (!path.includes(node)) onClose();
    };
    window.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onClick);
    return () => {
      window.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onClick);
    };
  }, [onClose]);

  const trimmedDirect = direct.trim();
  const trimmedRewrite = rewrite.trim();
  // Sprint AF.4 — dropped the `trimmedDirect !== target.text.trim()`
  // equality check. The user might want to RE-RENDER the same text
  // (e.g. after a transient render glitch) — refusing to submit when
  // the input matches the original confused users who typed the
  // original text back in. Server-side edit_slide_text is idempotent
  // so a same-text submit is harmless.
  const canSubmitDirect = trimmedDirect.length > 0 && !busy;
  const canSubmitRewrite = trimmedRewrite.length > 0 && !busy;

  const submitDirect = (e?: FormEvent | KeyboardEvent) => {
    e?.preventDefault();
    if (!canSubmitDirect) return;
    onSubmit({
      mode: "direct",
      slideIndex: target.slideIndex,
      role: target.role,
      oldText: target.text,
      newText: trimmedDirect,
    });
  };
  const submitRewrite = (e?: FormEvent | KeyboardEvent) => {
    e?.preventDefault();
    if (!canSubmitRewrite) return;
    onSubmit({
      mode: "rewrite",
      slideIndex: target.slideIndex,
      instruction: trimmedRewrite,
    });
  };

  const roleLabel = useMemo(() => roleToLabel(target.role), [target.role]);

  // Tiny SVG noise filter painted at very low opacity — gives the paper
  // a tactile fibre; pulls the card away from the LLM-default flat-plastic
  // surface.
  const paperNoise =
    "url(\"data:image/svg+xml;utf8,%3Csvg viewBox='0 0 220 220' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.85' numOctaves='2'/%3E%3CfeColorMatrix values='0 0 0 0 0.07 0 0 0 0 0.04 0 0 0 0 0.03 0 0 0 0.55 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")";

  const transform = enter
    ? "translateY(0) scale(1)"
    : pos.placement === "below"
      ? "translateY(-6px) scale(0.985)"
      : "translateY(6px) scale(0.985)";

  return (
    <div
      ref={rootRef}
      role="dialog"
      aria-label="Edit slide text"
      style={{
        position: "fixed",
        left: pos.left,
        top: pos.top,
        width: POPOVER_WIDTH,
        zIndex: 60,
        transform,
        opacity: enter ? 1 : 0,
        transition:
          "opacity 180ms cubic-bezier(0.16, 1, 0.3, 1), transform 220ms cubic-bezier(0.16, 1, 0.3, 1)",
      }}
    >
      {/* Top vermillion bleed strip — the press registration mark.
          Sits flush with the paper card so the card feels printed, not
          floated. */}
      <div className="h-[3px] w-[42px] bg-[color:var(--vermillion)]" />

      <div
        className="relative border border-[color:var(--ink)]/15 bg-[#FBF9F2] text-[color:var(--ink)]"
        style={{
          boxShadow:
            // soft tinted shadow — warm-grey rather than pure black so it
            // sits on top of the paper canvas without a "Material card" tell
            "0 2px 0 rgba(26,22,20,0.04), 0 18px 32px -20px rgba(50,40,32,0.32), 0 38px 60px -30px rgba(50,40,32,0.18)",
        }}
      >
        {/* Paper grain — never block clicks. */}
        <div
          aria-hidden
          className="pointer-events-none absolute inset-0 z-0 opacity-[0.045] mix-blend-multiply"
          style={{ backgroundImage: paperNoise, backgroundSize: "180px 180px" }}
        />

        {/* ────────── Header strip: breadcrumb + close ────────── */}
        <div className="relative z-10 flex items-center justify-between border-b border-[color:var(--ink)]/12 px-5 pb-3 pt-4">
          <div className="flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
            <span className="text-[color:var(--vermillion)]">§</span>
            <span>Marginalia</span>
            <span className="text-[color:var(--ink-faint)]">/</span>
            <span>Slide {String(target.slideIndex).padStart(2, "0")}</span>
            <span className="text-[color:var(--ink-faint)]">/</span>
            <span>{roleLabel}</span>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="-mr-1 inline-flex h-6 w-6 items-center justify-center text-[color:var(--ink-faint)] transition-colors hover:text-[color:var(--ink)]"
          >
            <X size={13} strokeWidth={1.6} />
          </button>
        </div>

        {/* ────────── Quoted text — italic serif marginalia ────────── */}
        <blockquote className="relative z-10 px-5 pb-1 pt-3">
          <span className="font-mono-jb text-[9px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
            Quoting
          </span>
          <p className="mt-1.5 font-display text-[15px] italic leading-snug text-[color:var(--ink-soft)] before:mr-1 before:content-['“'] after:ml-1 after:content-['”']">
            {target.text}
          </p>
        </blockquote>

        {/* ────────── File-folder tabs ────────── */}
        <div className="relative z-10 mt-3 flex items-end gap-1 px-5">
          <FolderTab active={tab === "direct"} onClick={() => setTab("direct")}>
            直接改
          </FolderTab>
          <FolderTab active={tab === "rewrite"} onClick={() => setTab("rewrite")}>
            <span className="inline-flex items-baseline gap-1">
              让 AI 重写
              <Sparkles size={9} strokeWidth={1.8} className="translate-y-[1px]" />
            </span>
          </FolderTab>
          <div className="flex-1 border-b border-[color:var(--ink)]/15" />
        </div>

        {/* ────────── Body ────────── */}
        <div className="relative z-10 px-5 pb-4 pt-4">
          {tab === "direct" ? (
            <form onSubmit={submitDirect}>
              <label className="font-mono-jb text-[9px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
                Revised wording
              </label>
              <div className="mt-2 border-b border-[color:var(--ink)]/40 pb-1.5 transition-colors focus-within:border-[color:var(--vermillion)]">
                <input
                  ref={inputRef}
                  value={direct}
                  onChange={(e) => setDirect(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") submitDirect(e);
                  }}
                  disabled={busy}
                  placeholder="改写后的文本…"
                  className="w-full bg-transparent font-display text-[18px] leading-snug text-[color:var(--ink)] placeholder:font-display placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
                />
              </div>
              {error && !busy ? <ErrorRow message={error} /> : null}
              <Footer
                hint={error ? "Enter 重试 · Esc 取消" : "Enter 提交 · Esc 取消"}
                actionLabel={error ? "重试" : "覆盖"}
                disabled={!canSubmitDirect}
                busy={busy}
                onSubmit={submitDirect}
              />
            </form>
          ) : (
            <form onSubmit={submitRewrite}>
              <label className="font-mono-jb text-[9px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
                Instruction · 让 AI 重写整页
              </label>
              <div className="mt-2 border-b border-[color:var(--ink)]/40 pb-1.5 transition-colors focus-within:border-[color:var(--vermillion)]">
                <textarea
                  ref={textareaRef}
                  value={rewrite}
                  onChange={(e) => setRewrite(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submitRewrite(e);
                  }}
                  rows={3}
                  disabled={busy}
                  placeholder={`例: "把这页改得更激进、加点数据"`}
                  className="w-full resize-none bg-transparent font-display text-[16px] leading-snug text-[color:var(--ink)] placeholder:font-display placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
                />
              </div>
              {error && !busy ? <ErrorRow message={error} /> : null}
              <Footer
                hint={error ? "⌘ / Ctrl + Enter 重试 · Esc 取消" : "⌘ / Ctrl + Enter 提交 · Esc 取消"}
                actionLabel={error ? "重试" : "重写"}
                disabled={!canSubmitRewrite}
                busy={busy}
                onSubmit={submitRewrite}
              />
            </form>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Sub-bits ─────────────────────────────────────────────────────────

function FolderTab({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={clsx(
        "relative -mb-[1px] inline-flex items-center px-3 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] transition-colors",
        active
          ? "border border-b-0 border-[color:var(--ink)]/15 bg-[#FBF9F2] text-[color:var(--ink)]"
          : "border border-transparent text-[color:var(--ink-faint)] hover:text-[color:var(--ink-soft)]",
      )}
    >
      {/* hairline ink accent on the active tab's top edge */}
      {active ? (
        <span aria-hidden className="absolute left-0 right-0 top-0 h-[2px] bg-[color:var(--ink)]" />
      ) : null}
      {children}
    </button>
  );
}

function Footer({
  hint,
  actionLabel,
  disabled,
  busy,
  onSubmit,
}: {
  hint: string;
  actionLabel: string;
  disabled: boolean;
  busy: boolean;
  onSubmit: () => void;
}) {
  return (
    <div className="mt-4 flex items-center justify-between">
      <span className="font-mono-jb text-[9px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
        {hint}
      </span>
      <button
        type="button"
        onClick={onSubmit}
        disabled={disabled}
        className={clsx(
          "group inline-flex items-center gap-2 px-3 py-1.5 font-mono-jb text-[10px] uppercase tracking-[0.24em] transition-all duration-200",
          disabled
            ? "cursor-not-allowed text-[color:var(--ink-faint)]"
            : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)] active:translate-y-[1px]",
        )}
      >
        {busy ? (
          <Loader2 size={11} strokeWidth={1.8} className="animate-spin" />
        ) : (
          <ArrowUpRight
            size={11}
            strokeWidth={1.8}
            className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]"
          />
        )}
        <span>{actionLabel}</span>
      </button>
    </div>
  );
}

// Sprint I0.3 — inline error pill rendered above Footer when the last
// edit attempt failed. Single hairline rule + vermillion left-tab to
// signal "something went sideways" without yelling — the popover
// stays open and the existing submit button (now labelled 重试) is
// the action.
function ErrorRow({ message }: { message: string }) {
  return (
    <div
      role="alert"
      className="mt-3 flex items-start gap-2 border-l-2 border-[color:var(--vermillion)] bg-[color:var(--vermillion)]/[0.04] px-3 py-2"
    >
      <span className="mt-[2px] font-mono-jb text-[9px] uppercase tracking-[0.24em] text-[color:var(--vermillion)]">
        Err
      </span>
      <span className="font-display text-[12px] italic leading-snug text-[color:var(--ink)]">
        {message}
      </span>
    </div>
  );
}

// ─── Geometry + i18n helpers ──────────────────────────────────────────

function computePosition(anchor: DOMRect): {
  left: number;
  top: number;
  placement: "below" | "above";
} {
  const vw = typeof window !== "undefined" ? window.innerWidth : 1024;
  const vh = typeof window !== "undefined" ? window.innerHeight : 768;

  // Horizontal: prefer left-edge aligned with anchor; clamp to viewport
  // with a healthy margin so the popover never kisses the page edge.
  let left = anchor.left;
  if (left + POPOVER_WIDTH + POPOVER_MARGIN > vw) {
    left = vw - POPOVER_WIDTH - POPOVER_MARGIN;
  }
  if (left < POPOVER_MARGIN) left = POPOVER_MARGIN;

  // Vertical: 10px below anchor by default; flip above when the
  // estimated card height won't fit.
  const estimatedHeight = 280;
  let placement: "below" | "above" = "below";
  let top = anchor.bottom + 10;
  if (top + estimatedHeight + POPOVER_MARGIN > vh) {
    placement = "above";
    top = anchor.top - estimatedHeight - 10;
    if (top < POPOVER_MARGIN) top = POPOVER_MARGIN;
  }
  return { left, top, placement };
}

function roleToLabel(role: string): string {
  switch (role) {
    case "title":
      return "Title";
    case "subtitle":
      return "Subtitle";
    case "bullet":
      return "Bullet";
    case "quote":
      return "Quote";
    case "metric":
      return "Metric";
    case "footer":
      return "Footer";
    default:
      return "Text";
  }
}
