"use client";

import { useEffect, useState, type FormEvent } from "react";
import {
  Briefcase,
  BookOpen,
  BarChart3,
  Users,
  Star,
  FileText,
  ChevronLeft,
  ChevronRight,
  Loader2,
  type LucideIcon,
} from "lucide-react";
import clsx from "clsx";

import type { WizardStepView } from "./transport";

// WizardCard — Sprint N1 pre-generation multi-step wizard UI.
//
// Replaces the single-shot L1 ClarificationCard. Always fires (no
// vague-topic gating); 3 steps total in the MVP:
//
//   step 1 — scenario picker (radio-card list of 6 categories;
//            REQUIRED). User taps one card, optionally taps 下一步.
//   step 2 — audience free-text (scenario-aware question + placeholder).
//            SKIPPABLE.
//   step 3 — extra-info free-text. SKIPPABLE.
//
// Visual reference: a Manus pre-deck questionnaire — radio-card list
// with leading icon, hairline outline, progress indicator in the
// header, [跳过] [下一步] footer.
//
// All state is owned locally per step view; the parent passes a fresh
// view + onSubmit handler each time, so this component never has to
// remember prior steps.

// Lucide icon names mapping. Backend's WizardScenarioOption.icon is
// a string we look up here — keeps the wire payload tiny without
// shipping SVG over the WebSocket.
const ICON_BY_NAME: Record<string, LucideIcon> = {
  Briefcase,
  BookOpen,
  BarChart3,
  Users,
  Star,
  FileText,
};

export function WizardCard({
  view,
  onSubmit,
  busy,
}: {
  view: WizardStepView;
  onSubmit: (step: number, answer: string, skip: boolean) => void | Promise<void>;
  busy?: boolean;
}) {
  const isScenario = view.kind === "scenario";

  // Local state per step view. Resets whenever a new step arrives
  // (view identity changes) — guarded by useEffect on view.step.
  const [scenarioPick, setScenarioPick] = useState<string>("");
  const [freeText, setFreeText] = useState<string>("");

  useEffect(() => {
    setScenarioPick("");
    setFreeText("");
  }, [view.step]);

  const progressPct = Math.round(((view.step - 1) / view.total) * 100);

  // Validation for the 下一步 button.
  // Step 1 (scenario): must have a pick.
  // Step 2/3 (free-text): allow empty if optional (the button still
  //   disables empty, but the 跳过 button covers the "no answer" path).
  const canNext = isScenario
    ? scenarioPick !== ""
    : freeText.trim().length > 0;

  const submitNext = (e?: FormEvent) => {
    e?.preventDefault();
    if (busy || !canNext) return;
    const answer = isScenario ? scenarioPick : freeText.trim();
    onSubmit(view.step, answer, false);
  };

  const submitSkip = () => {
    if (busy) return;
    onSubmit(view.step, "", true);
  };

  return (
    <div className="mx-auto my-4 w-full max-w-2xl">
      {/* Top vermillion press-strip — matches existing L1 cards. */}
      <div className="h-[3px] w-[42px] bg-[color:var(--vermillion)]" />

      <form
        onSubmit={submitNext}
        className="relative border border-[color:var(--ink)]/15 bg-[#FBF9F2] p-6"
        style={{
          boxShadow:
            "0 2px 0 rgba(26,22,20,0.04), 0 18px 32px -20px rgba(50,40,32,0.32)",
        }}
      >
        {/* ─── Header: kicker + progress ─────────────────────────── */}
        <div className="mb-4 flex items-baseline justify-between gap-2">
          <div className="flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
            <span className="text-[color:var(--vermillion)]">§</span>
            <span>Pre-Brief</span>
            <span className="text-[color:var(--ink-faint)]">/</span>
            <span>
              Step {view.step} of {view.total}
            </span>
          </div>
          {/* Numeric progress, mono caps, e.g. "33%" */}
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.18em] text-[color:var(--ink-faint)]">
            {progressPct}%
          </span>
        </div>

        {/* Hairline progress rule — fills from 0→100% as user advances */}
        <div className="mb-5 h-[1px] w-full bg-[color:var(--ink)]/10">
          <div
            className="h-[1px] bg-[color:var(--vermillion)] transition-all duration-300"
            style={{ width: `${progressPct}%` }}
          />
        </div>

        {/* ─── Question (matches existing display-serif body copy) ── */}
        <h3 className="mb-5 font-display text-[26px] leading-tight text-[color:var(--ink)]">
          {view.question}
        </h3>

        {/* ─── Body: scenario radio-card list OR free-text input ─── */}
        {isScenario ? (
          <ScenarioPicker
            options={view.options ?? []}
            value={scenarioPick}
            onChange={setScenarioPick}
            disabled={busy}
          />
        ) : (
          <FreeTextField
            value={freeText}
            onChange={setFreeText}
            placeholder={view.placeholder ?? ""}
            disabled={busy}
          />
        )}

        {/* ─── Footer: 跳过 + 下一步 ──────────────────────────────── */}
        <div className="mt-6 flex items-center justify-between">
          <div className="flex items-center gap-3">
            {/* The back-arrow + 跳过 cluster matches the Manus reference */}
            {view.step > 1 && (
              <button
                type="button"
                disabled
                className="inline-flex h-7 w-7 cursor-not-allowed items-center justify-center border border-[color:var(--ink)]/20 text-[color:var(--ink-faint)]"
                title="返回（暂不支持）"
              >
                <ChevronLeft size={14} strokeWidth={1.8} />
              </button>
            )}
            {view.optional && (
              <button
                type="button"
                onClick={submitSkip}
                disabled={busy}
                className="font-mono-jb text-[11px] uppercase tracking-[0.22em] text-[color:var(--ink-soft)] underline-offset-4 transition-colors hover:text-[color:var(--vermillion)] hover:underline disabled:opacity-40"
              >
                跳过
              </button>
            )}
          </div>
          <button
            type="submit"
            disabled={!canNext || busy}
            className={clsx(
              "group inline-flex items-center gap-2 px-4 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] transition-all duration-200",
              !canNext || busy
                ? "cursor-not-allowed bg-[color:var(--ink)]/10 text-[color:var(--ink-faint)]"
                : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)] active:translate-y-[1px]",
            )}
          >
            {busy ? (
              <Loader2 size={12} strokeWidth={1.8} className="animate-spin" />
            ) : (
              <ChevronRight
                size={14}
                strokeWidth={1.8}
                className="transition-transform group-hover:translate-x-[2px]"
              />
            )}
            <span>{view.step === view.total ? "完成" : "下一步"}</span>
          </button>
        </div>
      </form>
    </div>
  );
}

// ─── Step-body subcomponents ──────────────────────────────────────────

function ScenarioPicker({
  options,
  value,
  onChange,
  disabled,
}: {
  options: { value: string; label: string; icon: string }[];
  value: string;
  onChange: (next: string) => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-col gap-2.5">
      {options.map((opt) => {
        const Icon = ICON_BY_NAME[opt.icon] ?? FileText;
        const checked = value === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => onChange(opt.value)}
            disabled={disabled}
            className={clsx(
              "group flex items-center gap-3 border px-4 py-3.5 text-left transition-all duration-150",
              "disabled:cursor-not-allowed disabled:opacity-50",
              checked
                ? "border-[color:var(--ink)] bg-[#F5EFE0]/60"
                : "border-[color:var(--ink)]/15 bg-transparent hover:border-[color:var(--ink)]/40 hover:bg-[#F5EFE0]/30",
            )}
          >
            <Icon
              size={18}
              strokeWidth={1.6}
              className={clsx(
                "shrink-0 transition-colors",
                checked
                  ? "text-[color:var(--vermillion)]"
                  : "text-[color:var(--ink-soft)] group-hover:text-[color:var(--ink)]",
              )}
            />
            <span className="flex-1 font-display text-[16px] text-[color:var(--ink)]">
              {opt.label}
            </span>
            {/* Radio indicator — a small ring that fills when checked */}
            <span
              className={clsx(
                "inline-flex h-[14px] w-[14px] shrink-0 items-center justify-center rounded-full border transition-colors",
                checked
                  ? "border-[color:var(--vermillion)]"
                  : "border-[color:var(--ink)]/30",
              )}
            >
              {checked && (
                <span className="h-[6px] w-[6px] rounded-full bg-[color:var(--vermillion)]" />
              )}
            </span>
          </button>
        );
      })}
    </div>
  );
}

function FreeTextField({
  value,
  onChange,
  placeholder,
  disabled,
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder: string;
  disabled?: boolean;
}) {
  return (
    <div className="border-b border-[color:var(--ink)]/40 pb-1 transition-colors focus-within:border-[color:var(--vermillion)]">
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        placeholder={placeholder}
        className="w-full bg-transparent font-display text-[17px] leading-snug text-[color:var(--ink)] placeholder:font-display placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
      />
    </div>
  );
}
