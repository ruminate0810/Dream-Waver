"use client";

import { useState, type FormEvent } from "react";
import { createPortal } from "react-dom";
import { ArrowUpRight, Loader2, X } from "lucide-react";
import clsx from "clsx";

import { createUserTemplate, type UserTemplate } from "@/lib/api";
import { TEMPLATES } from "@/lib/templates";

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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-[color:var(--ink)]/40 p-4 backdrop-blur-[2px]">
      <div
        className="relative flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden border border-[color:var(--ink)]/15 bg-[#FBF9F2] shadow-[0_24px_60px_-20px_rgba(50,40,32,0.50)]"
        style={{ boxShadow: "0 2px 0 rgba(26,22,20,0.04), 0 32px 64px -28px rgba(50,40,32,0.55)" }}
      >
        {/* Top vermillion bleed strip */}
        <div className="h-[3px] w-[42px] bg-[color:var(--vermillion)]" />

        <header className="flex items-baseline justify-between gap-3 border-b border-[color:var(--rule)] px-6 pt-5 pb-4">
          <div className="flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
            <span className="text-[color:var(--vermillion)]">§</span>
            <span>New Template</span>
            <span className="text-[color:var(--ink-faint)]">/</span>
            <span>添加我的模板</span>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-7 w-7 items-center justify-center text-[color:var(--ink-faint)] transition-colors hover:text-[color:var(--ink)]"
            aria-label="Close"
          >
            <X size={14} strokeWidth={1.8} />
          </button>
        </header>

        <form onSubmit={submit} className="flex flex-1 flex-col gap-6 overflow-y-auto px-6 py-6">
          {/* Name */}
          <div className="flex flex-col gap-2">
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              Name · 名字
            </span>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={120}
              autoFocus
              disabled={submitting}
              placeholder='例: "公司主调" / "Q4 客户演讲"'
              className="border-b border-[color:var(--ink)]/40 bg-transparent pb-1 font-display text-[18px] text-[color:var(--ink)] placeholder:text-[color:var(--ink-faint)] focus:border-[color:var(--vermillion)] focus:outline-none disabled:opacity-50"
            />
          </div>

          {/* Theme picker */}
          <div className="flex flex-col gap-3">
            <div className="flex items-baseline justify-between gap-2">
              <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
                Theme · 基底主题
              </span>
              <span className="font-display text-[13px] italic text-[color:var(--ink-soft)]">
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
                        "relative aspect-[16/10] w-full overflow-hidden border bg-white transition-all",
                        isPicked
                          ? "border-[color:var(--vermillion)] shadow-[0_0_0_2px_var(--vermillion)]"
                          : "border-[color:var(--ink)]/15 group-hover:border-[color:var(--ink)]/40",
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
                        "font-mono-jb text-[9px] uppercase tracking-[0.18em] transition-colors",
                        isPicked
                          ? "text-[color:var(--vermillion)]"
                          : "text-[color:var(--ink-soft)]",
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
            <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
              Font · 字体
            </span>
            <select
              value={fontFamily}
              onChange={(e) => setFontFamily(e.target.value)}
              disabled={submitting}
              className="border-b border-[color:var(--ink)]/40 bg-transparent pb-1 font-display text-[15px] text-[color:var(--ink)] focus:border-[color:var(--vermillion)] focus:outline-none disabled:opacity-50"
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
              className="border-l-2 border-[color:var(--vermillion)] bg-[color:var(--vermillion)]/[0.05] px-3 py-2 font-display text-[13px] italic leading-snug text-[color:var(--ink)]"
            >
              {err}
            </div>
          ) : null}
        </form>

        <footer className="flex items-center justify-between gap-3 border-t border-[color:var(--rule)] px-6 py-4">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
            保存后出现在「我的模板」里
          </span>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={onClose}
              disabled={submitting}
              className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => submit()}
              disabled={submitting || !name.trim()}
              className={clsx(
                "group inline-flex items-center gap-2 px-4 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] transition-all duration-200",
                submitting || !name.trim()
                  ? "cursor-not-allowed text-[color:var(--ink-faint)]"
                  : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)] active:translate-y-[1px]",
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
      <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
        {label}
      </span>
      <div className="flex items-center gap-3 border-b border-[color:var(--ink)]/40 pb-1 focus-within:border-[color:var(--vermillion)]">
        <input
          type="color"
          value={value || "#000000"}
          onChange={(e) => setValue(e.target.value)}
          disabled={disabled}
          className="h-8 w-10 cursor-pointer border border-[color:var(--ink)]/15 bg-transparent disabled:cursor-not-allowed"
          aria-label={label + " swatch"}
        />
        <input
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          disabled={disabled}
          placeholder={placeholder}
          className="flex-1 bg-transparent font-mono-jb text-[13px] uppercase tracking-[0.04em] text-[color:var(--ink)] placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
        />
        {value ? (
          <button
            type="button"
            onClick={() => setValue("")}
            disabled={disabled}
            aria-label={`Clear ${label}`}
            className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)] transition-colors hover:text-[color:var(--vermillion)]"
          >
            clear
          </button>
        ) : null}
      </div>
    </div>
  );
}
