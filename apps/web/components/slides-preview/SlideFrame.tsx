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
  focusTick,
  clearActiveTick,
  successTick,
  onEdit,
  numberLabel,
}: {
  jobId: string;
  index: number;
  version: number;
  // `active` is the "just updated" highlight — vermillion border pulse
  // for ~1.5s after slides.updated lands.
  active?: boolean;
  // `focusTick` bumps when the parent wants this frame to scroll into
  // view. Different value from prior render → smoothly scroll to centre.
  // Decoupled from `active` so we can scroll without highlight (e.g.
  // on initial mount of a new add_slide frame).
  focusTick?: number;
  // clearActiveTick bumps → post dw-clear-active to the iframe so it
  // releases the .__dw-active highlight on the clicked element.
  // (Triggered when the EditPopover closes.)
  clearActiveTick?: number;
  // successTick bumps → post dw-edit-success to the iframe so the
  // clicked element gets a 700ms green flash before clearing.
  // (Triggered when slides.updated lands for a pending submission.)
  successTick?: number;
  onEdit?: (req: EditRequest, anchorRect: DOMRect) => void;
  numberLabel?: string;
}) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const [scale, setScale] = useState(1);
  const [loaded, setLoaded] = useState(false);
  // Sprint U2.2 — detect iframe load failures (404 from missing page
  // HTML, network drop, orchestrator restart mid-edit). The iframe's
  // onLoad fires even on chi's 404 page so we sample contentDocument
  // for the "404 page not found" body marker. Bumping retryNonce
  // re-renders the iframe with a fresh cache-bust.
  const [loadError, setLoadError] = useState(false);
  const [retryNonce, setRetryNonce] = useState(0);
  // Sprint AF.4 — auto-retry counter. 404s during initial render are
  // common (Phase 3 generates slides in order; iframe fetches before
  // each is done). Auto-retry 3 times at 1s / 3s / 8s before showing
  // the human-visible fallback card — most 404s self-heal within ~3s.
  const [autoRetries, setAutoRetries] = useState(0);
  const AUTO_RETRY_MAX = 3;
  const AUTO_RETRY_DELAYS = [1000, 3000, 8000];
  // Track the last tick value of each control channel so its useEffect
  // only fires when the parent intentionally bumps it (not on every
  // unrelated render).
  const lastFocusTick = useRef<number | undefined>(focusTick);
  const lastClearTick = useRef<number | undefined>(clearActiveTick);
  const lastSuccessTick = useRef<number | undefined>(successTick);

  // Sprint AF.4 — auto-retry on load failure. When loadError flips
  // true AND we haven't exhausted AUTO_RETRY_MAX, schedule a retry
  // with exponentially-growing delay (1s / 3s / 8s). The retry bumps
  // retryNonce which re-mounts the iframe with a fresh cache-bust.
  // Most "404 page not found" responses self-heal within 3s as
  // chromedp finishes rendering the next slide. Only after exhausting
  // retries does the user see the manual-retry fallback card.
  useEffect(() => {
    if (!loadError) return;
    if (autoRetries >= AUTO_RETRY_MAX) return;
    const delay = AUTO_RETRY_DELAYS[autoRetries] ?? 8000;
    const id = setTimeout(() => {
      setLoadError(false);
      setLoaded(false);
      setRetryNonce((n) => n + 1);
      setAutoRetries((n) => n + 1);
    }, delay);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadError, autoRetries]);

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

  // Scroll into view when the parent bumps focusTick. We use 'center'
  // block alignment so the iframe lands in the middle of the viewport,
  // not awkwardly at the top edge. The lastFocusTick guard means
  // mount + ResizeObserver re-renders don't accidentally re-scroll.
  useEffect(() => {
    if (focusTick === undefined) return;
    if (focusTick === lastFocusTick.current) return;
    lastFocusTick.current = focusTick;
    hostRef.current?.scrollIntoView({ behavior: "smooth", block: "center" });
  }, [focusTick]);

  // Forward parent's clear/success ticks to the iframe via contentWindow
  // postMessage. The bridge script (see slideBridgeScriptTpl) listens
  // for these and toggles the .__dw-active / .__dw-success classes.
  useEffect(() => {
    if (clearActiveTick === undefined) return;
    if (clearActiveTick === lastClearTick.current) return;
    lastClearTick.current = clearActiveTick;
    iframeRef.current?.contentWindow?.postMessage({ type: "dw-clear-active" }, "*");
  }, [clearActiveTick]);

  useEffect(() => {
    if (successTick === undefined) return;
    if (successTick === lastSuccessTick.current) return;
    lastSuccessTick.current = successTick;
    iframeRef.current?.contentWindow?.postMessage({ type: "dw-edit-success" }, "*");
  }, [successTick]);

  // The URL bakes the version in so the browser fetches a fresh response
  // on every edit. We also remount via key={version} for paranoia — some
  // browsers cache HTML iframes aggressively even with no-store headers.
  // Sprint U2.2 — retryNonce adds an additional cache-bust segment so a
  // user-clicked "retry" actually re-fetches.
  const src = `/api/v1/slides/${jobId}/page/${index}.html?v=${version}&r=${retryNonce}`;

  return (
    <div
      ref={hostRef}
      className={clsx(
        "relative w-full overflow-hidden border bg-white",
        "transition-all duration-300",
        active
          ? // "Just revised" state — vermillion border + soft outer glow
            // for ~1.5s. The animate-pulse-once keyframe (in globals.css)
            // gives it a single attention-grabbing tick instead of a
            // continuous loop which would feel anxious.
            "border-[color:var(--vermillion)] shadow-[0_0_0_4px_rgba(181,55,30,0.18),0_18px_30px_-22px_rgba(181,55,30,0.35)] animate-[dw-frame-pulse_1400ms_ease-out_1]"
          : "border-[color:var(--rule)] shadow-[0_1px_0_rgba(26,22,20,0.04),0_18px_30px_-22px_rgba(26,22,20,0.18)]",
      )}
      style={{ aspectRatio: "16 / 9" }}
    >
      {/* Soft skeleton while the iframe loads its (CDN-backed) Tailwind +
          fonts. The host's background colour matches our paper canvas
          rather than going hard white so the fade-in feels seamless.
          Sprint AF.4 — copy upgraded from generic "Composing ##" to a
          warmer "正在排版第 ## 页…" that matches the editorial voice. */}
      {!loaded && !loadError && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-1.5 bg-[color:var(--paper)]/40">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--vermillion)]">
            § {String(index).padStart(2, "0")}
          </span>
          <span className="font-display text-[13px] italic text-[color:var(--ink-soft)]">
            正在排版第 {index} 页…
          </span>
        </div>
      )}
      {/* Auto-retry intermediate state — keep the same skeleton voice
          but signal the retry attempt so the user knows we're working. */}
      {loadError && autoRetries < AUTO_RETRY_MAX && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-1.5 bg-[color:var(--paper)]/60">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--vermillion)]/70">
            § {String(index).padStart(2, "0")} · 等待 agent
          </span>
          <span className="font-display text-[13px] italic text-[color:var(--ink-soft)]">
            还在渲染中, 再等一下…
          </span>
          <span className="font-mono-jb text-[9px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
            attempt {autoRetries + 1}/{AUTO_RETRY_MAX}
          </span>
        </div>
      )}

      <iframe
        ref={iframeRef}
        // key forces a real DOM remount on version bumps so cached pages
        // never linger after an edit. retryNonce changes ALSO bump key
        // so a user-clicked retry escapes any in-memory render cache.
        key={`${version}-${retryNonce}`}
        src={src}
        onLoad={(e) => {
          setLoaded(true);
          // Sprint U2.2 — sniff the body text to distinguish a real
          // slide render from various error responses chi / our own
          // handler can return as text-or-JSON instead of HTML.
          // Browsers don't fire onerror for HTTP-status failures
          // (only transport failures), so body inspection is the
          // only path.
          //
          // Known error bodies:
          //   "404 page not found"             — chi default 404
          //   `{"error":"job not found", ...}` — handler errorJSON
          //   `{"error":"deck not ready", ...}`— Phase 3 not finished
          //   `{"error":"slide index out of range", ...}`
          //   `{"error":"live preview not configured", ...}`
          // The shape detector: any body that's pure text (no
          // <html>/<head>/<body> structure beyond chi's empty doc)
          // AND contains either "404 page not found" or starts with
          // `{"error":` is treated as a load failure.
          try {
            const doc = (e.target as HTMLIFrameElement).contentDocument;
            const text = (doc?.body?.textContent ?? "").trim();
            const isChi404 = text.startsWith("404 page not found");
            const isErrJSON =
              text.startsWith("{") &&
              text.includes('"ok":false') &&
              text.includes('"error":');
            if (isChi404 || isErrJSON) {
              // Sprint AF.4 — don't IMMEDIATELY show the fallback card.
              // Schedule an auto-retry (effect below handles the delay).
              // Only after AUTO_RETRY_MAX failures does loadError stick
              // and the fallback card render.
              setLoadError(true);
            } else {
              setLoadError(false);
              setAutoRetries(0);
            }
          } catch {
            // Cross-origin would throw — but sandbox allow-same-origin
            // means we shouldn't hit this. Defensive no-op.
          }
        }}
        onError={() => setLoadError(true)}
        title={`Slide ${index}`}
        // Sandbox: allow scripts (we need the bridge) but disable
        // navigation/forms/popups/storage. Same-origin needed for the
        // postMessage payload to include trusted bbox numbers.
        sandbox="allow-scripts allow-same-origin"
        // Sprint Z.4 — fade iframe in once it actually has content.
        // Starts at opacity 0 so the "Composing ##" skeleton (above)
        // is what the user sees while the iframe is fetching;
        // crossfades to the rendered slide over var(--dur-frame-load).
        // loadError keeps it hidden so the error fallback (below)
        // takes the visual stage uncontested.
        className="absolute left-0 top-0 origin-top-left border-0 transition-opacity"
        style={{
          width: 1920,
          height: 1080,
          transform: `scale(${scale})`,
          opacity: loaded && !loadError ? 1 : 0,
          transitionDuration: "var(--dur-frame-load)",
          transitionTimingFunction: "var(--ease-entrance)",
        }}
      />

      {/* Sprint U2.2 — visible fallback when the iframe 404s. Without
          this the user sees a blank white frame and assumes the whole
          deck broke. The retry button bumps retryNonce so the iframe
          re-fetches with a fresh cache-bust segment. Sprint AF.4 —
          only shown AFTER auto-retries are exhausted; before that we
          show the softer "等待 agent" placeholder. */}
      {loadError && autoRetries >= AUTO_RETRY_MAX ? (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 border border-dashed border-[color:var(--vermillion)]/40 bg-[#FBF9F2]/95 p-6 text-center">
          <span className="font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--vermillion)]">
            Slide {String(index).padStart(2, "0")} · 加载失败
          </span>
          <p className="font-display text-[14px] italic leading-snug text-[color:var(--ink-soft)]">
            这页暂时拿不到 — 可能是后台刚重启或缓存过期。
          </p>
          <button
            type="button"
            onClick={() => {
              // Reset the auto-retry counter so a manual click starts
              // a fresh cycle if it still fails.
              setLoadError(false);
              setLoaded(false);
              setRetryNonce((n) => n + 1);
              setAutoRetries(0);
            }}
            className="border border-[color:var(--ink)] bg-[color:var(--ink)] px-3 py-1.5 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--paper)] transition-all hover:bg-[color:var(--vermillion)]"
          >
            重试
          </button>
        </div>
      ) : null}

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
