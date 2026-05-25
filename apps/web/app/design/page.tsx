"use client";

import { useCallback, useRef, useState, type FormEvent } from "react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { ArrowLeft, Eraser, Loader2, Sparkles, Wand2, X } from "lucide-react";

import {
  ApiError,
  enhanceDesignImage,
  generateDesignImage,
  generateDesignVariants,
  removeDesignImageBg,
} from "@/lib/api";
import type {
  CanvasController,
  SelectedImageInfo,
} from "./TldrawCanvas";

// /design is the canvas surface — TLDraw as the artboard, our toolbars
// float on top:
//
//   AI panel (top-right)          create new images. Mode toggle for
//                                 single image vs. 4-variant grid.
//   Selection toolbar (top-center) acts on the currently-selected AI
//                                 image: Remove BG / Enhance. Hidden
//                                 when nothing's selected.
//
// Architecturally the canvas is the entire product surface — no
// /design/new + /design/[id] split because the canvas IS the document.
// Persistence will swap from TLDraw's localStorage default to a real
// /canvases endpoint when auth lands; for MVP refresh keeps the work.
//
// SSR note: TLDraw assumes a browser — it pokes at `window` during
// import. We lazy-load via next/dynamic with ssr:false; the page
// shell (header + panels) renders during SSR so the user sees
// chrome before TLDraw boots.

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

export default function DesignPage() {
  // CanvasController is the imperative API the panels call into. Held
  // in a ref so a new generation doesn't re-render the canvas.
  const controllerRef = useRef<CanvasController | null>(null);
  const [selected, setSelected] = useState<SelectedImageInfo | null>(null);

  const handleReady = useCallback((controller: CanvasController) => {
    controllerRef.current = controller;
  }, []);

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

      <div className="relative flex-1 overflow-hidden">
        <TldrawClient onReady={handleReady} onSelectionChange={setSelected} />

        {/* Selection toolbar — top-center, only when an AI image is
            selected. Fixed-position rather than anchored to the shape
            so dragging the image doesn't require re-positioning the
            popover; the trade-off is the toolbar feels less attached. */}
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

        {/* AI panel — floating top-right above the canvas. Always
            present; the selection toolbar overlays the top-center
            area so they don't collide visually. */}
        <div className="pointer-events-none absolute right-4 top-4 z-20">
          <AiImagePanel
            onImage={(url, w, h) => controllerRef.current?.placeImage(url, w, h)}
            onVariants={(variants) =>
              controllerRef.current?.placeVariants(variants)
            }
          />
        </div>
      </div>
    </main>
  );
}

// ─── Selection toolbar ────────────────────────────────────────────────

function SelectionToolbar({
  selected,
  onResult,
}: {
  selected: SelectedImageInfo;
  onResult: (url: string, width: number, height: number) => void;
}) {
  // pendingOp is "remove_bg" | "enhance" | null. We track which op is
  // in flight so the button can show its OWN spinner — important
  // because both ops take 30-60 s and the user needs to know which
  // request they're waiting on. Disabling the OTHER button during
  // flight also prevents double-firing.
  const [pendingOp, setPendingOp] = useState<"remove_bg" | "enhance" | null>(
    null,
  );
  const [err, setErr] = useState<string | null>(null);

  async function run(op: "remove_bg" | "enhance") {
    setErr(null);
    setPendingOp(op);
    try {
      const resp =
        op === "remove_bg"
          ? await removeDesignImageBg({ image_url: selected.url })
          : await enhanceDesignImage({ image_url: selected.url });
      // Sidecar returns optional width/height (DreamAPI doesn't always
      // echo them); fall back to source dimensions so the canvas can
      // still place the new shape sensibly. The IMG element itself
      // will reflect the true dimensions once loaded.
      const w = resp.width ?? selected.width;
      const h = resp.height ?? selected.height;
      onResult(resp.url, w, h);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPendingOp(null);
    }
  }

  return (
    <div className="pointer-events-auto inline-flex items-center gap-1 rounded-lg border border-zinc-200 bg-white p-1 shadow-md">
      <ToolbarButton
        icon={<Eraser size={13} />}
        label="Remove BG"
        pending={pendingOp === "remove_bg"}
        disabled={pendingOp === "enhance"}
        onClick={() => run("remove_bg")}
      />
      <div className="h-5 w-px bg-zinc-200" />
      <ToolbarButton
        icon={<Wand2 size={13} />}
        label="Enhance"
        pending={pendingOp === "enhance"}
        disabled={pendingOp === "remove_bg"}
        onClick={() => run("enhance")}
      />
      {err && (
        <span className="ml-2 max-w-[280px] truncate text-[11px] text-red-600">
          {err}
        </span>
      )}
    </div>
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
  onImage,
  onVariants,
}: {
  onImage: (url: string, width: number, height: number) => void;
  onVariants: (
    variants: Array<{ url: string; width: number; height: number }>,
  ) => void;
}) {
  const [open, setOpen] = useState(false);
  const [prompt, setPrompt] = useState("");
  const [aspect, setAspect] = useState<"1024x1024" | "1024x576" | "576x1024">(
    "1024x1024",
  );
  // mode = "single" (one image at centre) | "variants" (2x2 grid of 4)
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
    setSubmitting(true);
    try {
      if (mode === "single") {
        const resp = await generateDesignImage({ prompt, width: w, height: h });
        onImage(resp.url, resp.width, resp.height);
      } else {
        const resp = await generateDesignVariants({
          prompt,
          count: 4,
          width: w,
          height: h,
        });
        onVariants(resp.variants);
      }
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

      {/* Mode tabs — single vs variants */}
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
          {submitting
            ? mode === "single"
              ? "Generating (30-60 s)…"
              : "Generating 4 variants…"
            : mode === "single"
              ? "Generate"
              : "Generate 4 variants"}
        </button>

        <p className="text-[10px] leading-snug text-zinc-400">
          Powered by DreamAPI Flux. Select any AI image on the canvas to
          unlock Remove BG / Enhance in the top toolbar.
        </p>
      </form>
    </div>
  );
}
