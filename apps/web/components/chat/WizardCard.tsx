"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
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
  Sparkles,
  type LucideIcon,
} from "lucide-react";
import clsx from "clsx";

import type { WizardStepView } from "./transport";
import { useCardMotion } from "@/lib/motion";

// WizardCard — Sprint N1 pre-generation multi-step wizard UI.
//
// Replaces the single-shot L1 ClarificationCard. Always fires (no
// vague-topic gating); 3 steps total in the MVP:
//
//   step 1 — scenario picker (radio-card list of 6 categories;
//            REQUIRED). May arrive with view.suggested_value set
//            (N1.g topic-keyword heuristic) → that card shows an
//            「AI 建议」badge and is pre-selected.
//   step 2 — audience free-text (scenario-aware question + placeholder).
//            SKIPPABLE.
//   step 3 — extra-info free-text. SKIPPABLE.
//
// N1.i polish pass:
//   - step indicator dots (●○○ → ○●○ → ○○●) under the header
//   - breadcrumb of prior answers above the question (「场景: 商业计划」)
//   - 「AI 建议」 sparkle badge on the suggested scenario card
//   - back button (←) now functional: dispatches with back=true,
//     backend re-emits the previous step with prior choice preserved
//   - subtle vermillion left-edge bar on selected radio card
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
  onSubmit: (
    step: number,
    answer: string,
    skip: boolean,
    back?: boolean,
  ) => void | Promise<void>;
  busy?: boolean;
}) {
  // Sprint Q — `kind` is now usually "select" (LLM-generated dynamic
  // question). The legacy "scenario" value from the N1 hardcoded wizard
  // is kept as an alias so any in-flight pre-Q job still renders.
  const isScenario = view.kind === "scenario" || view.kind === "select";

  // Local state per step view. Resets whenever a new step arrives
  // (view identity changes) — guarded by useEffect on view.step +
  // suggested_value. The latter ensures back-step pre-fills with
  // the user's prior pick (which backend echoes via suggested_value).
  const [scenarioPick, setScenarioPick] = useState<string>(view.suggested_value ?? "");
  const [freeText, setFreeText] = useState<string>(() => {
    // N1.i — when going back to a free-text step, pre-fill with the
    // prior answer if it's still in the breadcrumb (backend keeps
    // them keyed by step).
    if (view.kind !== "free-text") return "";
    const keyByStep: Record<number, string> = { 2: "audience", 3: "extra" };
    const k = keyByStep[view.step];
    return (k && view.previous_answers?.[k]) ?? "";
  });

  // Sprint Q.5 — local submit lock that flips true the instant the
  // user clicks any submit button. Released when a NEW view arrives
  // (different step → fresh form). Without this, a fast user can
  // click 下一步 / 完成 multiple times before the server's busy state
  // propagates back through the WS stream; the duplicate clicks fire
  // POSTs that error out on the backend (the gate is already cleared).
  // The backend now ignores those benign errors, but this stops them
  // at the source — cleaner UX (button visibly disables on tap).
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setScenarioPick(view.suggested_value ?? "");
    if (view.kind === "free-text") {
      const keyByStep: Record<number, string> = { 2: "audience", 3: "extra" };
      const k = keyByStep[view.step];
      setFreeText((k && view.previous_answers?.[k]) ?? "");
    } else {
      setFreeText("");
    }
    // Reset the lock on every fresh step view — the user can submit
    // the NEW step.
    setSubmitting(false);
  }, [view.step, view.suggested_value, view.kind, view.previous_answers]);

  const progressPct = Math.round(((view.step - 1) / view.total) * 100);

  // Validation for the 下一步 button.
  // Step 1 (scenario): must have a pick.
  // Step 2/3 (free-text): allow empty if optional (the button still
  //   disables empty, but the 跳过 button covers the "no answer" path).
  const canNext = isScenario
    ? scenarioPick !== ""
    : freeText.trim().length > 0;

  // `locked` covers either signal: server-busy OR local submit lock
  // engaged. Buttons + form-submit all read this.
  const locked = busy || submitting;

  const submitNext = (e?: FormEvent) => {
    e?.preventDefault();
    if (locked || !canNext) return;
    setSubmitting(true);
    const answer = isScenario ? scenarioPick : freeText.trim();
    onSubmit(view.step, answer, false);
  };

  const submitSkip = () => {
    if (locked) return;
    setSubmitting(true);
    onSubmit(view.step, "", true);
  };

  const submitBack = () => {
    if (locked || !view.can_go_back) return;
    setSubmitting(true);
    // Go back to step N-1; backend re-emits that step's view with
    // prior answer pre-filled (via suggested_value / previous_answers).
    onSubmit(view.step - 1, "", false, true);
  };

  // Sprint Z.2 — mount entrance. Card fades + rises 12px once on
  // first paint. Reduced-motion users get opacity-only. The hook
  // does nothing on subsequent re-renders (e.g. when view.step
  // changes for a multi-step wizard) — that's deliberate, we don't
  // want the whole card flying around per step.
  const rootRef = useRef<HTMLDivElement | null>(null);
  useCardMotion(rootRef);

  return (
    <div ref={rootRef} className="mx-auto my-4 w-full max-w-2xl">
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
        {/* ─── Header: kicker + numeric progress ─────────────────── */}
        <div className="mb-4 flex items-baseline justify-between gap-2">
          <div className="flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)]">
            <span className="text-[color:var(--vermillion)]">§</span>
            <span>Pre-Brief</span>
            <span className="text-[color:var(--ink-faint)]">/</span>
            <span>
              Step {view.step} of {view.total}
            </span>
          </div>
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.18em] text-[color:var(--ink-faint)]">
            {progressPct}%
          </span>
        </div>

        {/* ─── Step indicator dots — clear 3-step affordance ────── */}
        <div className="mb-4 flex items-center gap-1.5">
          {Array.from({ length: view.total }).map((_, i) => {
            const n = i + 1;
            const done = n < view.step;
            const active = n === view.step;
            return (
              <span
                key={n}
                aria-label={`Step ${n}${active ? " (current)" : done ? " (done)" : ""}`}
                className={clsx(
                  "h-[6px] rounded-full transition-all duration-300",
                  active
                    ? "w-7 bg-[color:var(--vermillion)]"
                    : done
                      ? "w-[6px] bg-[color:var(--ink)]/60"
                      : "w-[6px] bg-[color:var(--ink)]/15",
                )}
              />
            );
          })}
        </div>

        {/* ─── Breadcrumb of prior answers (N1.i) ─────────────────
            Shown on steps 2+ — gives the user orientation + lets them
            verify the path so far. Uses small mono caps tags.        */}
        {view.previous_scenario && view.step > 1 && (
          <div className="mb-4 flex flex-wrap items-center gap-2 font-mono-jb text-[10px] uppercase tracking-[0.18em] text-[color:var(--ink-soft)]">
            <span className="text-[color:var(--ink-faint)]">已答</span>
            <BreadcrumbTag label="场景" value={view.previous_scenario} />
            {view.previous_answers?.audience && view.step > 2 && (
              <BreadcrumbTag label="受众" value={view.previous_answers.audience} />
            )}
          </div>
        )}

        {/* ─── Question (matches existing display-serif body copy) ── */}
        <h3 className="mb-5 font-display text-[26px] leading-tight text-[color:var(--ink)]">
          {view.question}
        </h3>

        {/* ─── Body: scenario radio-card list OR free-text input ─── */}
        {isScenario ? (
          <ScenarioPicker
            options={view.options ?? []}
            value={scenarioPick}
            suggested={view.suggested_value}
            onChange={setScenarioPick}
            disabled={locked}
          />
        ) : (
          <FreeTextField
            value={freeText}
            onChange={setFreeText}
            placeholder={view.placeholder ?? ""}
            disabled={locked}
          />
        )}

        {/* ─── Footer: ← + 跳过 (left)   |   下一步 / 完成 (right) */}
        <div className="mt-6 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={submitBack}
              disabled={locked || !view.can_go_back}
              title={view.can_go_back ? "返回上一步" : "已经是第一步"}
              className={clsx(
                "inline-flex h-7 w-7 items-center justify-center border transition-colors",
                view.can_go_back && !locked
                  ? "border-[color:var(--ink)]/25 text-[color:var(--ink-soft)] hover:border-[color:var(--vermillion)] hover:text-[color:var(--vermillion)]"
                  : "cursor-not-allowed border-[color:var(--ink)]/15 text-[color:var(--ink-faint)]/50",
              )}
            >
              <ChevronLeft size={14} strokeWidth={1.8} />
            </button>
            {view.optional && (
              <button
                type="button"
                onClick={submitSkip}
                disabled={locked}
                className="font-mono-jb text-[11px] uppercase tracking-[0.22em] text-[color:var(--ink-soft)] underline-offset-4 transition-colors hover:text-[color:var(--vermillion)] hover:underline disabled:opacity-40"
              >
                跳过
              </button>
            )}
          </div>
          <button
            type="submit"
            disabled={!canNext || locked}
            className={clsx(
              "group inline-flex items-center gap-2 px-4 py-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] transition-all duration-200",
              !canNext || locked
                ? "cursor-not-allowed bg-[color:var(--ink)]/10 text-[color:var(--ink-faint)]"
                : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)] active:translate-y-[1px]",
            )}
          >
            {/* Sprint U3.1 — spinner driven by `locked` (= busy ||
                submitting) instead of just `busy`. The local
                `submitting` flag flips synchronously on click while
                `busy` only flips when the first server event lands;
                without this, ~100ms of "is it frozen?" feedback gap
                opens. */}
            {locked ? (
              <Loader2 size={12} strokeWidth={1.8} className="animate-spin" />
            ) : (
              <ChevronRight
                size={14}
                strokeWidth={1.8}
                className="transition-transform group-hover:translate-x-[2px]"
              />
            )}
            <span>
              {locked
                ? "处理中"
                : view.step === view.total
                  ? "完成"
                  : "下一步"}
            </span>
          </button>
        </div>

        {/* Sprint U3.1 — visible caption when waiting. LLM clarification
            replies (or — on the final 完成 click — the entire outline
            planning loop) can take 30-90s; without this caption the
            user thinks the page froze. */}
        {locked ? (
          <p className="mt-3 font-display text-[13px] italic text-[color:var(--ink-soft)]">
            {view.step === view.total
              ? "agent 正在规划大纲... 通常 30-90 秒"
              : "处理中... agent 正在准备下一步"}
          </p>
        ) : null}
      </form>
    </div>
  );
}

// ─── Step-body subcomponents ──────────────────────────────────────────

function ScenarioPicker({
  options,
  value,
  suggested,
  onChange,
  disabled,
}: {
  options: { value: string; label: string; icon: string }[];
  value: string;
  suggested?: string;
  onChange: (next: string) => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-col gap-2.5">
      {options.map((opt) => {
        const Icon = ICON_BY_NAME[opt.icon] ?? FileText;
        const checked = value === opt.value;
        const isSuggested = suggested === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => onChange(opt.value)}
            disabled={disabled}
            className={clsx(
              // N1.i — slight bump on left padding to leave room for
              // the vermillion left-edge bar that appears when checked.
              "group relative flex items-center gap-3 border px-4 py-3.5 pl-5 text-left transition-all duration-150",
              "disabled:cursor-not-allowed disabled:opacity-50",
              checked
                ? "border-[color:var(--ink)] bg-[#F5EFE0]/60"
                : "border-[color:var(--ink)]/15 bg-transparent hover:border-[color:var(--ink)]/40 hover:bg-[#F5EFE0]/30",
            )}
          >
            {/* N1.i — vermillion left-edge bar on selected card. */}
            {checked && (
              <span
                aria-hidden
                className="pointer-events-none absolute inset-y-0 left-0 w-[3px] bg-[color:var(--vermillion)]"
              />
            )}
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
            {/* N1.i — 「AI 建议」 sparkle badge on the heuristic
                pre-pick. Shown regardless of whether the user has
                kept or overridden the suggestion, so the suggestion
                stays visible. */}
            {isSuggested && (
              <span
                aria-label="AI suggested"
                title="根据主题文字推测的默认选项 · 可以改"
                className="inline-flex items-center gap-1 border border-[color:var(--vermillion)]/30 bg-[color:var(--vermillion)]/8 px-1.5 py-[1px] font-mono-jb text-[9px] uppercase tracking-[0.18em] text-[color:var(--vermillion)]"
              >
                <Sparkles size={9} strokeWidth={2} />
                AI 建议
              </span>
            )}
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
        autoFocus
        className="w-full bg-transparent font-display text-[17px] leading-snug text-[color:var(--ink)] placeholder:font-display placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
      />
    </div>
  );
}

// ─── Breadcrumb subcomponent ──────────────────────────────────────────

function BreadcrumbTag({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-baseline gap-1.5 border border-[color:var(--ink)]/12 bg-white/40 px-2 py-[2px] normal-case">
      <span className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
        {label}
      </span>
      <span className="font-display text-[13px] italic text-[color:var(--ink-soft)]">
        {value}
      </span>
    </span>
  );
}
