"use client";

import { useEffect, useRef, useState } from "react";
import clsx from "clsx";

// SlideFrame hosts one slide as a live, click-to-edit iframe.
//
// The orchestrator serves the slide HTML at 1920×1080 (the same canvas
// chromedp screenshots for the PPTX). We don't want to render that at
// natural size — too big for any reasonable layout — so we wrap the
// iframe in an aspect-ratio container and apply `transform: scale(N)`
// where N is computed from the container's actual width. A ResizeObserver
// keeps the scale honest if the page wraps.
//
// The iframe HTML carries a small bridge script (injected server-side in
// routes_slides.go::slideBridgeScriptTpl) that posts an `dw-edit-request`
// message to window.parent whenever the user clicks a text element.
// We listen for it and call onEdit so the parent can open an EditPopover.

export type EditRequest = {
  slideIndex: number;
  text: string;
  role: string; // "title" | "subtitle" | "bullet" | "text" | custom
  // Coordinates inside the iframe's own 1920-wide viewport (not scaled).
  bbox: {
    left: number;
    top: number;
    right: number;
    bottom: number;
    width: number;
    height: number;
  };
};

export function SlideFrame({
  jobId,
  index, // 1-based
  version,
  active,
  onEdit,
  numberLabel,
}: {
  jobId: string;
  index: number;
  version: number;
  active?: boolean;
  onEdit?: (req: EditRequest, anchorRect: DOMRect) => void;
  numberLabel?: string;
}) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const [scale, setScale] = useState(1);
  const [loaded, setLoaded] = useState(false);

  // Compute (and keep current) the scale that maps the 1920px-wide slide
  // canvas onto whatever pixel width the host occupies. ResizeObserver
  // handles page-load fonts, sidebar collapses, and window resizes alike.
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const measure = () => {
      const w = host.getBoundingClientRect().width;
      if (w > 0) setScale(w / 1920);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(host);
    return () => ro.disconnect();
  }, []);

  // Listen for click→edit messages coming from any of our iframes. We
  // filter by slideIndex so each SlideFrame only handles its own events,
  // and translate iframe-local bbox coords into parent-document coords
  // (so the popover can anchor in the right spot).
  useEffect(() => {
    if (!onEdit) return;
    const handler = (e: MessageEvent) => {
      const d = e.data;
      if (!d || d.type !== "dw-edit-request" || d.slideIndex !== index) return;
      const host = hostRef.current;
      if (!host) return;
      const hostRect = host.getBoundingClientRect();
      // Local iframe coordinates (pre-scale) → parent-document coords.
      const anchor = new DOMRect(
        hostRect.left + d.bbox.left * scale,
        hostRect.top + d.bbox.top * scale,
        d.bbox.width * scale,
        d.bbox.height * scale,
      );
      onEdit(d as EditRequest, anchor);
    };
    window.addEventListener("message", handler);
    return () => window.removeEventListener("message", handler);
  }, [index, onEdit, scale]);

  // The URL bakes the version in so the browser fetches a fresh response
  // on every edit. We also remount via key={version} for paranoia — some
  // browsers cache HTML iframes aggressively even with no-store headers.
  const src = `/api/v1/slides/${jobId}/page/${index}.html?v=${version}`;

  return (
    <div
      ref={hostRef}
      className={clsx(
        "relative w-full overflow-hidden border border-[color:var(--rule)] bg-white shadow-[0_1px_0_rgba(26,22,20,0.04),0_18px_30px_-22px_rgba(26,22,20,0.18)]",
        "transition-shadow duration-300",
        active && "ring-1 ring-[color:var(--vermillion)]/30 shadow-[0_0_0_4px_rgba(181,55,30,0.06)]",
      )}
      style={{ aspectRatio: "16 / 9" }}
    >
      {/* Soft skeleton while the iframe loads its (CDN-backed) Tailwind +
          fonts. The host's background colour matches our paper canvas
          rather than going hard white so the fade-in feels seamless. */}
      {!loaded && (
        <div className="absolute inset-0 flex items-center justify-center bg-[color:var(--paper)]/40">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
            Composing · {String(index).padStart(2, "0")}
          </span>
        </div>
      )}

      <iframe
        ref={iframeRef}
        // key forces a real DOM remount on version bumps so cached pages
        // never linger after an edit.
        key={version}
        src={src}
        onLoad={() => setLoaded(true)}
        title={`Slide ${index}`}
        // Sandbox: allow scripts (we need the bridge) but disable
        // navigation/forms/popups/storage. Same-origin needed for the
        // postMessage payload to include trusted bbox numbers.
        sandbox="allow-scripts allow-same-origin"
        className="absolute left-0 top-0 origin-top-left border-0"
        style={{
          width: 1920,
          height: 1080,
          transform: `scale(${scale})`,
        }}
      />

      {/* Page chip — bottom-left. Hairline rule + mono caps; vermillion
          accent when the slide just updated (active prop). */}
      {numberLabel ? (
        <span
          className={clsx(
            "pointer-events-none absolute bottom-3 left-3 inline-flex items-center gap-2 rounded-sm border bg-white/80 px-2 py-1 font-mono-jb text-[10px] uppercase tracking-[0.24em] backdrop-blur-sm",
            active
              ? "border-[color:var(--vermillion)]/40 text-[color:var(--vermillion)]"
              : "border-[color:var(--rule)] text-[color:var(--ink-soft)]",
          )}
        >
          <span
            className={clsx(
              "h-1 w-1 rounded-full",
              active ? "bg-[color:var(--vermillion)] animate-pulse" : "bg-[color:var(--ink-faint)]",
            )}
          />
          {numberLabel}
        </span>
      ) : null}
    </div>
  );
}
