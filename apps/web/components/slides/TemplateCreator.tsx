"use client";

import { useState, type FormEvent } from "react";
import { createPortal } from "react-dom";
import { ArrowUpRight, Loader2, X } from "lucide-react";
import clsx from "clsx";

import { createUserTemplate, type UserTemplate } from "@/lib/api";
import { TEMPLATES } from "@/lib/templates";
import { useEscapeKey } from "@/lib/useEscapeKey";

// Sprint T4 — modal that lets a user save a theme + brand preset as
// "我的模板". MVP scope:
//   - name (required)
//   - base theme (chip rail — clicks set the picked theme)
//   - brand primary colour (optional; falls back to theme accent)
//   - brand accent colour (optional)
//   - font family (optional preset list)
//
// On submit, POST /api/v1/templates. Success closes the modal and
// calls onCreated(template) so the parent can append to its list +
// optionally pre-select the new template.
//
// Portal so the modal escapes any local overflow:hidden ancestors on
// /slides/new (the gallery sections use overflow-hidden on their
// cards).

const FONT_OPTIONS: Array<{ value: string; label: string }> = [
  { value: "", label: "默认 · 跟随主题" },
  { value: "Inter, system-ui, sans-serif", label: "Inter (modern sans)" },
  { value: "Fraunces, 'Source Han Serif SC', serif", label: "Fraunces (editorial serif)" },
  { value: "JetBrains Mono, 'Source Han Mono', monospace", label: "JetBrains Mono (tech)" },
  { value: "Caveat, 'Source Han Sans SC', cursive", label: "Caveat (handwritten)" },
  { value: "Bodoni Moda, 'Source Han Serif SC', serif", label: "Bodoni Moda (noir display)" },
  { value: "Source Han Sans SC, 'Noto Sans SC', sans-serif", label: "思源黑体 (中文优先)" },
];

export function TemplateCreator({
  defaultTheme,
  onClose,
  onCreated,
}: {
  /** Pre-selects a theme — typically the user's currently-picked Style Atlas card. */
  defaultTheme: string;
  onClose: () => void;
  onCreated: (template: UserTemplate) => void;
}) {
  const [name, setName] = useState("");
  const [theme, setTheme] = useState(defaultTheme);
  const [brandPrimary, setBrandPrimary] = useState("");
  const [brandAccent, setBrandAccent] = useState("");
  const [fontFamily, setFontFamily] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Esc closes the modal (it's only mounted while open).
  useEscapeKey(true, onClose);

  async function submit(e?: FormEvent) {
    e?.preventDefault();
    if (submitting) return;
    const trimmedName = name.trim();
    if (!trimmedName) {
      setErr("请给模板起一个名字");
      return;
    }
    if (brandPrimary && !/^#[0-9A-Fa-f]{6}$/.test(brandPrimary)) {
      setErr("主色必须是 #RRGGBB 格式");
      return;
    }
    if (brandAccent && !/^#[0-9A-Fa-f]{6}$/.test(brandAccent)) {
      setErr("辅色必须是 #RRGGBB 格式");
      return;
    }
    setSubmitting(true);
    setErr(null);
    try {
      const created = await createUserTemplate({
        name: trimmedName,
        theme,
        brand_primary: brandPrimary || undefined,
        brand_accent: brandAccent || undefined,
        font_family: fontFamily || undefined,
      });
      onCreated(created);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "保存失败");
      setSubmitting(false);
    }
  }

  // SSR-guard the portal — createPortal needs document.
  if (typeof document === "undefined") return null;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-ink/40 p-4 backdrop-blur-[2px]">
      <div className="relative flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-pixel border-2 border-ink bg-surface-2 shadow-pixel-lg">
        {/* Top accent bleed strip */}
        <div className="h-[3px] w-[42px] bg-accent" />

        <header className="flex items-center justify-between gap-3 border-b border-line px-6 pt-5 pb-4">
          <div className="flex items-center gap-2 font-pixel text-[0.55rem] tracking-wide text-ink-2">
            <span className="text-accent">§</span>
            <span>New Template</span>
            <span className="text-muted">/</span>
            <span className="font-mono text-[10px] font-semibold tracking-wide">添加我的模板</span>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-7 w-7 items-center justify-center rounded-pixel border-2 border-line-2 text-ink-2 transition-colors hover:border-ink hover:text-ink"
            aria-label="Close"
          >
            <X size={14} strokeWidth={1.8} />
          </button>
        </header>

        <form onSubmit={submit} className="flex flex-1 flex-col gap-6 overflow-y-auto px-6 py-6">
          {/* Name */}
          <div className="flex flex-col gap-2">
            <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
              Name · <span className="font-mono text-[10px] font-semibold tracking-wide">名字</span>
            </span>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={120}
              autoFocus
              disabled={submitting}
              placeholder='例: "公司主调" / "Q4 客户演讲"'
              className="rounded-pixel border-2 border-ink bg-surface px-3 py-2 font-mono text-[18px] text-ink placeholder:text-muted focus:shadow-pixel-sm focus:outline-none disabled:opacity-50"
            />
          </div>

          {/* Theme picker */}
          <div className="flex flex-col gap-3">
            <div className="flex items-baseline justify-between gap-2">
              <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
                Theme · <span className="font-mono text-[10px] font-semibold tracking-wide">基底主题</span>
              </span>
              <span className="font-mono text-[13px] font-semibold text-ink-2">
                {TEMPLATES.find((t) => t.name === theme)?.label ?? theme}
              </span>
            </div>
            <div className="-mx-1 flex gap-2 overflow-x-auto pb-1 pl-1 pr-1">
              {TEMPLATES.map((t) => {
                const isPicked = theme === t.name;
                return (
                  <button
                    key={t.name}
                    type="button"
                    disabled={submitting}
                    onClick={() => setTheme(t.name)}
                    className={clsx(
                      "group flex w-[100px] shrink-0 flex-col gap-1 transition-all",
                      submitting ? "cursor-not-allowed opacity-50" : "cursor-pointer",
                    )}
                  >
                    <div
                      className={clsx(
                        "relative aspect-[16/10] w-full overflow-hidden rounded-pixel border-2 bg-surface transition-all",
                        isPicked
                          ? "border-ink bg-accent-soft shadow-pixel-sm"
                          : "border-line-2 group-hover:border-ink",
                      )}
                    >
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        src={t.thumbnail}
                        alt={`${t.label} preview`}
                        loading="lazy"
                        draggable={false}
                        className="h-full w-full object-cover"
                      />
                    </div>
                    <span
                      className={clsx(
                        "font-mono text-[9px] font-semibold uppercase tracking-wide transition-colors",
                        isPicked
                          ? "text-accent"
                          : "text-ink-2",
                      )}
                    >
                      {t.label}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Brand colours */}
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <ColorField
              label="Primary · 主色"
              value={brandPrimary}
              setValue={setBrandPrimary}
              placeholder="#0066FF"
              disabled={submitting}
            />
            <ColorField
              label="Accent · 辅色"
              value={brandAccent}
              setValue={setBrandAccent}
              placeholder="#FF6B35"
              disabled={submitting}
            />
          </div>

          {/* Font */}
          <div className="flex flex-col gap-2">
            <span className="font-pixel text-[0.55rem] tracking-wide text-muted">
              Font · <span className="font-mono text-[10px] font-semibold tracking-wide">字体</span>
            </span>
            <select
              value={fontFamily}
              onChange={(e) => setFontFamily(e.target.value)}
              disabled={submitting}
              className="rounded-pixel border-2 border-ink bg-surface px-3 py-2 font-mono text-[15px] text-ink focus:shadow-pixel-sm focus:outline-none disabled:opacity-50"
            >
              {FONT_OPTIONS.map((f) => (
                <option key={f.value || "_default"} value={f.value}>
                  {f.label}
                </option>
              ))}
            </select>
          </div>

          {err ? (
            <div
              role="alert"
              className="rounded-pixel border-2 border-ink bg-[#fdece9] px-3 py-2 font-mono text-[13px] leading-snug text-[#a23a2a]"
            >
              {err}
            </div>
          ) : null}
        </form>

        <footer className="flex items-center justify-between gap-3 border-t border-line px-6 py-4">
          <span className="font-mono text-[10px] font-semibold tracking-wide text-muted">
            保存后出现在「我的模板」里
          </span>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={onClose}
              disabled={submitting}
              className="rounded-pixel border-2 border-line-2 px-3 py-2 font-mono text-[11px] font-semibold tracking-wide text-ink-2 transition-colors hover:border-ink hover:text-ink"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => submit()}
              disabled={submitting || !name.trim()}
              className={clsx(
                "group inline-flex items-center gap-2 rounded-pixel border-2 border-ink px-4 py-2 font-mono text-[11px] font-semibold tracking-wide transition-all duration-150",
                submitting || !name.trim()
                  ? "cursor-not-allowed bg-surface-2 text-muted"
                  : "bg-accent text-white shadow-pixel-sm hover:translate-x-[1px] hover:translate-y-[1px] hover:shadow-pixel-hover active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none",
              )}
            >
              {submitting ? (
                <Loader2 size={12} strokeWidth={1.8} className="animate-spin" />
              ) : (
                <ArrowUpRight
                  size={12}
                  strokeWidth={1.8}
                  className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]"
                />
              )}
              <span>保存</span>
            </button>
          </div>
        </footer>
      </div>
    </div>,
    document.body,
  );
}

function ColorField({
  label,
  value,
  setValue,
  placeholder,
  disabled,
}: {
  label: string;
  value: string;
  setValue: (v: string) => void;
  placeholder: string;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-col gap-2">
      <span className="font-mono text-[10px] font-semibold tracking-wide text-muted">
        {label}
      </span>
      <div className="flex items-center gap-3 rounded-pixel border-2 border-ink bg-surface px-3 py-2 focus-within:shadow-pixel-sm">
        <input
          type="color"
          value={value || "#000000"}
          onChange={(e) => setValue(e.target.value)}
          disabled={disabled}
          className="h-8 w-10 cursor-pointer rounded-pixel border-2 border-ink bg-transparent disabled:cursor-not-allowed"
          aria-label={label + " swatch"}
        />
        <input
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          disabled={disabled}
          placeholder={placeholder}
          className="flex-1 bg-transparent font-mono text-[13px] uppercase tracking-[0.04em] text-ink placeholder:text-muted focus:outline-none disabled:opacity-50"
        />
        {value ? (
          <button
            type="button"
            onClick={() => setValue("")}
            disabled={disabled}
            aria-label={`Clear ${label}`}
            className="font-pixel text-[0.5rem] tracking-wide text-muted transition-colors hover:text-accent"
          >
            clear
          </button>
        ) : null}
      </div>
    </div>
  );
}
