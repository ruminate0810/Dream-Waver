"use client";

import { useCallback, useRef, useState, type FormEvent } from "react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { ArrowLeft, Loader2, Sparkles, X } from "lucide-react";

import {
  ApiError,
  generateDesignImage,
  type GenerateDesignImageResponse,
} from "@/lib/api";

// /design is the canvas surface — TLDraw as the artboard, our toolbar
// floats on top, AI image generation lands new images directly into
// the editor as proper image shapes (which means TLDraw's selection /
// resize / rotate machinery works without us writing any of it).
//
// Architecturally the canvas is the entire product surface — there is
// no /design/new + /design/[id] split like slides / video / games,
// because the canvas IS the document. Persistence will come later via
// TLDraw's snapshot APIs + a /design/canvases endpoint; for MVP the
// session lives in localStorage (TLDraw default) and refresh keeps it.
//
// SSR note: TLDraw assumes a browser — it pokes at `window` during
// import. Next.js 15 / React 19 forces us to lazy-load with ssr:false;
// the rest of the page renders normally during SSR (header + AI panel
// chrome) so the user sees the shell before TLDraw boots.

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
  // The TldrawCanvas exposes a ref-like "place an image" handler via a
  // small imperative shim — we hold it here and the AI panel calls it
  // after a successful generation. Using a ref avoids re-rendering the
  // whole canvas every time the panel state changes.
  const placeImageRef = useRef<((url: string, w: number, h: number) => void) | null>(null);

  const handleCanvasReady = useCallback(
    (placeImage: (url: string, w: number, h: number) => void) => {
      placeImageRef.current = placeImage;
    },
    [],
  );

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
        <TldrawClient onReady={handleCanvasReady} />

        {/* AI panel — floating top-right above the canvas. Positioned
            absolutely so it doesn't fight TLDraw's own toolbar layout. */}
        <div className="pointer-events-none absolute right-4 top-4 z-20">
          <AiImagePanel
            onGenerated={(url, w, h) => placeImageRef.current?.(url, w, h)}
          />
        </div>
      </div>
    </main>
  );
}

// ─── AI panel ─────────────────────────────────────────────────────────

function AiImagePanel({
  onGenerated,
}: {
  onGenerated: (url: string, width: number, height: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const [prompt, setPrompt] = useState("");
  const [aspect, setAspect] = useState<"1024x1024" | "1024x576" | "576x1024">(
    "1024x1024",
  );
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
      const resp: GenerateDesignImageResponse = await generateDesignImage({
        prompt,
        width: w,
        height: h,
      });
      onGenerated(resp.url, resp.width, resp.height);
      // Keep the panel open with prompt cleared, so the user can
      // queue another generation without re-opening.
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
          {submitting ? "Generating (30-60 s)…" : "Generate"}
        </button>

        <p className="text-[10px] leading-snug text-zinc-400">
          Powered by DreamAPI Flux. Image lands centered on the canvas;
          drag, resize, or duplicate with TLDraw&apos;s built-in tools.
        </p>
      </form>
    </div>
  );
}
