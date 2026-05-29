"use client";

import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import {
  AlertTriangle,
  Film,
  Image as ImageIcon,
  Layers,
  Loader2,
  Paperclip,
  Pencil,
  RotateCcw,
  Send,
  Settings2,
  Sparkles,
  X,
} from "lucide-react";
import clsx from "clsx";

import type { DesignIntent, NanoBananaModel } from "@/lib/api";
import { routeDesignChat } from "@/lib/api";
import { applySkillScaffold, getSkill, type Skill } from "./skills";
import { SkillsLibrary } from "./SkillsLibrary";
import type { HistoryEntry } from "./page";
import type { SelectedImageInfo } from "./TldrawCanvas";

// RightSideChat is the requirements-gathering chat. Mirrors the left
// ChatCopilot's "type and forget" feel but with two extras:
//
//   1. Settings row above the input — model (Fast / Pro), aspect,
//      and an explicit "use selection as reference" chip that lights
//      up when an AI image is selected on the canvas.
//
//   2. Per-message action chips on each successful generation:
//      "4 variants" fans out 4 parallel NanoBanana calls with the
//      same prompt; "Animate" focuses the image so the canvas's
//      SelectionToolbar pops up with Seedance i2v one click away.
//
// Single shared `history` array with the left ChatCopilot — both
// surfaces render the same generation log, just in different shapes.
// The right side adds the conversational chrome (assistant bubbles
// with hints, NO_IMAGE retry button, action chips).

export type Aspect = "1:1" | "16:9" | "9:16" | "4:3";

export type RightSideChatProps = {
  history: HistoryEntry[];
  /** The currently-selected AI image on canvas, or null. Drives the
   *  "use selection" chip in the settings row. */
  selected: SelectedImageInfo | null;
  /** Controlled ref-toggle state. Lifted to the page so the canvas
   *  SelectionToolbar can flip it too (via "+ ref" button) without
   *  needing internal access to RightSideChat. */
  useSelectionRef: boolean;
  setUseSelectionRef: (v: boolean) => void;
  /** Fire a single-image generation. Goes through the parent's
   *  useGenerateWithProgress so a placeholder appears on canvas.
   *  `skillId` records which scaffold (if any) the chat used so the
   *  result's history entry can show skill-specific follow-up chips. */
  onSubmit: (
    prompt: string,
    opts: {
      model: NanoBananaModel;
      aspect: Aspect;
      refs?: string[];
      skillId?: string;
    },
  ) => void;
  /** Fan out N variants on the canvas. Parent runs 4 parallel
   *  NanoBanana calls and drops the grid via placeVariants. */
  onVariants: (
    prompt: string,
    opts: { model: NanoBananaModel; aspect: Aspect; refs?: string[] },
  ) => void;
  /** Focus a previously-generated image on canvas (selects + zooms).
   *  Used by the message thumbnail click AND by the "Animate" chip
   *  (focusing the image causes the SelectionToolbar to pop up). */
  onFocus: (shapeId: string) => void;
  /** Re-place a hydrated history image onto the canvas. Called when
   *  the user clicks a chat thumbnail whose source shape no longer
   *  exists (server-hydrated rows have shapeId === undefined). */
  onReplace: (entry: HistoryEntry) => void;
  /** Handle a file upload from the paperclip button — the parent
   *  reads the file, gets natural dimensions, places it on canvas via
   *  controller.placeUploadedImage. Returns once the image is on the
   *  canvas (or rejects with a user-facing error). */
  onUpload: (file: File) => Promise<void>;
  /** Capture the current canvas sketch as a PNG and return a data
   *  URL the chat can append to its ref list. Rejects with a
   *  user-facing message when nothing is drawn yet. */
  onSketchAsRef: () => Promise<string>;
  /** Dispatch a routed intent. Called after `routeDesignChat` returns
   *  a structured `{tool, args}` decision; the parent maps each intent
   *  to its existing handler (generate / variants / reimagine / animate
   *  / quick_edit). Original message passed alongside for fallback
   *  prompts. `activeSkillId` carries the skill id (if any) so the
   *  dispatcher can apply skill-specific defaults. */
  onRoutedIntent: (
    intent: DesignIntent,
    originalMessage: string,
    opts: { model: NanoBananaModel; aspect: Aspect; refs?: string[] },
    activeSkillId: string | null,
  ) => void;
  /** Fire a skill-suggested follow-up against a completed history
   *  entry. Parent looks up the matching SkillFollowup recipe and
   *  dispatches the corresponding intent (variants / reimagine /
   *  animate / generate) with the entry's URL as a ref. */
  onFollowup: (entry: HistoryEntry, label: string) => void;
};

const ASPECT_HINT: Record<Aspect, string> = {
  "1:1": "square",
  "16:9": "wide",
  "9:16": "tall",
  "4:3": "classic",
};

// Width bounds in px. Below 300 the message bubbles get pinched; above
// 480 the chat reads like an essay sidebar instead of a paired panel.
const MIN_WIDTH = 300;
const MAX_WIDTH = 480;
const DEFAULT_WIDTH = 340;
const WIDTH_STORAGE_KEY = "dw.designRightWidth";

export function RightSideChat({
  history,
  selected,
  useSelectionRef,
  setUseSelectionRef,
  onSubmit,
  onVariants,
  onFocus,
  onReplace,
  onUpload,
  onSketchAsRef,
  onRoutedIntent,
  onFollowup,
}: RightSideChatProps) {
  const [open, setOpen] = useState(true);
  const [input, setInput] = useState("");
  const [model, setModel] = useState<NanoBananaModel>("nano-banana-2");
  const [aspect, setAspect] = useState<Aspect>("1:1");
  // activeSkillId tracks the user's current "design task scaffold". When
  // non-null, the chat prepends the skill's promptScaffold to the user's
  // input before sending, and routes through the skill-aware path so
  // the planner LLM sees the skill context.
  const [activeSkillId, setActiveSkillId] = useState<string | null>(null);
  const [width, setWidth] = useState<number>(DEFAULT_WIDTH);
  // pastedRefs accumulate URLs the user explicitly added via the
  // "Add as reference" chip (triggered by URL detection in input).
  // Cleared after each successful submit. Capped at 4 to match
  // NanoBanana's gateway limit.
  const [pastedRefs, setPastedRefs] = useState<string[]>([]);
  // notice: transient toast-style message shown above the input (e.g.
  // "Uploads can be displayed but not yet used as a reference").
  // Clears itself after 5s or on next user action.
  const [notice, setNotice] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Restore persisted width on mount. SSR-safe — we only read in an
  // effect, the SSR snapshot uses DEFAULT_WIDTH so no hydration mismatch.
  useEffect(() => {
    try {
      const raw = window.localStorage.getItem(WIDTH_STORAGE_KEY);
      if (!raw) return;
      const parsed = Number.parseInt(raw, 10);
      if (Number.isFinite(parsed)) {
        setWidth(Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, parsed)));
      }
    } catch {
      // localStorage can throw on private windows; default-width fallback is fine.
    }
  }, []);

  // Drag handle — captures the mousedown then listens at document
  // level so the drag works even when the cursor leaves the handle
  // (which happens immediately on any drag). The chat lives at the
  // RIGHT edge so dragging LEFT widens it; we invert the delta sign.
  function startResize(e: React.MouseEvent<HTMLDivElement>) {
    e.preventDefault();
    const startX = e.clientX;
    const startW = width;
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";

    function onMove(ev: MouseEvent) {
      const delta = startX - ev.clientX;
      const next = Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, startW + delta));
      setWidth(next);
    }
    function onUp(ev: MouseEvent) {
      const delta = startX - ev.clientX;
      const next = Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, startW + delta));
      try {
        window.localStorage.setItem(WIDTH_STORAGE_KEY, String(next));
      } catch {
        // Private windows / quota — drop the persistence silently.
      }
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
    }
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  }

  // Auto-scroll to the latest message on every history change. The
  // chat reads bottom-to-top (newest at the bottom) so the user's
  // eye stays where they last took action.
  useEffect(() => {
    if (!scrollRef.current) return;
    scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [history.length, history[0]?.status]);

  // Drop the "use selection" flag when the underlying selection goes
  // away (user deleted the shape, switched to a non-image, etc.) —
  // otherwise the chip stays armed but has no source to pull in.
  useEffect(() => {
    if (!selected && useSelectionRef) setUseSelectionRef(false);
  }, [selected, useSelectionRef]);

  // Detect when the textarea content is a single bare URL. We surface
  // a different action chip in that case ("Add as reference") because
  // the user almost certainly wants to use the URL as a ref input, not
  // generate the literal string. Allows http(s) only — file: / data:
  // URLs would fail at the upstream gateway anyway.
  const detectedURL = (() => {
    const trimmed = input.trim();
    if (!/^https?:\/\/\S+$/i.test(trimmed)) return null;
    if (trimmed.length > 2000) return null; // sane cap
    return trimmed;
  })();

  function buildRefs(): string[] | undefined {
    const refs: string[] = [];
    if (useSelectionRef && selected) refs.push(selected.url);
    for (const r of pastedRefs) if (!refs.includes(r)) refs.push(r);
    return refs.length > 0 ? refs.slice(0, 4) : undefined;
  }

  async function submit(e?: FormEvent) {
    e?.preventDefault();
    const trimmed = input.trim();
    if (!trimmed) return;

    const skill = getSkill(activeSkillId);
    // When a skill is active, prepend its scaffold so the model gets
    // the style anchor. We send the scaffolded prompt to the router
    // too so it sees the skill context when picking an intent.
    const scaffoldedPrompt = applySkillScaffold(skill, trimmed);
    const opts = {
      model,
      aspect,
      refs: buildRefs(),
      skillId: activeSkillId ?? undefined,
    };
    setInput("");
    setPastedRefs([]); // refs are one-shot — clear after send
    setNotice(null);

    // Smart-routing path: ask the orchestrator to classify the
    // message into a design tool intent (generate / reimagine /
    // animate / variants / quick_edit). Slash-prefixed messages
    // ("/generate ..." someday) and routing errors short-circuit
    // to the literal generate path so the chat never breaks.
    const skipRouting =
      trimmed.startsWith("/") ||
      // Without canvas selection + without recent prompts AND no
      // active skill, the only intent that ever makes sense is
      // generate — no point round-tripping to the LLM. Cuts
      // perceived latency on first prompt. When a skill is active
      // we ALWAYS route so the planner sees the skill context.
      (!selected && history.length === 0 && !skill);

    if (skipRouting) {
      onSubmit(
        scaffoldedPrompt.replace(/^\/(generate\s+)?/, ""),
        opts,
      );
      return;
    }

    try {
      const recent = history
        .slice(0, 3)
        .map((h) => h.prompt)
        .reverse();
      const intent = await routeDesignChat({
        message: trimmed,
        selected_url: selected?.url,
        recent_prompts: recent,
        active_skill_id: skill?.id,
        skill_hint: skill?.routerHint,
      });
      // Pass the SCAFFOLDED prompt as the original, so the
      // dispatcher uses the styled version when the router falls
      // back to literal generate (intent.tool === "generate").
      onRoutedIntent(intent, scaffoldedPrompt, opts, activeSkillId);
    } catch {
      // Router failure — drop to the trivial generate path so the
      // user's message still produces an image. Quietly: a passing
      // routing failure shouldn't show a scary error.
      onSubmit(scaffoldedPrompt, opts);
    }
  }

  // Pick a skill from the library popover. Side effects mirror what
  // the user expects when they click a tile: input pre-fills with the
  // example, model/aspect snap to the skill's defaults so the next
  // submit is one click away. We KEEP whatever was already typed if
  // the user has half a thought — append the example as a hint
  // instead of overwriting.
  function applySkill(skill: Skill) {
    setActiveSkillId(skill.id);
    setModel(skill.model);
    setAspect(skill.aspect);
    setNotice(null);
    if (!input.trim()) {
      setInput(skill.examplePrompt);
    }
  }

  function clearSkill() {
    setActiveSkillId(null);
    // Don't touch model/aspect — user might want to keep them after
    // exiting skill mode.
  }

  function addPastedRef(url: string) {
    setPastedRefs((prev) => {
      if (prev.includes(url)) return prev;
      if (prev.length >= 4) {
        setNotice("Max 4 reference images.");
        return prev;
      }
      return [...prev, url];
    });
    setInput("");
  }

  function removePastedRef(url: string) {
    setPastedRefs((prev) => prev.filter((u) => u !== url));
  }

  async function handleFiles(files: FileList | null) {
    if (!files || files.length === 0) return;
    setNotice(null);
    let placed = 0;
    let firstErr: string | null = null;
    for (const file of Array.from(files)) {
      if (!file.type.startsWith("image/")) continue;
      try {
        await onUpload(file);
        placed++;
      } catch (e) {
        if (!firstErr) firstErr = e instanceof Error ? e.message : String(e);
      }
    }
    if (placed > 0) {
      setNotice(
        `Placed ${placed} image${placed === 1 ? "" : "s"} on canvas. ` +
          "Select an upload + click Reimagine / Variants / Edit to use it as a reference.",
      );
    } else if (firstErr) {
      setNotice("Upload failed: " + firstErr);
    }
    // Auto-clear the notice after 6s so it doesn't linger.
    setTimeout(() => setNotice(null), 6000);
  }

  function onFilePicked(e: React.ChangeEvent<HTMLInputElement>) {
    void handleFiles(e.target.files);
    // Reset the input so re-picking the same file fires change again.
    e.target.value = "";
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    // Enter sends; Shift+Enter inserts a newline.
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  }

  function retry(entry: HistoryEntry) {
    // Used by the NO_IMAGE retry button — prepends a richness hint
    // to the original prompt so Gemini's "decided not to return an
    // image" path is less likely. Same settings as the failed attempt.
    const enriched = `Detailed, high quality. ${entry.prompt}`;
    onSubmit(enriched, { model, aspect, refs: buildRefs() });
  }

  function runVariants(entry: HistoryEntry) {
    onVariants(entry.prompt, { model, aspect, refs: buildRefs() });
  }

  if (!open) {
    // Closed state — a thin spine on the right edge so the user can
    // toggle the chat back in without losing canvas real estate.
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="dw-design-right flex w-10 shrink-0 items-center justify-center border-l border-zinc-200 bg-white text-zinc-500 transition hover:bg-zinc-50 hover:text-zinc-800"
        aria-label="Open design assistant"
      >
        <Sparkles size={14} className="text-fuchsia-500" />
      </button>
    );
  }

  return (
    <aside
      className="dw-design-right relative flex shrink-0 flex-col border-l border-zinc-200 bg-white"
      style={{ width: `${width}px` }}
    >
      {/* Drag handle — 4px wide hit area along the left edge so it
          covers comfortably for the eye. Cursor + faint hover stripe
          give the affordance. */}
      <div
        onMouseDown={startResize}
        title="Drag to resize"
        aria-label="Resize design assistant"
        role="separator"
        className="absolute left-0 top-0 z-10 h-full w-1 cursor-col-resize bg-transparent transition hover:bg-fuchsia-200/60"
      />
      <header className="flex items-baseline justify-between border-b border-zinc-100 px-4 py-3">
        <h2 className="inline-flex items-baseline gap-1.5 text-[13px] font-medium text-zinc-900">
          <Sparkles size={12} className="self-center text-fuchsia-500" />
          Design assistant
          <span className="ml-1 font-mono text-[9px] uppercase tracking-[0.14em] text-zinc-400">
            NanoBanana
          </span>
        </h2>
        <button
          type="button"
          onClick={() => setOpen(false)}
          className="text-zinc-400 hover:text-zinc-700"
          aria-label="Close design assistant"
        >
          <X size={14} />
        </button>
      </header>

      {/* Message list. Renders the shared history newest-at-bottom so
          the conversational read order matches typing direction. */}
      <div ref={scrollRef} className="flex-1 space-y-3 overflow-y-auto px-3 py-3">
        {history.length === 0 ? (
          <EmptyState onSuggest={(s) => setInput(s)} />
        ) : (
          history
            .slice()
            .reverse()
            .map((entry) => (
              <ChatTurn
                key={entry.id}
                entry={entry}
                onFocus={onFocus}
                onReplace={onReplace}
                onRetry={() => retry(entry)}
                onVariants={() => runVariants(entry)}
                onFollowup={onFollowup}
              />
            ))
        )}
      </div>

      {/* Settings + input — pinned at bottom; settings row sits ABOVE
          the input so the controls are visible while typing. */}
      <form
        onSubmit={submit}
        className="space-y-2 border-t border-zinc-100 p-3"
      >
        <div className="flex flex-wrap items-center gap-1.5">
          <SettingsRow
            model={model}
            setModel={setModel}
            aspect={aspect}
            setAspect={setAspect}
            selectionAvailable={selected != null}
            useSelectionRef={useSelectionRef}
            setUseSelectionRef={setUseSelectionRef}
          />
          <SkillsLibrary
            activeSkillId={activeSkillId}
            onPick={applySkill}
            onClear={clearSkill}
          />
        </div>

        {/* Pasted-ref chips. Shown when the user added URLs via the
            "Add as reference" detection. Stay visible across multiple
            input edits until the next submit (where they're consumed). */}
        {pastedRefs.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {pastedRefs.map((url) => (
              <span
                key={url}
                className="inline-flex items-center gap-1 rounded border border-fuchsia-200 bg-fuchsia-50 px-1.5 py-0.5 font-mono text-[10px] text-fuchsia-700"
                title={url}
              >
                <ImageIcon size={9} />
                ref
                <button
                  type="button"
                  onClick={() => removePastedRef(url)}
                  className="text-fuchsia-500 hover:text-fuchsia-700"
                  aria-label="Remove reference"
                >
                  <X size={9} />
                </button>
              </span>
            ))}
          </div>
        )}

        {/* Transient notice — upload feedback or "max refs reached".
            Auto-dismisses after 6s. Sits above the textarea so eyes
            land on it before typing. */}
        {notice && (
          <div className="rounded border border-zinc-200 bg-zinc-50 px-2 py-1.5 text-[11px] leading-snug text-zinc-600">
            {notice}
          </div>
        )}

        <textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder={
            selected && useSelectionRef
              ? "Describe how to riff on the selected image…"
              : "What would you like to create? (style · mood · subject)"
          }
          rows={3}
          spellCheck={false}
          className="w-full resize-none rounded-md border border-zinc-200 px-2.5 py-2 text-[13px] focus:border-zinc-400 focus:outline-none"
        />

        {/* Hidden file input — opens via the paperclip button. accept
            filters to images so the picker is fast. multiple allows
            batch upload (paperclip → ⌘-click several files at once). */}
        <input
          ref={fileInputRef}
          type="file"
          accept="image/*"
          multiple
          onChange={onFilePicked}
          className="hidden"
          aria-hidden
        />

        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              title="Upload image(s) to the canvas — select then click Reimagine to use as a reference"
              aria-label="Upload images"
              className="inline-flex h-7 w-7 items-center justify-center rounded text-zinc-500 transition hover:bg-zinc-100 hover:text-zinc-800"
            >
              <Paperclip size={13} />
            </button>
            {/* Sketch-to-image — captures the user's drawn shapes off
                the canvas and adds them as a reference for the next
                prompt. The button is always clickable; if nothing is
                drawn yet, onSketchAsRef rejects with a clear message
                that we surface in the notice line. */}
            <button
              type="button"
              onClick={async () => {
                setNotice(null);
                try {
                  const dataURL = await onSketchAsRef();
                  addPastedRef(dataURL);
                  setNotice("Sketch added as reference for your next prompt.");
                  setTimeout(() => setNotice(null), 4000);
                } catch (e) {
                  setNotice(e instanceof Error ? e.message : String(e));
                  setTimeout(() => setNotice(null), 6000);
                }
              }}
              title="Capture your TLDraw strokes as a reference image"
              aria-label="Use sketch as reference"
              className="inline-flex h-7 w-7 items-center justify-center rounded text-zinc-500 transition hover:bg-zinc-100 hover:text-zinc-800"
            >
              <Pencil size={13} />
            </button>
            <span className="text-[10px] text-zinc-400">
              Enter sends · Shift+Enter newline
            </span>
          </div>

          {/* Submit button — swaps to "Add as reference" when the user
              has pasted what looks like a URL. Clicking that path adds
              the URL to pastedRefs and clears the input so they can
              type a real prompt next. */}
          {detectedURL ? (
            <button
              type="button"
              onClick={() => addPastedRef(detectedURL)}
              title="Use this URL as a reference image with your next prompt"
              className="inline-flex items-center gap-1.5 rounded-md bg-fuchsia-600 px-2.5 py-1.5 text-[12px] font-medium text-white hover:bg-fuchsia-700"
            >
              <ImageIcon size={12} />
              Add as reference
            </button>
          ) : (
            <button
              type="submit"
              disabled={!input.trim()}
              className="inline-flex items-center gap-1.5 rounded-md bg-zinc-900 px-2.5 py-1.5 text-[12px] font-medium text-white disabled:opacity-50"
            >
              <Send size={12} />
              Generate
            </button>
          )}
        </div>
      </form>
    </aside>
  );
}

// ─── Pieces ───────────────────────────────────────────────────────────

function EmptyState({ onSuggest }: { onSuggest: (s: string) => void }) {
  // Suggestion chips for first-time users — clicking populates the
  // input so the user can edit before sending instead of firing
  // immediately. Less surprising than auto-submit, and lets people
  // see what a "good" prompt looks like.
  const suggestions = [
    "A minimalist 3D icon of a coffee cup, soft pastel palette",
    "An editorial photo of a misty mountain at dawn, cinematic",
    "A cute watercolor cat sleeping on a windowsill at sunset",
    "A bold flat illustration of a rocket launching into space",
  ];
  return (
    <div className="space-y-3 px-1 py-6 text-center">
      <p className="text-[13px] text-zinc-600">
        Tell me what you&apos;d like to create.
      </p>
      <p className="text-[11px] text-zinc-400">
        Or pick a starter prompt:
      </p>
      <div className="flex flex-col gap-1.5">
        {suggestions.map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => onSuggest(s)}
            className="rounded border border-zinc-200 px-2 py-1.5 text-left text-[11px] leading-snug text-zinc-700 transition hover:border-zinc-400 hover:bg-zinc-50"
          >
            {s}
          </button>
        ))}
      </div>
    </div>
  );
}

/**
 * Settings row — main bar shows a single gear button (with the active
 * model + aspect rendered as compact label text next to it) plus the
 * `ref` chip. Clicking the gear opens a popover with the full toggles.
 *
 * Rationale for the collapse: narrow widths (the chat is resizable down
 * to 300px) made the previous flat row wrap onto two lines. The user
 * also rarely flips model/aspect mid-session — they're "set once"
 * controls, not "every prompt" ones. The `ref` chip is fundamentally
 * different — it depends on canvas selection and changes per-prompt —
 * so it stays in the main row where the eye expects it.
 */
function SettingsRow({
  model,
  setModel,
  aspect,
  setAspect,
  selectionAvailable,
  useSelectionRef,
  setUseSelectionRef,
}: {
  model: NanoBananaModel;
  setModel: (v: NanoBananaModel) => void;
  aspect: Aspect;
  setAspect: (v: Aspect) => void;
  selectionAvailable: boolean;
  useSelectionRef: boolean;
  setUseSelectionRef: (v: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  // Close on outside click — popover semantics. We attach at document
  // level and tear down when the popover closes so we don't keep an
  // idle listener around in the common closed state.
  useEffect(() => {
    if (!open) return;
    function onDocClick(ev: MouseEvent) {
      if (!containerRef.current) return;
      if (containerRef.current.contains(ev.target as Node)) return;
      setOpen(false);
    }
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [open]);

  const modelLabel = model === "nano-banana-2" ? "Fast" : "Pro";

  return (
    <div ref={containerRef} className="relative flex flex-wrap items-center gap-1.5">
      {/* Gear button with active settings as inline label — one click
          to open, current state legible at a glance even when closed. */}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        title="Change model + aspect"
        className={clsx(
          "inline-flex items-center gap-1 rounded border px-1.5 py-0.5 font-mono text-[9px] uppercase tracking-[0.12em] transition",
          open
            ? "border-zinc-400 bg-zinc-100 text-zinc-900"
            : "border-zinc-200 text-zinc-600 hover:border-zinc-300 hover:text-zinc-900",
        )}
      >
        <Settings2 size={10} />
        <span>
          {modelLabel} · {aspect}
        </span>
      </button>

      {/* Use-selection chip stays in the main row — it's prompt-scoped,
          not session-scoped, so the user wants it one click away. */}
      <button
        type="button"
        disabled={!selectionAvailable}
        onClick={() => setUseSelectionRef(!useSelectionRef)}
        title={
          selectionAvailable
            ? "Toggle: send the selected canvas image as a reference"
            : "Select an AI image on the canvas to enable"
        }
        className={clsx(
          "inline-flex items-center gap-1 rounded border px-1.5 py-0.5 font-mono text-[9px] uppercase tracking-[0.12em] transition",
          useSelectionRef && selectionAvailable
            ? "border-fuchsia-500 bg-fuchsia-500 text-white"
            : "border-zinc-200 text-zinc-500 enabled:hover:text-zinc-800 disabled:opacity-40",
        )}
      >
        <ImageIcon size={9} />
        ref
      </button>

      {/* Popover — anchored top-left so it grows upward (the settings
          row lives at the BOTTOM of the chat, so opening UP keeps the
          popover from clipping below the viewport). */}
      {open && (
        <div className="absolute bottom-full left-0 z-30 mb-1 w-60 rounded-md border border-zinc-200 bg-white p-2.5 shadow-md">
          <p className="mb-1 font-mono text-[10px] uppercase tracking-[0.14em] text-zinc-400">
            Quality
          </p>
          <div className="mb-2 inline-flex rounded border border-zinc-200 p-0.5">
            {(["nano-banana-2", "nano-banana-pro"] as const).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => setModel(m)}
                title={
                  m === "nano-banana-2" ? "Gemini 3.1 Flash" : "Gemini 3 Pro"
                }
                className={clsx(
                  "rounded px-2 py-0.5 font-mono text-[10px] uppercase tracking-[0.12em] transition",
                  model === m
                    ? "bg-zinc-800 text-white"
                    : "text-zinc-500 hover:text-zinc-800",
                )}
              >
                {m === "nano-banana-2" ? "Fast" : "Pro"}
              </button>
            ))}
          </div>

          <p className="mb-1 font-mono text-[10px] uppercase tracking-[0.14em] text-zinc-400">
            Aspect
          </p>
          <div className="inline-flex rounded border border-zinc-200 p-0.5">
            {(["1:1", "16:9", "9:16", "4:3"] as const).map((a) => (
              <button
                key={a}
                type="button"
                onClick={() => setAspect(a)}
                title={ASPECT_HINT[a]}
                className={clsx(
                  "rounded px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-[0.12em] transition",
                  aspect === a
                    ? "bg-zinc-800 text-white"
                    : "text-zinc-500 hover:text-zinc-800",
                )}
              >
                {a}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function ChatTurn({
  entry,
  onFocus,
  onReplace,
  onRetry,
  onVariants,
  onFollowup,
}: {
  entry: HistoryEntry;
  onFocus: (shapeId: string) => void;
  /** Called when the user clicks a hydrated thumbnail (shapeId is
   *  undefined because the source shape no longer exists on canvas).
   *  Parent re-places the image at viewport centre. */
  onReplace: (entry: HistoryEntry) => void;
  onRetry: () => void;
  onVariants: () => void;
  /** Fire a skill-suggested follow-up action against this entry. The
   *  parent reads `entry.skillId`, looks up the followup recipe, and
   *  dispatches as variants / reimagine / animate / generate using
   *  the entry's image URL as the reference. */
  onFollowup: (entry: HistoryEntry, label: string) => void;
}) {
  // Each history entry renders as TWO bubbles — the user's prompt on
  // the right, the assistant's response (spinner / thumbnail / error)
  // on the left. Tight visual link keeps the call-and-response read
  // even when several turns are stacked.
  const isError = entry.status === "error";
  const isRunning = entry.status === "running";
  const isDone = entry.status === "done";
  const noImage =
    isError && (entry.errorMessage ?? "").toUpperCase().includes("NO_IMAGE");

  return (
    <div className="space-y-1.5">
      {/* User bubble — right-aligned, zinc-tinted. */}
      <div className="flex justify-end">
        <div className="max-w-[80%] rounded-lg rounded-br-sm bg-zinc-100 px-2.5 py-1.5 text-[12px] leading-snug text-zinc-800">
          {entry.prompt}
        </div>
      </div>

      {/* Assistant bubble — left-aligned, branded with the fuchsia dot. */}
      <div className="flex items-start gap-1.5">
        <div className="mt-1 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-fuchsia-50">
          <Sparkles size={9} className="text-fuchsia-500" />
        </div>
        <div className="max-w-[85%] flex-1 space-y-1.5">
          {isRunning && (
            <div className="inline-flex items-center gap-1.5 rounded-lg rounded-bl-sm border border-zinc-200 bg-white px-2.5 py-1.5 text-[12px] text-zinc-600">
              <Loader2 size={11} className="animate-spin" />
              Generating on the canvas…
            </div>
          )}

          {isDone && entry.thumbnailUrl && (
            <>
              <button
                type="button"
                onClick={() => {
                  if (entry.shapeId) onFocus(entry.shapeId);
                  else onReplace(entry);
                }}
                className="block overflow-hidden rounded-lg border border-zinc-200 transition hover:border-zinc-400"
                title={
                  entry.shapeId
                    ? "Click to focus on canvas"
                    : "Restore this image to the canvas"
                }
              >
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={entry.thumbnailUrl}
                  alt={entry.prompt}
                  className="h-32 w-full object-cover"
                />
              </button>
              <ChatTurnFollowups
                entry={entry}
                onVariants={onVariants}
                onFollowup={onFollowup}
                onFocus={onFocus}
                onReplace={onReplace}
              />
            </>
          )}

          {isError && (
            <div className="rounded-lg rounded-bl-sm border border-red-200 bg-red-50 px-2.5 py-1.5 text-[12px] text-red-700">
              <div className="inline-flex items-baseline gap-1">
                <AlertTriangle size={11} className="self-center" />
                <span className="break-words">
                  {prettyErr(entry.errorMessage)}
                </span>
              </div>
              {noImage && (
                <button
                  type="button"
                  onClick={onRetry}
                  className="mt-1.5 inline-flex items-center gap-1 rounded border border-red-300 bg-white px-1.5 py-0.5 text-[11px] text-red-700 hover:bg-red-100"
                  title="Re-fire with a richness hint prepended to the prompt"
                >
                  <RotateCcw size={9} />
                  Retry with detail
                </button>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

/**
 * ChatTurnFollowups renders the chip row beneath a successful entry.
 *
 * When the entry came from a SKILL (entry.skillId set), we render
 * up to 3 chips derived from the skill's `followups` recipe — these
 * are the "you just made a logo, here are the natural next moves"
 * suggestions. Each chip fires onFollowup with the label; the parent
 * looks up the recipe and dispatches.
 *
 * When the entry has no skill (free-form generation or server-
 * hydrated row), we fall back to the original two chips ("4 variants"
 * + "Animate"). Hydrated rows (no shapeId) need a "Restore" detour
 * for Animate since Seedance needs a canvas shape next to which the
 * video lands.
 */
function ChatTurnFollowups({
  entry,
  onVariants,
  onFollowup,
  onFocus,
  onReplace,
}: {
  entry: HistoryEntry;
  onVariants: () => void;
  onFollowup: (entry: HistoryEntry, label: string) => void;
  onFocus: (shapeId: string) => void;
  onReplace: (entry: HistoryEntry) => void;
}) {
  const skill = getSkill(entry.skillId);
  if (skill) {
    return (
      <div className="flex flex-wrap gap-1">
        {skill.followups.slice(0, 3).map((f) => (
          <ActionChip
            key={f.label}
            icon={<Sparkles size={10} className="text-fuchsia-500" />}
            label={f.label}
            onClick={() => onFollowup(entry, f.label)}
            title={`${f.intent}: ${f.prompt}`}
          />
        ))}
      </div>
    );
  }
  // Free-form / hydrated — keep the legacy two chips.
  return (
    <div className="flex flex-wrap gap-1">
      <ActionChip
        icon={<Layers size={10} />}
        label="4 variants"
        onClick={onVariants}
        title="Generate 4 new images from the same prompt"
      />
      {entry.shapeId ? (
        <ActionChip
          icon={<Film size={10} />}
          label="Animate"
          onClick={() => onFocus(entry.shapeId!)}
          title="Focus the image, then use the canvas Animate button"
        />
      ) : (
        <ActionChip
          icon={<Film size={10} />}
          label="Restore + Animate"
          onClick={() => onReplace(entry)}
          title="Restore this image to the canvas, then use the canvas Animate button"
        />
      )}
    </div>
  );
}

function ActionChip({
  icon,
  label,
  onClick,
  title,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  title?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      className="inline-flex items-center gap-1 rounded border border-zinc-200 bg-white px-1.5 py-0.5 font-mono text-[9px] uppercase tracking-[0.12em] text-zinc-600 transition hover:border-zinc-400 hover:text-zinc-900"
    >
      {icon}
      {label}
    </button>
  );
}

function prettyErr(msg: string | undefined): string {
  if (!msg) return "Something went wrong.";
  // Friendly translation for the most common upstream failure.
  if (msg.toUpperCase().includes("NO_IMAGE")) {
    return "Gemini decided not to return an image — try a richer prompt or click Retry.";
  }
  // Strip the leading "dreamapi-sidecar: " prefix so the message
  // reads naturally inside the bubble.
  return msg.replace(/^dreamapi-sidecar:\s*/, "");
}
