"use client";

import { useState } from "react";
import clsx from "clsx";

// PreviewGrid renders the WITH-TEXT slide thumbnails the orchestrator saved
// next to the PPTX. Click any thumbnail to open a lightbox-style fullscreen
// view. URLs come from the API (job.preview_urls); we proxy them through
// Next.js's rewrite so same-origin lazy-load works without CORS.
//
// `cacheBust` is bumped by the parent every time a follow-up edit
// completes — it appends a `?v=N` query string so the browser re-fetches
// the (now updated) PNG at the same logical URL instead of serving the
// stale cached copy.
export function PreviewGrid({
  urls,
  cacheBust = 0,
}: {
  urls: string[];
  cacheBust?: number;
}) {
  const [openIndex, setOpenIndex] = useState<number | null>(null);
  const bust = (u: string) => (cacheBust > 0 ? `${u}?v=${cacheBust}` : u);

  if (!urls?.length) return null;

  return (
    <>
      <section className="mt-10">
        <div className="mb-4 flex items-baseline justify-between">
          <h2 className="font-mono text-sm font-bold tracking-tight text-ink">
            预览 · {urls.length} 张
          </h2>
          <span className="font-mono text-xs text-muted">点击放大查看</span>
        </div>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
          {urls.map((url, i) => (
            <button
              key={url}
              type="button"
              onClick={() => setOpenIndex(i)}
              className="group relative aspect-[16/9] overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm transition-all hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-pixel"
              aria-label={`查看第 ${i + 1} 张幻灯片`}
            >
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={bust(url)}
                alt={`Slide ${i + 1}`}
                loading="lazy"
                className="absolute inset-0 h-full w-full object-cover"
              />
              <span className="absolute bottom-2 left-2 rounded-[4px] border border-ink/40 bg-ink/85 px-2 py-0.5 font-pixel text-[0.6rem] text-paper backdrop-blur-sm">
                {i + 1}
              </span>
            </button>
          ))}
        </div>
      </section>

      {openIndex !== null && (
        <Lightbox
          urls={urls.map(bust)}
          index={openIndex}
          onClose={() => setOpenIndex(null)}
          onIndexChange={setOpenIndex}
        />
      )}
    </>
  );
}

function Lightbox({
  urls,
  index,
  onClose,
  onIndexChange,
}: {
  urls: string[];
  index: number;
  onClose: () => void;
  onIndexChange: (n: number) => void;
}) {
  const prev = () => onIndexChange(Math.max(0, index - 1));
  const next = () => onIndexChange(Math.min(urls.length - 1, index + 1));

  return (
    <div
      role="dialog"
      aria-modal
      onClick={onClose}
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
        if (e.key === "ArrowLeft") prev();
        if (e.key === "ArrowRight") next();
      }}
      tabIndex={-1}
      className="fixed inset-0 z-50 flex items-center justify-center bg-ink/90 p-8"
    >
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={urls[index]}
        alt={`Slide ${index + 1}`}
        className="max-h-full max-w-full rounded-pixel border-2 border-ink bg-surface"
        onClick={(e) => e.stopPropagation()}
      />
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          prev();
        }}
        disabled={index === 0}
        className={clsx(
          "absolute left-6 top-1/2 -translate-y-1/2 rounded-pixel border-2 border-white/30 bg-white/10 p-3 font-mono text-white backdrop-blur",
          index === 0 ? "opacity-30" : "hover:border-white/60 hover:bg-white/20",
        )}
        aria-label="Previous slide"
      >
        ←
      </button>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          next();
        }}
        disabled={index === urls.length - 1}
        className={clsx(
          "absolute right-6 top-1/2 -translate-y-1/2 rounded-pixel border-2 border-white/30 bg-white/10 p-3 font-mono text-white backdrop-blur",
          index === urls.length - 1 ? "opacity-30" : "hover:border-white/60 hover:bg-white/20",
        )}
        aria-label="Next slide"
      >
        →
      </button>
      <button
        type="button"
        onClick={onClose}
        className="absolute right-6 top-6 rounded-pixel border-2 border-white/30 bg-white/10 p-2 font-mono text-white backdrop-blur hover:border-white/60 hover:bg-white/20"
        aria-label="Close preview"
      >
        ✕
      </button>
      <div className="absolute bottom-6 left-1/2 -translate-x-1/2 rounded-pixel border-2 border-white/30 bg-white/10 px-4 py-1 font-mono text-sm text-white backdrop-blur">
        {index + 1} / {urls.length}
      </div>
    </div>
  );
}
