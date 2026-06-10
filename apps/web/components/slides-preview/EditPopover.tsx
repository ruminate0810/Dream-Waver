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
// text inside a SlideFrame iframe. It reads — by design — like a tiny
// retro-terminal window pinned over the slide: hard 2px ink border +
// offset pixel shadow, traffic-dot title bar with a breadcrumb, a
// segmented tab switch, mono marginalia quoting the captured text, and
// a single accent press-button action.
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
      <div className="relative overflow-hidden rounded-pixel border-2 border-ink bg-surface text-ink shadow-pixel-lg">
        {/* ────────── Window bar: traffic dots + breadcrumb + close ────────── */}
        <div className="flex items-center justify-between gap-3 border-b-2 border-ink bg-surface-2 px-4 py-2.5">
          <div className="flex min-w-0 items-center gap-2.5">
            <span aria-hidden className="flex flex-none gap-1.5">
              <i className="h-[9px] w-[9px] rounded-full border border-ink bg-[#ff8a8a]" />
              <i className="h-[9px] w-[9px] rounded-full border border-ink bg-gold" />
              <i className="h-[9px] w-[9px] rounded-full border border-ink bg-grass" />
            </span>
            <div className="flex min-w-0 items-baseline gap-1.5 truncate font-pixel text-[0.55rem] tracking-wide text-muted">
              <span className="text-accent">§</span>
              <span>Marginalia</span>
              <span className="text-line-2">/</span>
              <span>Slide {String(target.slideIndex).padStart(2, "0")}</span>
              <span className="text-line-2">/</span>
              <span>{roleLabel}</span>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="-mr-1 inline-flex h-6 w-6 flex-none items-center justify-center rounded-[4px] text-muted transition-colors hover:bg-paper hover:text-ink"
          >
            <X size={13} strokeWidth={1.6} />
          </button>
        </div>

        {/* ────────── Quoted text — mono marginalia ────────── */}
        <blockquote className="px-4 pb-1 pt-3">
          <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
            Quoting
          </span>
          <p className="mt-1.5 font-mono text-[13px] leading-relaxed text-ink-2 before:mr-1 before:content-['“'] after:ml-1 after:content-['”']">
            {target.text}
          </p>
        </blockquote>

        {/* ────────── Mode tabs — segmented pixel switch ────────── */}
        <div className="mt-3 px-4">
          <div className="inline-flex overflow-hidden rounded-pixel border-2 border-ink">
            <FolderTab active={tab === "direct"} onClick={() => setTab("direct")}>
              直接改
            </FolderTab>
            <FolderTab active={tab === "rewrite"} onClick={() => setTab("rewrite")}>
              <span className="inline-flex items-baseline gap-1">
                让 AI 重写
                <Sparkles size={9} strokeWidth={1.8} className="translate-y-[1px]" />
              </span>
            </FolderTab>
          </div>
        </div>

        {/* ────────── Body ────────── */}
        <div className="px-4 pb-4 pt-4">
          {tab === "direct" ? (
            <form onSubmit={submitDirect}>
              <label className="font-pixel text-[0.55rem] tracking-wide text-muted">
                Revised wording
              </label>
              <div className="mt-2 border-b-2 border-line-2 pb-1.5 transition-colors focus-within:border-accent">
                <input
                  ref={inputRef}
                  value={direct}
                  onChange={(e) => setDirect(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") submitDirect(e);
                  }}
                  disabled={busy}
                  placeholder="改写后的文本…"
                  className="w-full bg-transparent font-mono text-[15px] font-medium leading-snug text-ink placeholder:text-muted focus:outline-none disabled:opacity-50"
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
              <label className="font-mono text-[10px] font-semibold tracking-wide text-muted">
                Instruction · 让 AI 重写整页
              </label>
              <div className="mt-2 border-b-2 border-line-2 pb-1.5 transition-colors focus-within:border-accent">
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
                  className="w-full resize-none bg-transparent font-mono text-[14px] leading-relaxed text-ink placeholder:text-muted focus:outline-none disabled:opacity-50"
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
        "inline-flex items-center px-3 py-1.5 font-mono text-[11px] font-semibold transition-colors",
        active
          ? "bg-ink text-paper"
          : "bg-surface text-ink-2 hover:bg-surface-2 hover:text-ink",
      )}
    >
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
    <div className="mt-4 flex items-center justify-between gap-3">
      <span className="font-mono text-[10px] text-muted">
        {hint}
      </span>
      <button
        type="button"
        onClick={onSubmit}
        disabled={disabled}
        className={clsx(
          "group inline-flex flex-none items-center gap-2 rounded-pixel border-2 px-3 py-1.5 font-mono text-[11px] font-semibold transition-all duration-100",
          disabled
            ? "cursor-not-allowed border-line-2 bg-surface-2 text-muted"
            : "border-ink bg-accent text-white shadow-pixel-sm hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none",
        )}
      >
        {busy ? (
          <Loader2 size={11} strokeWidth={1.8} className="animate-spin" />
        ) : (
          <ArrowUpRight size={11} strokeWidth={1.8} />
        )}
        <span>{actionLabel}</span>
      </button>
    </div>
  );
}

// Sprint I0.3 — inline error row rendered above Footer when the last
// edit attempt failed. Hard pixel callout in the red family to signal
// "something went sideways" without yelling — the popover stays open
// and the existing submit button (now labelled 重试) is the action.
function ErrorRow({ message }: { message: string }) {
  return (
    <div
      role="alert"
      className="mt-3 flex items-start gap-2 rounded-pixel border-2 border-ink bg-[#fdece9] px-3 py-2"
    >
      <span className="mt-[2px] font-mono text-[10px] font-semibold uppercase tracking-wide text-[#a23a2a]">
        Err
      </span>
      <span className="font-mono text-[12px] leading-snug text-ink">
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
