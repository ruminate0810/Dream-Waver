"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { gsap } from "gsap";
import { useGSAP } from "@gsap/react";
import {
  ArrowLeft,
  ArrowRight,
  Download,
  Film,
  Layers,
  Link as LinkIcon,
  Loader2,
  Repeat,
  Sparkles,
  Undo2,
  Redo2,
  Wand2,
  X,
} from "lucide-react";

import {
  ApiError,
  generateDesignNanoBanana,
  listDesignAssets,
  seedanceI2V,
  type DesignAsset,
  type DesignIntent,
  type NanoBananaModel,
  type SeedanceResolution,
} from "@/lib/api";
import { RightSideChat, type Aspect } from "./RightSideChat";
import { getSkill } from "./skills";
import type {
  CanvasController,
  CanvasHistoryFlags,
  SelectedImageInfo,
} from "./TldrawCanvas";
import { DUR, EASE, PREFERS_FULL_MOTION } from "@/lib/motion";

gsap.registerPlugin(useGSAP);

// /design is the canvas surface — TLDraw as the artboard, one
// persistent right sidebar and one floating toolbar on top:
//
//   Design assistant (right)       conversational requirements chat —
//                                 settings row (model / aspect /
//                                 use-selection-as-ref), input, per-
//                                 message action chips (variants /
//                                 animate / NO_IMAGE retry). Also
//                                 carries the generation history.
//   Selection toolbar (top-center) acts on the currently-selected AI
//                                 image: Reimagine (NanoBanana w/ ref)
//                                 / Animate (Seedance i2v → MP4).
//                                 Hidden when nothing's selected.
//
// Image flow: synchronous NanoBanana call wrapped in a synthetic
// "Generating · Ns" placeholder shape — the placeholder lands on the
// canvas immediately, the caption ticks every 2s, and the real image
// replaces it on completion (typical wait 15-30s).
//
// SSR note: TLDraw assumes a browser — it pokes at `window` during
// import. We lazy-load via next/dynamic with ssr:false; the page
// shell renders during SSR so the user sees chrome before TLDraw boots.

const TldrawClient = dynamic(
  () => import("./TldrawCanvas").then((m) => m.TldrawCanvas),
  {
    ssr: false,
    loading: () => (
      <div className="flex h-full w-full items-center justify-center text-sm text-zinc-400">
        Loading canvas…
      </div>
    ),
  },
);

/** One row in the generation history. Stored at the page level so it
 *  outlives the chat panel's open/close state (closing the right side
 *  preserves prior generations on the next reopen). */
export type HistoryEntry = {
  id: string;
  prompt: string;
  thumbnailUrl: string;
  /** The image shape this generation produced. Used by the history
   *  click handler to focusShape on the canvas. */
  shapeId?: string;
  status: "running" | "done" | "error";
  errorMessage?: string;
  createdAt: number;
  /** Skill id that scaffolded this generation (when one was active).
   *  Drives the skill-derived follow-up chips shown beneath the
   *  thumbnail in the chat. Server-hydrated entries leave this
   *  unset — those fall back to the default 4-variants/Animate chips. */
  skillId?: string;
};

export default function DesignPage() {
  const controllerRef = useRef<CanvasController | null>(null);
  const [selected, setSelected] = useState<SelectedImageInfo | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  // hydrated tracks whether the server-side history load has finished
  // so the chat doesn't show the "empty state" suggestions while we're
  // still loading. Falls open (true) on error so the UI doesn't get
  // stuck loading forever.
  const [hydrated, setHydrated] = useState(false);
  // canUndo/canRedo mirror TLDraw's internal history stack. Surfaced
  // up here so the header's Undo/Redo buttons can show disabled state
  // — without it the buttons would always look enabled even on an
  // empty canvas.
  const [historyFlags, setHistoryFlags] = useState<CanvasHistoryFlags>({
    canUndo: false,
    canRedo: false,
  });
  // useSelectionRef is the "send the currently-selected canvas image
  // as a reference with the next chat prompt" toggle. Lifted out of
  // RightSideChat so the canvas SelectionToolbar can flip it too —
  // both surfaces show the user the same state.
  const [useSelectionRef, setUseSelectionRef] = useState(false);
  const mainRef = useRef<HTMLElement | null>(null);

  const handleReady = useCallback((controller: CanvasController) => {
    controllerRef.current = controller;
  }, []);

  const startGeneration = useGenerateWithProgress(controllerRef, setHistory);

  // Hydrate history from the server on mount. The server returns
  // workspace-scoped design_assets newest-first; we map each row to a
  // HistoryEntry with shapeId: undefined so the chat knows the source
  // shape is no longer on the canvas (the click handler re-places it
  // at viewport centre instead of "focus existing").
  //
  // Failures are silent — the chat just starts empty rather than
  // blocking the page. The empty state shows starter prompts so
  // users aren't blocked even if the API is down.
  useEffect(() => {
    let cancelled = false;
    listDesignAssets(100)
      .then((assets) => {
        if (cancelled) return;
        const entries: HistoryEntry[] = assets.map(toHistoryEntry);
        setHistory(entries);
        setHydrated(true);
      })
      .catch(() => {
        if (cancelled) return;
        // Network / 500 — fall through to empty state.
        setHydrated(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Chrome-only entrance. Header settles from above, the design
  // assistant slides in from the right edge. Deliberately NOT
  // animating TldrawCanvas — tldraw owns its own pointer model, and
  // a stale GSAP transform on the canvas wrapper would fight selection
  // drags.
  useGSAP(
    () => {
      const mm = gsap.matchMedia();
      mm.add(PREFERS_FULL_MOTION, () => {
        const tl = gsap.timeline({
          defaults: { ease: EASE.entrance, duration: DUR.entrance },
        });
        tl.from(".dw-design-header", {
          y: -8,
          opacity: 0,
          duration: DUR.secondary,
        }).from(".dw-design-right", { x: 24, opacity: 0 }, "-=0.4");
      });
      mm.add("(prefers-reduced-motion: reduce)", () => {
        gsap.from(".dw-design-header, .dw-design-right", {
          opacity: 0,
          duration: 0.2,
          stagger: 0.04,
        });
      });
    },
    { scope: mainRef },
  );

  // Selection toolbar appears mid-session when a user clicks an AI image.
  // Scale-from-0.94 + fade so it lands like an answer to the click,
  // not a sudden overlay. Effect-driven (not part of mount timeline)
  // because `selected` flips during use, not on first paint.
  useEffect(() => {
    if (!selected) return;
    const card = mainRef.current?.querySelector(".dw-design-toolbar");
    if (!card) return;
    const mm = gsap.matchMedia();
    mm.add(PREFERS_FULL_MOTION, () => {
      gsap.from(card, {
        scale: 0.94,
        opacity: 0,
        duration: DUR.micro,
        ease: EASE.feedback,
      });
    });
    return () => {
      mm.revert();
    };
  }, [selected]);

  return (
    <main
      ref={mainRef}
      className="flex h-screen flex-col bg-zinc-50 text-zinc-900"
    >
      <header className="dw-design-header z-10 flex items-center justify-between border-b border-zinc-100 bg-white px-6 py-2">
        <Link
          href="/"
          className="inline-flex items-baseline gap-2 text-xs text-zinc-500 hover:text-zinc-800"
        >
          <ArrowLeft size={12} /> Dream-Waver
        </Link>

        {/* Centre group — undo / redo. Lives on the controller because
            TLDraw owns the history stack; pressing these is identical
            to the user hitting Cmd-Z / Cmd-Shift-Z in the canvas. */}
        <div className="inline-flex items-center gap-0.5">
          <HeaderIconButton
            icon={<Undo2 size={14} />}
            label="Undo"
            disabled={!historyFlags.canUndo}
            onClick={() => controllerRef.current?.undo()}
          />
          <HeaderIconButton
            icon={<Redo2 size={14} />}
            label="Redo"
            disabled={!historyFlags.canRedo}
            onClick={() => controllerRef.current?.redo()}
          />
          <div className="mx-1 h-4 w-px bg-zinc-200" />
          <ExportMenu controllerRef={controllerRef} />
        </div>

        <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-400">
          Design · Canvas
        </span>
      </header>

      <div className="relative flex flex-1 overflow-hidden">
        <div className="relative flex-1 overflow-hidden">
          <TldrawClient
            onReady={handleReady}
            onSelectionChange={setSelected}
            onHistoryFlagsChange={setHistoryFlags}
          />

          {/* Empty-canvas hint — fades out once the first generation
              lands in history. pointer-events-none so it doesn't
              swallow TLDraw clicks. The right-pointing arrow nudges
              eyes toward the Design assistant input. */}
          {history.length === 0 && <EmptyCanvasHint />}

          {/* Selection toolbar — anchored ABOVE the selected shape's
              screen-space top edge, centered horizontally. Follows pan
              / zoom / drag because the store-listener feeds fresh
              screen coords on every TLDraw mutation. */}
          {selected && (
            <FloatingSelectionToolbar selected={selected}>
              <SelectionToolbar
                selected={selected}
                history={history}
                onResult={(url, w, h) =>
                  controllerRef.current?.placeNextToSelection(url, w, h)
                }
                onVideoResult={(videoURL) =>
                  controllerRef.current?.placeVideoNextToSelection(videoURL)
                }
                isRefActive={useSelectionRef}
                onToggleRef={() => setUseSelectionRef(!useSelectionRef)}
              />
            </FloatingSelectionToolbar>
          )}
        </div>

        {/* Right side: conversational requirements chat. Persistent
            sidebar (mirrors the left ChatCopilot's structure) so the
            user can type without floating-panel hunt. Same `history`
            both panels render, just in different shapes. */}
        <RightSideChat
          history={history}
          selected={selected}
          useSelectionRef={useSelectionRef}
          setUseSelectionRef={setUseSelectionRef}
          onSubmit={(prompt, opts) =>
            startGeneration({
              prompt,
              width: aspectW(opts.aspect),
              height: aspectH(opts.aspect),
              model: opts.model,
              aspect_ratio: opts.aspect,
              refs: opts.refs,
              skillId: opts.skillId,
            })
          }
          onVariants={(prompt, opts) =>
            runVariants(controllerRef, prompt, opts)
          }
          onFocus={(shapeId) => controllerRef.current?.focusShape(shapeId)}
          onSketchAsRef={() => sketchToDataURLRef(controllerRef)}
          onRoutedIntent={(intent, originalMessage, opts, activeSkillId) => {
            dispatchIntent({
              intent,
              originalMessage,
              opts,
              activeSkillId,
              selected,
              controllerRef,
              startGeneration,
            });
          }}
          onFollowup={(entry, label) =>
            dispatchSkillFollowup({
              entry,
              label,
              controllerRef,
              startGeneration,
            })
          }
          onReplace={(entry) => {
            // Hydrated history rows have no live shape — drop a fresh
            // image at the viewport centre using the asset URL.
            // Dimensions come from the entry; we don't have natural
            // pixel size for hydrated rows so default to 1024 if the
            // entry didn't carry them either (entry is constructed from
            // DesignAsset which has width/height columns).
            const url = entry.thumbnailUrl;
            if (!url) return;
            controllerRef.current?.placeImage(url, 1024, 1024);
          }}
          onUpload={(file) => uploadToCanvas(controllerRef, file)}
        />
      </div>
    </main>
  );
}

// ─── Generation hook (NanoBanana, synchronous w/ placeholder) ──────────

type GenerateRequest = {
  prompt: string;
  width: number;
  height: number;
  /** Optional reference URLs forwarded to NanoBanana (max 4). When the
   *  caller passes none, generation is text-only. */
  refs?: string[];
  /** Defaults to nano-banana-2 (Gemini Flash). Caller can override to
   *  nano-banana-pro for higher fidelity at ~2.5× cost. */
  model?: NanoBananaModel;
  /** "1:1" | "16:9" | "9:16" | "4:3" — forwarded but currently
   *  unused by the gateway (kept for forward-compat). */
  aspect_ratio?: string;
  /** Skill id that scaffolded this generation. Recorded on the
   *  resulting HistoryEntry so the chat can show skill-specific
   *  follow-up chips. */
  skillId?: string;
};

/**
 * useGenerateWithProgress drives the canvas image-generation flow on
 * top of the synchronous NanoBanana endpoint:
 *
 *   1. Place a dashed placeholder shape on the canvas immediately
 *   2. POST /api/v1/design/images/nano_banana — typically resolves
 *      in 15-30s
 *   3. Tick the placeholder's "Generating · Ns" caption every 2s
 *      while we wait (synthetic — no SSE upstream)
 *   4. On success: swap the placeholder for the real image
 *   5. On failure: turn the placeholder red with the error message
 *
 * Returns a `start` function the panels call. Each call is independent
 * — fire several concurrently and each gets its own placeholder.
 */
function useGenerateWithProgress(
  controllerRef: React.RefObject<CanvasController | null>,
  setHistory: React.Dispatch<React.SetStateAction<HistoryEntry[]>>,
): (req: GenerateRequest) => Promise<void> {
  return useCallback(
    async (req: GenerateRequest) => {
      const ctrl = controllerRef.current;
      if (!ctrl) return;

      // Placeholder shape goes down FIRST — gives the user immediate
      // feedback that the click registered. Caption ticks via the
      // ticker below while we await NanoBanana.
      const placeholderId = ctrl.placePlaceholder(req.prompt, req.width, req.height);
      const historyId = crypto.randomUUID();
      setHistory((prev) => [
        {
          id: historyId,
          prompt: req.prompt,
          thumbnailUrl: "",
          shapeId: placeholderId,
          status: "running",
          createdAt: Date.now(),
          skillId: req.skillId,
        },
        ...prev,
      ]);

      // Synthetic progress — Gemini is sync, so we just tick a counter
      // every 2s and update the caption. Stops on success / failure.
      let elapsed = 0;
      const ticker = setInterval(() => {
        elapsed += 2;
        ctrl.updatePlaceholderProgress(placeholderId, elapsed, "running");
      }, 2000);

      try {
        const resp = await generateDesignNanoBanana({
          prompt: req.prompt,
          model: req.model,
          images: req.refs && req.refs.length > 0 ? req.refs : undefined,
          aspect_ratio: req.aspect_ratio,
        });
        clearInterval(ticker);
        // NanoBanana doesn't echo dimensions — fall back to the
        // requested width/height. The asset record carries this; the
        // browser's <img> reads the real pixel size on load anyway.
        const w = resp.width > 0 ? resp.width : req.width;
        const h = resp.height > 0 ? resp.height : req.height;
        ctrl.replacePlaceholderWithImage(placeholderId, resp.url, w, h);
        setHistory((prev) =>
          prev.map((h) =>
            h.id === historyId
              ? { ...h, status: "done", thumbnailUrl: resp.url }
              : h,
          ),
        );
      } catch (e) {
        clearInterval(ticker);
        const msg =
          e instanceof ApiError ? e.message : String((e as Error).message ?? e);
        ctrl.failPlaceholder(placeholderId, msg);
        setHistory((prev) =>
          prev.map((h) =>
            h.id === historyId
              ? { ...h, status: "error", errorMessage: msg }
              : h,
          ),
        );
      }
    },
    [controllerRef, setHistory],
  );
}

// ─── Selection toolbar ────────────────────────────────────────────────
//
// Acts on the currently-selected AI image. Six actions, grouped:
//
//   Generate group: Reimagine (prompt + ref) · Variants (4 refs of this)
//                   · Edit ▾ (preset prompts)
//   Output group:   Animate (Seedance i2v) · Download · Copy URL
//   Cross-tool:     + ref (toggle chat's ref state for next prompt)
//
// Each action uses the selected image as a reference (where applicable)
// so the user can iterate on a single shape without leaving the canvas.

type SelectionOp =
  | "reimagine"
  | "animate"
  | "variants"
  | "edit"
  | "download";

/** Preset prompts for the Edit ▾ dropdown. Each one wraps NanoBanana
 *  Pro with the selected image as a single reference. Curated for
 *  high-signal one-click transforms — every entry produces a visibly
 *  different output without further prompt engineering. */
const QUICK_EDITS: Array<{ label: string; prompt: string }> = [
  { label: "Minimalize", prompt: "Strip down to a minimalist version — fewer details, cleaner shapes, muted palette" },
  { label: "Cartoonize", prompt: "Re-render as a vibrant flat-style cartoon illustration" },
  { label: "Photoreal", prompt: "Re-render as a photorealistic photograph, natural lighting, sharp focus" },
  { label: "Watercolor", prompt: "Re-render as a soft watercolor painting with gentle gradients and visible paper texture" },
  { label: "Darken", prompt: "Same composition, but at night — moody lighting, deep shadows, cool tones" },
  { label: "Brighten", prompt: "Same composition, brighter and airier — warm golden-hour light, soft pastels" },
  { label: "Vintage", prompt: "Apply a vintage aesthetic — faded colors, film grain, slight sepia tint" },
  { label: "Add neon", prompt: "Add neon lighting accents — vibrant pinks and cyans, dark background, futuristic mood" },
];

function SelectionToolbar({
  selected,
  history,
  onResult,
  onVideoResult,
  onToggleRef,
  isRefActive,
}: {
  selected: SelectedImageInfo;
  /** Lets Variants look up the original prompt that produced this
   *  shape — falls back to "a variation of this image" when the
   *  shape was placed via upload or external paste. */
  history: HistoryEntry[];
  onResult: (url: string, width: number, height: number) => void;
  /** Place a TLDraw video shape next to the selected image — used by
   *  Seedance i2v. URL is the realised MP4. */
  onVideoResult: (videoURL: string) => void;
  /** Toggle the chat's ref state to include the currently-selected
   *  image. Allows users to send "use this" → next prompt in one click. */
  onToggleRef: () => void;
  /** Whether the chat's ref state currently includes this image. */
  isRefActive: boolean;
}) {
  // pendingOp tracks which op is in flight so each button shows its
  // own spinner. Reimagine ~30s, Variants ~30-60s (parallel), Animate
  // ~60-120s — distinct icons keep the user oriented.
  const [pendingOp, setPendingOp] = useState<SelectionOp | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [reimagineOpen, setReimagineOpen] = useState(false);
  const [animateOpen, setAnimateOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);

  function closeAllPopovers() {
    setReimagineOpen(false);
    setAnimateOpen(false);
    setEditOpen(false);
  }

  async function runReimagine(prompt: string) {
    if (!prompt.trim()) {
      setErr("Tell us how to transform the image.");
      return;
    }
    setErr(null);
    setPendingOp("reimagine");
    setReimagineOpen(false);
    try {
      // Reimagine = NanoBanana with the source as a single reference.
      // Gemini's image-conditioned generation is strictly more capable
      // than Flux image2image — it understands "make this nighttime"
      // / "wood grain texture" / etc. without needing a precise prompt.
      const resp = await generateDesignNanoBanana({
        prompt,
        model: "nano-banana-pro",
        images: [selected.url],
      });
      const w = resp.width > 0 ? resp.width : selected.width;
      const h = resp.height > 0 ? resp.height : selected.height;
      onResult(resp.url, w, h);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPendingOp(null);
    }
  }

  async function runVariants() {
    // Look up the original prompt by shapeId so the 4 variants stay
    // thematically anchored. Falls back to a generic "variation of"
    // wrapper when the source was uploaded / external (no history row).
    const originalPrompt = history.find(
      (h) => h.shapeId === selected.shapeId,
    )?.prompt;
    const prompt = originalPrompt
      ? originalPrompt
      : "A variation of the reference image — keep the subject, vary composition and lighting.";

    setErr(null);
    setPendingOp("variants");
    try {
      const results = await Promise.allSettled(
        Array.from({ length: 4 }).map(() =>
          generateDesignNanoBanana({
            prompt,
            model: "nano-banana-2",
            images: [selected.url],
          }),
        ),
      );
      const placed = results.flatMap((r) =>
        r.status === "fulfilled"
          ? [{
              url: r.value.url,
              width: r.value.width > 0 ? r.value.width : selected.width,
              height: r.value.height > 0 ? r.value.height : selected.height,
            }]
          : [],
      );
      if (placed.length === 0) {
        const firstErr = results.find((r) => r.status === "rejected") as
          | PromiseRejectedResult
          | undefined;
        throw firstErr?.reason ?? new Error("all variants failed");
      }
      // Reach out to controller via global ref through onResult's
      // sibling path: we don't have direct placeVariants here, so
      // we synthesize a placement by calling onResult for each.
      // The first goes next to selection; subsequent ones cascade.
      // (Simpler and more legible than threading placeVariants
      // through.)
      for (const v of placed) {
        onResult(v.url, v.width, v.height);
      }
      if (placed.length < 4) {
        setErr(`${4 - placed.length} of 4 variants failed.`);
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPendingOp(null);
    }
  }

  async function runQuickEdit(preset: (typeof QUICK_EDITS)[number]) {
    setErr(null);
    setPendingOp("edit");
    setEditOpen(false);
    try {
      const resp = await generateDesignNanoBanana({
        prompt: preset.prompt,
        model: "nano-banana-pro",
        images: [selected.url],
      });
      const w = resp.width > 0 ? resp.width : selected.width;
      const h = resp.height > 0 ? resp.height : selected.height;
      onResult(resp.url, w, h);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPendingOp(null);
    }
  }

  async function runAnimate(
    prompt: string,
    resolution: SeedanceResolution,
    duration: number,
  ) {
    if (!prompt.trim()) {
      setErr("Tell us how the image should move.");
      return;
    }
    setErr(null);
    setPendingOp("animate");
    setAnimateOpen(false);
    try {
      const resp = await seedanceI2V({
        image_url: selected.url,
        prompt,
        resolution,
        duration,
        ratio: "adaptive",
      });
      onVideoResult(resp.video_url);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPendingOp(null);
    }
  }

  function runDownload() {
    // Anchor + click pattern — no library, no permission prompt. The
    // download attribute hints to the browser but most CORS-restricted
    // images will open in a new tab instead. That's still useful (user
    // can right-click → save) so we accept the fallback.
    const a = document.createElement("a");
    a.href = selected.url;
    a.download = `dream-waver-${Date.now()}.png`;
    a.target = "_blank";
    a.rel = "noopener";
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  async function runCopyUrl() {
    setErr(null);
    try {
      await navigator.clipboard.writeText(selected.url);
      // Brief visual feedback via the pendingOp spinner — repurpose
      // download as the "post-action" state since both are quick.
      setPendingOp("download");
      setTimeout(() => setPendingOp(null), 600);
    } catch (e) {
      setErr("Clipboard blocked — " + (e instanceof Error ? e.message : String(e)));
    }
  }

  return (
    <div className="pointer-events-auto inline-flex flex-col items-start gap-1">
      <div className="inline-flex items-center gap-0.5 rounded-lg border border-zinc-200 bg-white p-1 shadow-md">
        <ToolbarButton
          icon={<Repeat size={13} />}
          label="Reimagine"
          pending={pendingOp === "reimagine"}
          disabled={pendingOp !== null}
          onClick={() => {
            closeAllPopovers();
            setReimagineOpen(true);
          }}
          title="Regenerate with this image as a reference (NanoBanana Pro)"
        />
        <ToolbarButton
          icon={<Layers size={13} />}
          label="Variants"
          pending={pendingOp === "variants"}
          disabled={pendingOp !== null}
          onClick={runVariants}
          title="4 variants using this image as reference"
        />
        <ToolbarButton
          icon={<Wand2 size={13} />}
          label="Edit"
          pending={pendingOp === "edit"}
          disabled={pendingOp !== null}
          onClick={() => {
            closeAllPopovers();
            setEditOpen(true);
          }}
          title="Quick style transforms (preset prompts)"
        />
        <div className="h-5 w-px bg-zinc-200" />
        <ToolbarButton
          icon={<Film size={13} />}
          label="Animate"
          pending={pendingOp === "animate"}
          disabled={pendingOp !== null}
          onClick={() => {
            closeAllPopovers();
            setAnimateOpen(true);
          }}
          title="Image-to-video via Seedance 1.5 Pro (60-120s)"
        />
        <div className="h-5 w-px bg-zinc-200" />
        {/* "+ ref" toggles the chat's next-prompt ref state. The
            button reflects current state so users can SEE whether
            this image is armed. */}
        <ToolbarButton
          icon={<Sparkles size={13} className={isRefActive ? "text-fuchsia-500" : ""} />}
          label={isRefActive ? "Ref ✓" : "+ ref"}
          disabled={pendingOp !== null}
          onClick={onToggleRef}
          title={
            isRefActive
              ? "This image will be sent as a reference with your next chat prompt — click to remove"
              : "Send this image as a reference with your next chat prompt"
          }
        />
        <ToolbarButton
          icon={<Download size={13} />}
          label="Save"
          disabled={pendingOp !== null}
          onClick={runDownload}
          title="Download the image"
        />
        <ToolbarButton
          icon={<LinkIcon size={13} />}
          label={pendingOp === "download" ? "Copied" : "URL"}
          disabled={pendingOp !== null && pendingOp !== "download"}
          onClick={runCopyUrl}
          title="Copy image URL to clipboard"
        />
        {err && (
          <span className="ml-2 max-w-[240px] truncate text-[11px] text-red-600">
            {err}
          </span>
        )}
      </div>

      {reimagineOpen && (
        <ReimaginePopover
          onSubmit={runReimagine}
          onClose={() => setReimagineOpen(false)}
        />
      )}

      {editOpen && (
        <QuickEditsPopover
          onPick={runQuickEdit}
          onClose={() => setEditOpen(false)}
        />
      )}

      {animateOpen && (
        <AnimatePopover
          onSubmit={runAnimate}
          onClose={() => setAnimateOpen(false)}
        />
      )}
    </div>
  );
}

// QuickEditsPopover — small grid of preset transforms. Pure presentation
// (no state) — picking any item immediately fires runQuickEdit on the
// parent and closes the popover.
function QuickEditsPopover({
  onPick,
  onClose,
}: {
  onPick: (preset: (typeof QUICK_EDITS)[number]) => void;
  onClose: () => void;
}) {
  return (
    <div className="rounded-lg border border-zinc-200 bg-white p-2 shadow-md">
      <div className="mb-1.5 flex items-baseline justify-between px-1">
        <p className="text-[11px] text-zinc-500">Quick edits · Pro model</p>
        <button
          type="button"
          onClick={onClose}
          className="text-zinc-400 hover:text-zinc-700"
          aria-label="Close quick edits"
        >
          <X size={12} />
        </button>
      </div>
      <div className="grid grid-cols-2 gap-1">
        {QUICK_EDITS.map((preset) => (
          <button
            key={preset.label}
            type="button"
            onClick={() => onPick(preset)}
            title={preset.prompt}
            className="rounded border border-zinc-200 px-2 py-1 text-left text-[11px] text-zinc-700 transition hover:border-zinc-400 hover:bg-zinc-50"
          >
            {preset.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function AnimatePopover({
  onSubmit,
  onClose,
}: {
  onSubmit: (
    prompt: string,
    resolution: SeedanceResolution,
    duration: number,
  ) => void;
  onClose: () => void;
}) {
  const [text, setText] = useState("");
  // 720p is the doc default — strikes the best quality / latency / cost
  // balance. 480p halves cost, 1080p triples it.
  const [resolution, setResolution] = useState<SeedanceResolution>("720p");
  // 5s is the doc default; 4s is the floor (4 = Seedance minimum),
  // 8s is the practical max where motion stays coherent. The sidecar
  // caps at 12s but we expose 4-8 to keep cost predictable.
  const [duration, setDuration] = useState<number>(5);
  return (
    <form
      className="w-80 rounded-lg border border-zinc-200 bg-white p-3 shadow-md"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(text, resolution, duration);
      }}
    >
      <div className="mb-2 flex items-baseline justify-between">
        <p className="inline-flex items-baseline gap-1 text-[11px] text-zinc-500">
          <Film size={11} className="self-center" />
          Animate this image
        </p>
        <button
          type="button"
          onClick={onClose}
          className="text-zinc-400 hover:text-zinc-700"
        >
          <X size={12} />
        </button>
      </div>
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="Slow camera push-in, clouds drift across the sky, leaves rustle…"
        rows={3}
        className="w-full rounded-md border border-zinc-200 px-2 py-1.5 text-[12px] focus:border-zinc-400 focus:outline-none"
      />

      <div className="mt-2 grid grid-cols-2 gap-3">
        <div>
          <p className="mb-1 font-mono text-[10px] uppercase tracking-[0.14em] text-zinc-400">
            Resolution
          </p>
          <div className="inline-flex rounded border border-zinc-200 p-0.5">
            {(["480p", "720p", "1080p"] as const).map((r) => (
              <button
                key={r}
                type="button"
                onClick={() => setResolution(r)}
                className={
                  "rounded px-1.5 py-1 font-mono text-[10px] uppercase tracking-[0.12em] transition " +
                  (resolution === r
                    ? "bg-zinc-800 text-white"
                    : "text-zinc-500 hover:text-zinc-800")
                }
              >
                {r}
              </button>
            ))}
          </div>
        </div>
        <div>
          <p className="mb-1 font-mono text-[10px] uppercase tracking-[0.14em] text-zinc-400">
            Duration · {duration}s
          </p>
          <input
            type="range"
            min={4}
            max={8}
            step={1}
            value={duration}
            onChange={(e) => setDuration(Number(e.target.value))}
            className="w-full accent-zinc-900"
          />
        </div>
      </div>

      <button
        type="submit"
        disabled={!text.trim()}
        className="mt-3 w-full rounded-md bg-zinc-900 px-3 py-1.5 text-[12px] font-medium text-white disabled:opacity-50"
      >
        Animate (60-120s wait)
      </button>
      <p className="mt-1.5 text-[10px] leading-snug text-zinc-400">
        Seedance 1.5 Pro turns the source image into an MP4. Output lands
        on the canvas beside the source — click to play.
      </p>
    </form>
  );
}

function ReimaginePopover({
  onSubmit,
  onClose,
}: {
  onSubmit: (prompt: string) => void;
  onClose: () => void;
}) {
  const [text, setText] = useState("");
  return (
    <form
      className="w-80 rounded-lg border border-zinc-200 bg-white p-3 shadow-md"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(text);
      }}
    >
      <div className="mb-2 flex items-baseline justify-between">
        <p className="text-[11px] text-zinc-500">Reimagine this image</p>
        <button
          type="button"
          onClick={onClose}
          className="text-zinc-400 hover:text-zinc-700"
        >
          <X size={12} />
        </button>
      </div>
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="Make it nighttime, neon lighting, raining…"
        rows={3}
        className="w-full rounded-md border border-zinc-200 px-2 py-1.5 text-[12px] focus:border-zinc-400 focus:outline-none"
      />
      <button
        type="submit"
        disabled={!text.trim()}
        className="mt-2 w-full rounded-md bg-zinc-900 px-3 py-1.5 text-[12px] font-medium text-white disabled:opacity-50"
      >
        Reimagine
      </button>
    </form>
  );
}

function ToolbarButton({
  icon,
  label,
  pending,
  disabled,
  onClick,
  title,
}: {
  icon: React.ReactNode;
  label: string;
  pending?: boolean;
  disabled?: boolean;
  onClick: () => void;
  title?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={pending || disabled}
      title={title}
      className="inline-flex items-center gap-1.5 rounded px-2.5 py-1.5 text-[13px] font-medium text-zinc-800 transition hover:bg-zinc-100 disabled:cursor-not-allowed disabled:opacity-50"
    >
      {pending ? <Loader2 size={13} className="animate-spin" /> : icon}
      <span>{label}</span>
    </button>
  );
}

// ─── Header icon button (Undo / Redo) ────────────────────────────────
//
// Compact icon-only variant of ToolbarButton — header has limited
// vertical space so we drop the label and lean on the title tooltip.
// Disabled state is a transparency change rather than a full grey-out
// so the button still anchors the eye when undo/redo isn't available.

function HeaderIconButton({
  icon,
  label,
  disabled,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={label}
      aria-label={label}
      className="inline-flex h-7 w-7 items-center justify-center rounded text-zinc-700 transition hover:bg-zinc-100 disabled:cursor-not-allowed disabled:opacity-30 disabled:hover:bg-transparent"
    >
      {icon}
    </button>
  );
}

// ─── Floating selection toolbar wrapper ──────────────────────────────
//
// Positions the SelectionToolbar over the canvas in CSS pixels, anchored
// to the selected shape's screen-space top edge. Two ergonomic rules:
//
//   1. Center horizontally over the shape, then clamp to the canvas
//      wrapper so the toolbar never bleeds past either edge.
//   2. Prefer ABOVE the shape (more breathing room for the action
//      chips); flip BELOW when the shape is near the top of the
//      viewport and there's no room.
//
// Position derives from `selected.screenX/Y/W/H` which is updated by
// TldrawCanvas on every store mutation — pan / zoom / drag all refresh
// in step.

function FloatingSelectionToolbar({
  selected,
  children,
}: {
  selected: SelectedImageInfo;
  children: React.ReactNode;
}) {
  // Toolbar is ~360px wide in its current state (selection ops).
  // Hard-code the half so we can offset before render — measuring
  // with a ref would cause a one-frame jitter on first selection.
  const TOOLBAR_HALF_W = 180;
  const TOOLBAR_H = 40;
  const MARGIN = 8;

  // Centre of the shape's top edge, in canvas-wrapper pixels.
  const centerX = selected.screenX + selected.screenW / 2;
  const aboveY = selected.screenY - TOOLBAR_H - MARGIN;
  const belowY = selected.screenY + selected.screenH + MARGIN;
  const useAbove = aboveY > MARGIN;
  const top = useAbove ? aboveY : belowY;

  // Clamp horizontally inside the wrapper. We don't know the wrapper's
  // exact width here (no ref to it) so we use a generous heuristic:
  // never let `left` go below 8px. The right-edge clamp happens via
  // CSS `max-width: calc(100% - 16px)` on the inner positioner —
  // browsers handle the overflow without us measuring.
  const left = Math.max(MARGIN, centerX - TOOLBAR_HALF_W);

  return (
    <div
      className="dw-design-toolbar pointer-events-none absolute z-20"
      style={{ left: `${left}px`, top: `${top}px` }}
    >
      {children}
    </div>
  );
}

// ─── Export menu (header dropdown) ────────────────────────────────────
//
// "Download ▾" — clicked from the header, opens a small popover with
// PNG / SVG options. PNG is the default for sharing; SVG keeps vector
// fidelity for any drawn TLDraw shapes (uploads / AI raster images
// inside the SVG are <image href="..."> tagged with their data URL or
// hosted URL, so the file is self-contained either way).
//
// Selection-aware: when the user has shapes selected, only those
// shapes export. Empty selection → whole page. Same convention as
// Figma's "Export selection / Export frame".

function ExportMenu({
  controllerRef,
}: {
  controllerRef: React.RefObject<CanvasController | null>;
}) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState<"png" | "svg" | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);

  // Close on outside click.
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

  async function run(format: "png" | "svg") {
    setErr(null);
    setBusy(format);
    try {
      const blob = await controllerRef.current?.exportCanvas(format);
      if (!blob) {
        setErr("Canvas is empty.");
        return;
      }
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `dream-waver-canvas-${Date.now()}.${format}`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      // Free the blob URL after a tick — the browser starts the download
      // synchronously on click, so we can revoke right after.
      setTimeout(() => URL.revokeObjectURL(url), 100);
      setOpen(false);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        title="Export the canvas as PNG or SVG"
        aria-label="Export canvas"
        className="inline-flex h-7 items-center gap-1 rounded px-1.5 text-[12px] text-zinc-700 transition hover:bg-zinc-100"
      >
        <Download size={13} />
        <span className="font-mono text-[10px] uppercase tracking-[0.14em]">
          Export
        </span>
      </button>
      {open && (
        <div className="absolute right-0 top-full z-30 mt-1 w-44 rounded-md border border-zinc-200 bg-white p-1 shadow-md">
          <button
            type="button"
            disabled={busy !== null}
            onClick={() => run("png")}
            className="flex w-full items-center justify-between rounded px-2 py-1.5 text-left text-[12px] text-zinc-700 hover:bg-zinc-50 disabled:opacity-50"
          >
            <span>PNG</span>
            <span className="font-mono text-[9px] uppercase tracking-[0.12em] text-zinc-400">
              {busy === "png" ? "…" : "raster"}
            </span>
          </button>
          <button
            type="button"
            disabled={busy !== null}
            onClick={() => run("svg")}
            className="flex w-full items-center justify-between rounded px-2 py-1.5 text-left text-[12px] text-zinc-700 hover:bg-zinc-50 disabled:opacity-50"
          >
            <span>SVG</span>
            <span className="font-mono text-[9px] uppercase tracking-[0.12em] text-zinc-400">
              {busy === "svg" ? "…" : "vector"}
            </span>
          </button>
          {err && (
            <p className="mt-1 px-2 text-[10px] text-red-600">{err}</p>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Empty canvas hint ────────────────────────────────────────────────
//
// First-run nudge for new users. Vertically centered over the canvas,
// pointer-events-none so it doesn't intercept TLDraw input. Visibly
// suggests "type on the right" via a horizontal arrow + a Sparkles
// brand-color dot. Disappears the moment the first generation lands
// in history.

function EmptyCanvasHint() {
  return (
    <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
      <div className="flex flex-col items-center gap-2 text-zinc-400">
        <div className="inline-flex items-center gap-2 rounded-full border border-zinc-200 bg-white/80 px-4 py-2 shadow-sm backdrop-blur-sm">
          <Sparkles size={14} className="text-fuchsia-500" />
          <span className="text-[13px] text-zinc-700">
            Describe what you want on the right
          </span>
          <ArrowRight size={14} className="text-fuchsia-500" />
        </div>
        <p className="text-[11px] text-zinc-400">
          The generated image will land here.
        </p>
      </div>
    </div>
  );
}

// ─── Aspect-ratio + variants helpers ──────────────────────────────────
//
// The right-side chat picks an Aspect tier ("1:1" / "16:9" / "9:16" /
// "4:3"); useGenerateWithProgress wants pixel dimensions for the
// canvas placeholder. ASPECT_TO_WH is the lookup. Sizes are tuned so
// the source 1024-wide pre-divide-by-2 ends up at a comfortable 512
// in the typical viewport.

const ASPECT_TO_WH: Record<Aspect, [number, number]> = {
  "1:1": [1024, 1024],
  "16:9": [1280, 720],
  "9:16": [720, 1280],
  "4:3": [1024, 768],
};

/**
 * toHistoryEntry projects a server-side DesignAsset into the chat's
 * HistoryEntry shape. shapeId is intentionally undefined — the canvas
 * shape that produced this asset is long gone (closed tab / different
 * device / etc.). The chat's thumbnail click handler watches for
 * undefined shapeId and re-places the image instead of attempting to
 * focus a non-existent shape.
 */
function toHistoryEntry(asset: DesignAsset): HistoryEntry {
  return {
    id: asset.id,
    prompt: asset.prompt || "(no prompt recorded)",
    thumbnailUrl: asset.image_url,
    shapeId: undefined,
    status: "done",
    createdAt: new Date(asset.created_at).getTime(),
  };
}

function aspectW(a: Aspect): number {
  return ASPECT_TO_WH[a][0];
}

function aspectH(a: Aspect): number {
  return ASPECT_TO_WH[a][1];
}

/**
 * runVariants fans out 4 parallel NanoBanana calls with the same
 * prompt/refs/model — Gemini's internal sampling makes each output
 * distinct. Whichever succeed get placed as a 2×2 grid; failures
 * are silently dropped (the chat surfaces them inline if all 4 fail).
 *
 * Lives at page level rather than inside RightSideChat because it
 * needs the controllerRef to call placeVariants on the canvas.
 */
async function runVariants(
  controllerRef: React.RefObject<CanvasController | null>,
  prompt: string,
  opts: { model: NanoBananaModel; aspect: Aspect; refs?: string[] },
): Promise<void> {
  const ctrl = controllerRef.current;
  if (!ctrl) return;
  const [w, h] = ASPECT_TO_WH[opts.aspect];
  const results = await Promise.allSettled(
    Array.from({ length: 4 }).map(() =>
      generateDesignNanoBanana({
        prompt,
        model: opts.model,
        aspect_ratio: opts.aspect,
        images: opts.refs && opts.refs.length > 0 ? opts.refs : undefined,
      }),
    ),
  );
  const placed = results.flatMap((r) =>
    r.status === "fulfilled"
      ? [
          {
            url: r.value.url,
            width: r.value.width > 0 ? r.value.width : w,
            height: r.value.height > 0 ? r.value.height : h,
          },
        ]
      : [],
  );
  if (placed.length > 0) ctrl.placeVariants(placed);
}

// ─── Intent dispatcher (smart chat routing) ──────────────────────────
//
// The router returns a `DesignIntent` envelope; this maps each tool
// to the existing generation paths. Reused from the SelectionToolbar
// where possible:
//
//   generate    → startGeneration with prompt from intent.args
//   reimagine   → startGeneration with refs=[selected.url] + intent prompt
//   variants    → runVariants with refs=[selected.url] when selection exists
//   quick_edit  → startGeneration with selected as ref + the preset's
//                 prompt expansion (looked up from QUICK_EDITS via
//                 PRESET_PROMPT_BY_KEY below)
//   animate     → seedanceI2V; result is placed via placeVideoNextToSelection
//
// Every intent that needs a selection but the user has none falls
// back to plain `generate` so we never trigger a no-op endpoint.

const PRESET_PROMPT_BY_KEY: Record<string, string> = {
  minimalize: "Strip down to a minimalist version — fewer details, cleaner shapes, muted palette",
  cartoonize: "Re-render as a vibrant flat-style cartoon illustration",
  photoreal: "Re-render as a photorealistic photograph, natural lighting, sharp focus",
  watercolor: "Re-render as a soft watercolor painting with gentle gradients and visible paper texture",
  darken: "Same composition, but at night — moody lighting, deep shadows, cool tones",
  brighten: "Same composition, brighter and airier — warm golden-hour light, soft pastels",
  vintage: "Apply a vintage aesthetic — faded colors, film grain, slight sepia tint",
  add_neon: "Add neon lighting accents — vibrant pinks and cyans, dark background, futuristic mood",
};

function dispatchIntent({
  intent,
  originalMessage,
  opts,
  activeSkillId,
  selected,
  controllerRef,
  startGeneration,
}: {
  intent: DesignIntent;
  originalMessage: string;
  opts: { model: NanoBananaModel; aspect: Aspect; refs?: string[] };
  /** Currently-active skill id from the chat. Recorded on the resulting
   *  HistoryEntry so the per-result chip row shows skill-specific
   *  follow-ups (e.g. "Dark background" + "Monochrome" for Logo). */
  activeSkillId: string | null;
  selected: SelectedImageInfo | null;
  controllerRef: React.RefObject<CanvasController | null>;
  startGeneration: (req: GenerateRequest) => Promise<void>;
}): void {
  const skillId = activeSkillId ?? undefined;
  const argPrompt =
    typeof intent.args["prompt"] === "string"
      ? (intent.args["prompt"] as string)
      : originalMessage;

  switch (intent.tool) {
    case "reimagine":
    case "quick_edit": {
      // Need a selection to operate on. Fall back to generate if the
      // model picked a selection-only intent without one — defensive
      // even though router.go filters those tools out server-side.
      if (!selected) {
        void startGeneration({
          prompt: argPrompt,
          width: aspectW(opts.aspect),
          height: aspectH(opts.aspect),
          model: opts.model,
          aspect_ratio: opts.aspect,
          refs: opts.refs,
        });
        return;
      }
      const prompt =
        intent.tool === "quick_edit"
          ? PRESET_PROMPT_BY_KEY[
              (intent.args["preset"] as string) ?? ""
            ] ?? argPrompt
          : argPrompt;
      void startGeneration({
        prompt,
        width: aspectW(opts.aspect),
        height: aspectH(opts.aspect),
        model: intent.tool === "quick_edit" ? "nano-banana-pro" : opts.model,
        aspect_ratio: opts.aspect,
        refs: [selected.url, ...(opts.refs ?? [])].slice(0, 4),
        skillId,
      });
      return;
    }

    case "variants": {
      void runVariants(controllerRef, argPrompt, {
        model: opts.model,
        aspect: opts.aspect,
        // Use selected as ref when present so variants riff on it.
        refs: selected
          ? [selected.url, ...(opts.refs ?? [])].slice(0, 4)
          : opts.refs,
      });
      return;
    }

    case "animate": {
      if (!selected) {
        // Animate needs an image; fall back to generate to give
        // the user *something* useful.
        void startGeneration({
          prompt: argPrompt,
          width: aspectW(opts.aspect),
          height: aspectH(opts.aspect),
          model: opts.model,
          aspect_ratio: opts.aspect,
          refs: opts.refs,
        });
        return;
      }
      const resolution =
        (intent.args["resolution"] as SeedanceResolution | undefined) ?? "720p";
      const duration =
        typeof intent.args["duration"] === "number"
          ? (intent.args["duration"] as number)
          : 5;
      void seedanceI2V({
        image_url: selected.url,
        prompt: argPrompt,
        resolution,
        duration: Math.max(4, Math.min(12, duration)),
        ratio: "adaptive",
      })
        .then((resp) =>
          controllerRef.current?.placeVideoNextToSelection(resp.video_url),
        )
        .catch(() => {
          // Animate failures (Seedance content filter, network) — surface
          // would be nice but we don't have a chat-level toast plumbed
          // through this helper. The next /design iteration should
          // route this through the same notice channel as upload.
        });
      return;
    }

    case "extend_canvas":
    case "generate":
    default: {
      void startGeneration({
        prompt: argPrompt,
        width: aspectW(opts.aspect),
        height: aspectH(opts.aspect),
        model: opts.model,
        aspect_ratio: opts.aspect,
        refs: opts.refs,
        skillId,
      });
      return;
    }
  }
}

// ─── Skill follow-up dispatcher ──────────────────────────────────────
//
// When a user clicks one of the chips beneath a chat result (e.g.
// "Dark background" / "4 palette variants" / "Animate this"), look up
// the matching SkillFollowup recipe and fire the corresponding intent
// against THIS entry's image URL. Mirrors dispatchIntent's branching
// but keyed off the entry (history-anchored) rather than the live
// canvas selection.
//
// Falls back silently when the entry has no skill or the label has
// no match — the chip wouldn't render in those cases, so this is
// defence-in-depth.

function dispatchSkillFollowup({
  entry,
  label,
  controllerRef,
  startGeneration,
}: {
  entry: HistoryEntry;
  label: string;
  controllerRef: React.RefObject<CanvasController | null>;
  startGeneration: (req: GenerateRequest) => Promise<void>;
}): void {
  const skill = getSkill(entry.skillId);
  if (!skill) return;
  const followup = skill.followups.find((f) => f.label === label);
  if (!followup) return;

  const refURL = entry.thumbnailUrl;

  switch (followup.intent) {
    case "variants": {
      void runVariants(controllerRef, followup.prompt, {
        model: skill.model,
        aspect: skill.aspect,
        refs: refURL ? [refURL] : undefined,
      });
      return;
    }
    case "reimagine": {
      void startGeneration({
        prompt: followup.prompt,
        width: 1024,
        height: 1024,
        model: "nano-banana-pro",
        aspect_ratio: skill.aspect,
        refs: refURL ? [refURL] : undefined,
        skillId: skill.id,
      });
      return;
    }
    case "animate": {
      if (!refURL) return;
      void seedanceI2V({
        image_url: refURL,
        prompt: followup.prompt,
        resolution: "720p",
        duration: 5,
        ratio: "adaptive",
      })
        .then((resp) => {
          // Best-effort placement: if the entry's shape is still on
          // canvas, drop the MP4 next to it. Otherwise viewport
          // centre via placeImage's fallback (placeVideo doesn't
          // have a "place anywhere" mode yet).
          if (entry.shapeId) {
            controllerRef.current?.focusShape(entry.shapeId);
          }
          controllerRef.current?.placeVideoNextToSelection(resp.video_url);
        })
        .catch(() => {
          // Seedance content filter / network error — silent for
          // chip-driven actions; the user retrying is cheap.
        });
      return;
    }
    case "generate":
    default: {
      void startGeneration({
        prompt: followup.prompt,
        width: 1024,
        height: 1024,
        model: skill.model,
        aspect_ratio: skill.aspect,
        skillId: skill.id,
      });
      return;
    }
  }
}

/**
 * sketchToDataURLRef captures the user's drawn strokes off the canvas,
 * converts the resulting PNG blob into a base64 data URL, and returns
 * that URL. The chat appends it to its ref list so the user's next
 * NanoBanana prompt is conditioned on the sketch.
 *
 * Throws when the canvas has no drawable shapes — the chat surfaces
 * the message inline. This is the Krea-style "I scribbled, AI fills
 * in the rest" loop minus the realtime preview.
 */
async function sketchToDataURLRef(
  controllerRef: React.RefObject<CanvasController | null>,
): Promise<string> {
  const ctrl = controllerRef.current;
  if (!ctrl) throw new Error("canvas not ready");
  const blob = await ctrl.captureSketchRegion();
  if (!blob) {
    throw new Error(
      "Nothing to capture — draw something on the canvas first (use the TLDraw draw tool).",
    );
  }
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") resolve(reader.result);
      else reject(new Error("FileReader returned non-string"));
    };
    reader.onerror = () => reject(reader.error ?? new Error("encode failed"));
    reader.readAsDataURL(blob);
  });
}

/**
 * uploadToCanvas reads a user-selected file as a data URL, gets the
 * natural dimensions off the loaded image, and places it on the canvas
 * via the dedicated "user-upload" path (which tags the asset so the
 * SelectionToolbar's AI ops don't appear — data URLs would 502 at the
 * upstream gateway anyway).
 *
 * Returns once the shape is on the canvas. Rejects if the file isn't
 * an image or if the FileReader / Image load fails.
 */
async function uploadToCanvas(
  controllerRef: React.RefObject<CanvasController | null>,
  file: File,
): Promise<void> {
  const ctrl = controllerRef.current;
  if (!ctrl) throw new Error("canvas not ready");

  // Read the file as a data URL. This is the simplest path that
  // doesn't depend on a hosted upload endpoint — fine for display.
  const dataURL = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") resolve(reader.result);
      else reject(new Error("FileReader returned non-string"));
    };
    reader.onerror = () => reject(reader.error ?? new Error("read failed"));
    reader.readAsDataURL(file);
  });

  // Measure the natural dimensions so the shape's display size
  // matches what the user uploaded.
  const dims = await new Promise<{ w: number; h: number }>((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve({ w: img.naturalWidth, h: img.naturalHeight });
    img.onerror = () => reject(new Error("could not decode image"));
    img.src = dataURL;
  });

  ctrl.placeUploadedImage(dataURL, dims.w, dims.h);
}
