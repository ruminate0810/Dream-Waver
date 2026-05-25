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
import {
  ArrowLeft,
  Eraser,
  Loader2,
  Maximize2,
  Repeat,
  Sparkles,
  Wand2,
  X,
} from "lucide-react";

import {
  ApiError,
  designGenerateEventsURL,
  enhanceDesignImage,
  generateDesignVariants,
  image2imageDesignImage,
  outpaintDesignImage,
  removeDesignImageBg,
  submitDesignGenerate,
} from "@/lib/api";
import type {
  CanvasController,
  SelectedImageInfo,
} from "./TldrawCanvas";
import { ChatCopilot } from "./ChatCopilot";

// /design is the canvas surface — TLDraw as the artboard, our toolbars
// float on top:
//
//   ChatCopilot drawer (left)     prompt-first natural language entry;
//                                 history of generations with thumbnails
//                                 (click to jump on canvas).
//   AI panel (top-right)          structured generation — single vs
//                                 4 variants, aspect-ratio selector.
//   Selection toolbar (top-center) acts on the currently-selected AI
//                                 image: Remove BG / Enhance / Extend /
//                                 Reimagine. Hidden when nothing's selected.
//
// Single-image generation goes through the SSE flow (submit + EventSource)
// so the canvas shows a "Generating · 8s" placeholder rectangle in place
// of the eventual image — the 30-60 s wait becomes legible. Variants +
// edit ops stay synchronous for MVP (less benefit, more complexity).
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

/** One row in the ChatCopilot history. Stored at the page level so the
 *  copilot can outlive its own component instance (open/close drawer
 *  toggles the React subtree). */
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
};

export default function DesignPage() {
  const controllerRef = useRef<CanvasController | null>(null);
  const [selected, setSelected] = useState<SelectedImageInfo | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);

  const handleReady = useCallback((controller: CanvasController) => {
    controllerRef.current = controller;
  }, []);

  const startGeneration = useGenerateWithProgress(controllerRef, setHistory);

  return (
    <main className="flex h-screen flex-col bg-zinc-50 text-zinc-900">
      <header className="z-10 flex items-baseline justify-between border-b border-zinc-100 bg-white px-6 py-3">
        <Link
          href="/"
          className="inline-flex items-baseline gap-2 text-xs text-zinc-500 hover:text-zinc-800"
        >
          <ArrowLeft size={12} /> Dream-Waver
        </Link>
        <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-400">
          Design · Canvas
        </span>
      </header>

      <div className="relative flex flex-1 overflow-hidden">
        {/* Left drawer — ChatCopilot. Collapsible to give the canvas
            full width when not in use. */}
        <ChatCopilot
          history={history}
          onSubmit={(prompt) =>
            startGeneration({ prompt, width: 1024, height: 1024 })
          }
          onFocus={(shapeId) => controllerRef.current?.focusShape(shapeId)}
        />

        <div className="relative flex-1 overflow-hidden">
          <TldrawClient onReady={handleReady} onSelectionChange={setSelected} />

          {/* Selection toolbar — top-center, only when an AI image is
              selected. Position is fixed relative to the canvas wrapper
              rather than anchored to the shape — dragging the image
              doesn't require re-positioning the popover. */}
          {selected && (
            <div className="pointer-events-none absolute left-1/2 top-4 z-20 -translate-x-1/2">
              <SelectionToolbar
                selected={selected}
                onResult={(url, w, h) =>
                  controllerRef.current?.placeNextToSelection(url, w, h)
                }
              />
            </div>
          )}

          {/* AI panel — floating top-right. Always present; closed by
              default to keep the canvas uncluttered. */}
          <div className="pointer-events-none absolute right-4 top-4 z-20">
            <AiImagePanel
              onSingle={(req) => startGeneration(req)}
              onVariants={(variants) =>
                controllerRef.current?.placeVariants(variants)
              }
            />
          </div>
        </div>
      </div>
    </main>
  );
}

// ─── Generation hook (SSE-backed for single images) ────────────────────

type GenerateRequest = { prompt: string; width: number; height: number };

/**
 * useGenerateWithProgress wires the SSE flow:
 *   1. Place a placeholder shape on the canvas immediately
 *   2. POST submit → get task_id
 *   3. Open EventSource on the events URL
 *   4. On `progress` events: update the placeholder caption
 *   5. On `done`: swap the placeholder for the real image
 *   6. On `error`: turn the placeholder red with the error
 *
 * Returns a `start` function the panels call. Each call is independent
 * — you can queue several generations in parallel, each with its own
 * placeholder + EventSource.
 */
function useGenerateWithProgress(
  controllerRef: React.RefObject<CanvasController | null>,
  setHistory: React.Dispatch<React.SetStateAction<HistoryEntry[]>>,
): (req: GenerateRequest) => Promise<void> {
  // Keep references to open EventSources so we can close them if the
  // component unmounts mid-generation.
  const eventSourcesRef = useRef<Set<EventSource>>(new Set());
  useEffect(() => {
    return () => {
      eventSourcesRef.current.forEach((es) => es.close());
      eventSourcesRef.current.clear();
    };
  }, []);

  return useCallback(
    async (req: GenerateRequest) => {
      const ctrl = controllerRef.current;
      if (!ctrl) return;

      // Placeholder shape goes down FIRST — gives the user immediate
      // feedback that the click registered. The SSE flow then mutates
      // its caption / replaces it on completion.
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
        },
        ...prev,
      ]);

      let taskId: string;
      try {
        const resp = await submitDesignGenerate({
          prompt: req.prompt,
          width: req.width,
          height: req.height,
        });
        taskId = resp.task_id;
      } catch (e) {
        const msg = e instanceof ApiError ? e.message : String((e as Error).message ?? e);
        ctrl.failPlaceholder(placeholderId, msg);
        setHistory((prev) =>
          prev.map((h) =>
            h.id === historyId
              ? { ...h, status: "error", errorMessage: msg }
              : h,
          ),
        );
        return;
      }

      // Open the SSE stream — browser handles reconnect on transient
      // disconnects (5xx, mid-stream drop). We close it ourselves on
      // any terminal event so the connection doesn't dangle.
      const es = new EventSource(designGenerateEventsURL(taskId));
      eventSourcesRef.current.add(es);
      const cleanup = () => {
        es.close();
        eventSourcesRef.current.delete(es);
      };

      es.addEventListener("progress", (e) => {
        try {
          const data = JSON.parse((e as MessageEvent).data) as {
            elapsed_s?: number;
            status?: string;
          };
          ctrl.updatePlaceholderProgress(
            placeholderId,
            data.elapsed_s ?? 0,
            data.status ?? "running",
          );
        } catch {
          // Drop malformed frames silently.
        }
      });

      es.addEventListener("done", (e) => {
        try {
          const data = JSON.parse((e as MessageEvent).data) as {
            url: string;
            width: number;
            height: number;
          };
          ctrl.replacePlaceholderWithImage(
            placeholderId,
            data.url,
            data.width,
            data.height,
          );
          setHistory((prev) =>
            prev.map((h) =>
              h.id === historyId
                ? { ...h, status: "done", thumbnailUrl: data.url }
                : h,
            ),
          );
        } catch (parseErr) {
          ctrl.failPlaceholder(placeholderId, String(parseErr));
        } finally {
          cleanup();
        }
      });

      es.addEventListener("error", (e) => {
        let msg = "stream error";
        const data = (e as MessageEvent).data;
        if (data) {
          try {
            const parsed = JSON.parse(data) as { message?: string };
            if (parsed.message) msg = parsed.message;
          } catch {
            /* fall back to default */
          }
        }
        ctrl.failPlaceholder(placeholderId, msg);
        setHistory((prev) =>
          prev.map((h) =>
            h.id === historyId
              ? { ...h, status: "error", errorMessage: msg }
              : h,
          ),
        );
        cleanup();
      });
    },
    [controllerRef, setHistory],
  );
}

// ─── Selection toolbar ────────────────────────────────────────────────

type SelectionOp = "remove_bg" | "enhance" | "extend" | "reimagine";

function SelectionToolbar({
  selected,
  onResult,
}: {
  selected: SelectedImageInfo;
  onResult: (url: string, width: number, height: number) => void;
}) {
  // pendingOp tracks which op is in flight so each button shows its
  // own spinner — both remove_bg + enhance + outpaint take 30-60 s,
  // and visualising which one the user is waiting on matters.
  const [pendingOp, setPendingOp] = useState<SelectionOp | null>(null);
  const [err, setErr] = useState<string | null>(null);
  // extendOpen + reimagineOpen toggle small popovers below the toolbar
  // when the corresponding action needs more input than a simple click.
  const [extendOpen, setExtendOpen] = useState(false);
  const [reimagineOpen, setReimagineOpen] = useState(false);

  async function runSimple(op: "remove_bg" | "enhance") {
    setErr(null);
    setPendingOp(op);
    try {
      const resp =
        op === "remove_bg"
          ? await removeDesignImageBg({ image_url: selected.url })
          : await enhanceDesignImage({ image_url: selected.url });
      onResult(resp.url, resp.width ?? selected.width, resp.height ?? selected.height);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPendingOp(null);
    }
  }

  async function runOutpaint(preset: "16:9" | "9:16" | "1:1") {
    // Map preset → extend amounts. We extend symmetrically until the
    // result hits the target ratio, capped at +1024 px per side to
    // keep cost in check (DreamAPI prices roughly by output area).
    const { width: srcW, height: srcH } = selected;
    const targetRatio = preset === "16:9" ? 16 / 9 : preset === "9:16" ? 9 / 16 : 1;
    const currentRatio = srcW / srcH;
    let left = 0, right = 0, top = 0, bottom = 0;
    if (Math.abs(currentRatio - targetRatio) < 0.02) {
      setErr(`Already close to ${preset}`);
      return;
    }
    if (currentRatio < targetRatio) {
      // Need wider — extend horizontally.
      const targetW = Math.round(srcH * targetRatio);
      const extra = Math.min(targetW - srcW, 2048);
      left = right = Math.round(extra / 2);
    } else {
      // Need taller — extend vertically.
      const targetH = Math.round(srcW / targetRatio);
      const extra = Math.min(targetH - srcH, 2048);
      top = bottom = Math.round(extra / 2);
    }
    setErr(null);
    setPendingOp("extend");
    setExtendOpen(false);
    try {
      const resp = await outpaintDesignImage({
        image_url: selected.url,
        left,
        right,
        top,
        bottom,
      });
      onResult(
        resp.url,
        (resp.width ?? srcW + left + right),
        (resp.height ?? srcH + top + bottom),
      );
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPendingOp(null);
    }
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
      const resp = await image2imageDesignImage({
        image_url: selected.url,
        prompt,
        width: selected.width,
        height: selected.height,
      });
      onResult(resp.url, resp.width, resp.height);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPendingOp(null);
    }
  }

  return (
    <div className="pointer-events-auto inline-flex flex-col items-start gap-1">
      <div className="inline-flex items-center gap-1 rounded-lg border border-zinc-200 bg-white p-1 shadow-md">
        <ToolbarButton
          icon={<Eraser size={13} />}
          label="Remove BG"
          pending={pendingOp === "remove_bg"}
          disabled={pendingOp !== null}
          onClick={() => runSimple("remove_bg")}
        />
        <div className="h-5 w-px bg-zinc-200" />
        <ToolbarButton
          icon={<Wand2 size={13} />}
          label="Enhance"
          pending={pendingOp === "enhance"}
          disabled={pendingOp !== null}
          onClick={() => runSimple("enhance")}
        />
        <div className="h-5 w-px bg-zinc-200" />
        <ToolbarButton
          icon={<Maximize2 size={13} />}
          label="Extend"
          pending={pendingOp === "extend"}
          disabled={pendingOp !== null}
          onClick={() => {
            setExtendOpen(!extendOpen);
            setReimagineOpen(false);
          }}
        />
        <div className="h-5 w-px bg-zinc-200" />
        <ToolbarButton
          icon={<Repeat size={13} />}
          label="Reimagine"
          pending={pendingOp === "reimagine"}
          disabled={pendingOp !== null}
          onClick={() => {
            setReimagineOpen(!reimagineOpen);
            setExtendOpen(false);
          }}
        />
        {err && (
          <span className="ml-2 max-w-[280px] truncate text-[11px] text-red-600">
            {err}
          </span>
        )}
      </div>

      {extendOpen && (
        <div className="rounded-lg border border-zinc-200 bg-white p-3 shadow-md">
          <p className="mb-2 text-[11px] text-zinc-500">Extend to aspect ratio</p>
          <div className="flex gap-1.5">
            {(["16:9", "9:16", "1:1"] as const).map((r) => (
              <button
                key={r}
                type="button"
                onClick={() => runOutpaint(r)}
                className="rounded border border-zinc-200 px-3 py-1.5 font-mono text-[11px] hover:border-zinc-400 hover:bg-zinc-50"
              >
                {r}
              </button>
            ))}
          </div>
        </div>
      )}

      {reimagineOpen && (
        <ReimaginePopover
          onSubmit={runReimagine}
          onClose={() => setReimagineOpen(false)}
        />
      )}
    </div>
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
}: {
  icon: React.ReactNode;
  label: string;
  pending?: boolean;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={pending || disabled}
      className="inline-flex items-center gap-1.5 rounded px-2.5 py-1.5 text-[13px] font-medium text-zinc-800 transition hover:bg-zinc-100 disabled:cursor-not-allowed disabled:opacity-50"
    >
      {pending ? <Loader2 size={13} className="animate-spin" /> : icon}
      <span>{label}</span>
    </button>
  );
}

// ─── AI panel (create new) ────────────────────────────────────────────

function AiImagePanel({
  onSingle,
  onVariants,
}: {
  onSingle: (req: GenerateRequest) => void;
  onVariants: (
    variants: Array<{ url: string; width: number; height: number }>,
  ) => void;
}) {
  const [open, setOpen] = useState(false);
  const [prompt, setPrompt] = useState("");
  const [aspect, setAspect] = useState<"1024x1024" | "1024x576" | "576x1024">(
    "1024x1024",
  );
  const [mode, setMode] = useState<"single" | "variants">("single");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<{ message: string; fieldErrors: string[] } | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr(null);
    if (!prompt.trim()) {
      setErr({ message: "Tell us what to generate.", fieldErrors: [] });
      return;
    }
    const [w, h] = aspect.split("x").map(Number) as [number, number];

    if (mode === "single") {
      // Single image takes the SSE path — placeholder appears
      // immediately, the panel can be reused without waiting.
      onSingle({ prompt, width: w, height: h });
      setPrompt("");
      return;
    }

    // Variants still synchronous — 4-image grid drops at once.
    setSubmitting(true);
    try {
      const resp = await generateDesignVariants({
        prompt,
        count: 4,
        width: w,
        height: h,
      });
      onVariants(resp.variants);
      setPrompt("");
    } catch (e) {
      if (e instanceof ApiError) {
        setErr({ message: e.message, fieldErrors: e.fieldErrors });
      } else {
        setErr({ message: String((e as Error).message ?? e), fieldErrors: [] });
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="pointer-events-auto inline-flex items-center gap-1.5 rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm font-medium text-zinc-800 shadow-sm transition hover:border-zinc-300 hover:bg-zinc-50"
      >
        <Sparkles size={14} className="text-fuchsia-500" />
        <span>AI image</span>
      </button>
    );
  }

  return (
    <div className="pointer-events-auto w-80 rounded-lg border border-zinc-200 bg-white p-4 shadow-lg">
      <header className="mb-3 flex items-baseline justify-between">
        <h3 className="inline-flex items-baseline gap-1.5 text-sm font-medium text-zinc-900">
          <Sparkles size={13} className="self-center text-fuchsia-500" />
          AI image
        </h3>
        <button
          type="button"
          onClick={() => setOpen(false)}
          className="text-zinc-400 hover:text-zinc-700"
          aria-label="Close AI panel"
        >
          <X size={14} />
        </button>
      </header>

      <div className="mb-3 inline-flex rounded border border-zinc-200 p-0.5">
        {(["single", "variants"] as const).map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => setMode(m)}
            disabled={submitting}
            className={
              "rounded px-2.5 py-1 font-mono text-[10px] uppercase tracking-[0.14em] transition " +
              (mode === m
                ? "bg-zinc-800 text-white"
                : "text-zinc-500 hover:text-zinc-800")
            }
          >
            {m === "single" ? "1 image" : "4 variants"}
          </button>
        ))}
      </div>

      <form onSubmit={onSubmit} className="space-y-3">
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="A vibrant neon cyberpunk cityscape at dusk…"
          rows={4}
          disabled={submitting}
          spellCheck={false}
          className="w-full rounded-md border border-zinc-200 px-2.5 py-2 text-[13px] focus:border-zinc-400 focus:outline-none"
        />

        <div className="flex items-center gap-1">
          {(["1024x1024", "1024x576", "576x1024"] as const).map((a) => (
            <button
              key={a}
              type="button"
              onClick={() => setAspect(a)}
              disabled={submitting}
              className={
                "rounded border px-2 py-1 font-mono text-[10px] uppercase tracking-[0.12em] transition " +
                (aspect === a
                  ? "border-zinc-800 bg-zinc-800 text-white"
                  : "border-zinc-200 text-zinc-600 hover:border-zinc-300")
              }
            >
              {a === "1024x1024" ? "1:1" : a === "1024x576" ? "16:9" : "9:16"}
            </button>
          ))}
        </div>

        {err && (
          <div className="rounded border border-red-200 bg-red-50 px-2.5 py-2 text-[12px] text-red-700">
            <div>{err.message}</div>
            {err.fieldErrors.length > 0 && (
              <ul className="mt-1 list-disc space-y-0.5 pl-4">
                {err.fieldErrors.map((line, i) => (
                  <li key={i}>{line}</li>
                ))}
              </ul>
            )}
          </div>
        )}

        <button
          type="submit"
          disabled={submitting || !prompt.trim()}
          className="inline-flex w-full items-center justify-center gap-2 rounded-md bg-zinc-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
        >
          {submitting && <Loader2 size={13} className="animate-spin" />}
          {mode === "single"
            ? "Generate (in-canvas progress)"
            : submitting
              ? "Generating 4 variants…"
              : "Generate 4 variants"}
        </button>

        <p className="text-[10px] leading-snug text-zinc-400">
          Single-image generation shows a live placeholder on the canvas.
          Select any AI image to unlock Remove BG / Enhance / Extend / Reimagine.
        </p>
      </form>
    </div>
  );
}
