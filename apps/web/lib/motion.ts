// Sprint Y1 — shared GSAP motion primitives.
//
// Centralises eases, durations, and the prefers-reduced-motion guard
// so every animated component agrees on the same vocabulary. Per the
// gsap-react skill: components import useGSAP from @gsap/react and
// the constants below — never duplicate magic numbers.
//
// Why a separate file (not inline per component):
//   - One place to tune the "feel" of the whole product.
//   - Reduced-motion gate is wired once, not 6 times across surfaces.
//   - Future "evening edition" (dark mode) variations can swap eases
//     without touching every component.

import { useEffect, useRef, useState, type RefObject } from "react";
import { gsap } from "gsap";
import { useGSAP } from "@gsap/react";

// ─── Shared eases ─────────────────────────────────────────────────
//
// "Out" curves for entrances — the object decelerates as it arrives,
// reading as material settling rather than appearing. "InOut" for
// reversible micro-interactions where the motion should feel
// symmetric on the way back.

export const EASE = {
  /** Editorial entrance — material gently settling. Default for
   *  fade+rise patterns on Hero / cards / login. */
  entrance: "power3.out",
  /** Quick UI feedback — button press, tile hover. Short + crisp. */
  feedback: "power2.out",
  /** Magnetic hover — slow pull-in, sharper release. */
  magnetic: "expo.out",
  /** Brand mark breath — symmetric in/out for the looping pulse. */
  breath: "sine.inOut",
} as const;

// ─── Durations (seconds) ──────────────────────────────────────────

export const DUR = {
  /** Big-element entrance (hero title, sidebar slide-in). */
  entrance: 0.6,
  /** Secondary-element entrance (form, grid groups). */
  secondary: 0.5,
  /** Micro-interactions (button press, hover lift). */
  micro: 0.22,
  /** Scroll-revealed cards. */
  reveal: 0.55,
  /** Brand mark breath (full cycle). */
  breath: 4.0,
  // ─── Sprint Z additions ────────────────────────────────────────
  /** Chat card mount (gate cards / pending row / message bubble). */
  card: 0.32,
  /** Card exit before unmount (slightly faster than entry). */
  cardExit: 0.22,
  /** Body collapse/expand (ToolStrip body, ThoughtCollapse). */
  collapse: 0.28,
  /** Status glyph crossfade (pending → running → done). */
  statusFlip: 0.2,
  /** iframe onLoad fade-in. */
  frameLoad: 0.24,
} as const;

// ─── Staggers ─────────────────────────────────────────────────────

export const STAGGER = {
  /** Hero entrance — title → form → support text. */
  hero: 0.08,
  /** Grid tiles (within one group). */
  tile: 0.04,
  /** Recent-decks scroll reveal. */
  card: 0.06,
} as const;

// ─── Reduced-motion helper ────────────────────────────────────────
//
// gsap.matchMedia is the canonical way to honour prefers-reduced-motion
// without sprinkling window.matchMedia checks across every component.
// The pattern from gsap-core: pass two branches, GSAP picks at run-time
// and re-evaluates if the user toggles the OS preference.
//
// Usage inside useGSAP:
//
//   useGSAP(() => {
//     const mm = gsap.matchMedia();
//     mm.add(
//       { full: PREFERS_FULL_MOTION, reduced: PREFERS_REDUCED_MOTION },
//       (ctx) => {
//         if (ctx.conditions?.full) {
//           // full motion timeline
//         } else {
//           // reduced — opacity only, no transforms
//         }
//       },
//     );
//   }, { scope: containerRef });
//
// We export the query strings so every call site agrees on the
// breakpoint names.

export const PREFERS_FULL_MOTION = "(prefers-reduced-motion: no-preference)";
export const PREFERS_REDUCED_MOTION = "(prefers-reduced-motion: reduce)";

// ─── Reasonable defaults applied process-wide ─────────────────────
//
// Set the GSAP global defaults on first import so every tween that
// doesn't explicitly set ease/duration picks up the editorial feel.
// Idempotent — calling set defaults again with the same shape is a
// no-op.
//
// Call exactly once at module init via the side-effect import below.
// Components that want a different ease just pass it on the tween.

let applied = false;
export function applyMotionDefaults() {
  if (applied) return;
  applied = true;
  gsap.defaults({
    ease: EASE.entrance,
    duration: DUR.secondary,
  });
}

// Apply on first import so any subsequent gsap.* call uses our
// defaults. The check above guards against React strict-mode double-
// invocation in dev.
applyMotionDefaults();

// ─── Sprint Z — shared hooks for chat-layer motion ────────────────
//
// useCardMotion: mount-only entrance for cards that DON'T need an
// exit animation. Use on cards that live inside a stable parent
// (ChatThread always rendered, PendingRow conditional but unmounted
// directly). Honors prefers-reduced-motion (opacity-only fallback).
//
// useExitMotion: presence wrapper for cards that NEED an exit
// animation before React unmounts them. The wizard / outline-review /
// clarification cards are the canonical case — they vanish on
// `gate_submitted` and the user benefits from a 220ms farewell before
// the next phase chrome takes over.

type MotionRef = RefObject<HTMLElement | null>;

/**
 * Mount-only entrance for a card. Call from inside the component's
 * body with a ref attached to the outer element. Runs once on mount;
 * cleanup is automatic via useGSAP scoping.
 *
 * @param ref       ref to the outer element
 * @param opts.y    pixel distance to rise from (default 12). Set 0
 *                  to skip translate and keep opacity-only.
 * @param opts.delay seconds to delay the start (default 0). Useful
 *                  when chaining after a parent's entrance.
 */
export function useCardMotion(
  ref: MotionRef,
  opts: { y?: number; delay?: number; duration?: number } = {},
) {
  const { y = 12, delay = 0, duration = DUR.card } = opts;
  useGSAP(
    () => {
      if (!ref.current) return;
      const mm = gsap.matchMedia();
      mm.add(PREFERS_FULL_MOTION, () => {
        gsap.from(ref.current, {
          opacity: 0,
          y,
          duration,
          delay,
          ease: EASE.entrance,
          clearProps: "transform,opacity",
        });
      });
      mm.add(PREFERS_REDUCED_MOTION, () => {
        gsap.from(ref.current, {
          opacity: 0,
          duration: Math.min(duration, 0.2),
          delay: 0,
          ease: "none",
          clearProps: "opacity",
        });
      });
    },
    { scope: ref },
  );
}

/**
 * useExitMotion — returns a `mounted` flag + `nodeRef` that gates
 * React rendering AND a ref to attach to the outer element. When the
 * parent flips `visible` false, we tween the element out then drop
 * mounted=false so React unmounts. Mount entry uses the same
 * useCardMotion semantics.
 *
 * Usage:
 *
 *   const { mounted, ref } = useExitMotion(pending?.kind === "wizard");
 *   if (!mounted) return null;
 *   return <div ref={ref}>...</div>;
 *
 * The hook stays mounted through one extra render cycle while the
 * exit tween plays, then drops the node. Idempotent on rapid
 * visible toggles — kills in-flight tweens before retargeting.
 */
export function useExitMotion(
  visible: boolean,
  opts: { y?: number; duration?: number } = {},
): { mounted: boolean; ref: MotionRef } {
  const { y = -6, duration = DUR.cardExit } = opts;
  const ref = useRef<HTMLElement | null>(null);
  const [mounted, setMounted] = useState(visible);
  // Track latest visibility so the deferred exit fires on the right edge.
  const visibleRef = useRef(visible);
  visibleRef.current = visible;

  useEffect(() => {
    if (visible && !mounted) {
      setMounted(true);
      return;
    }
    if (!visible && mounted) {
      const node = ref.current;
      if (!node) {
        setMounted(false);
        return;
      }
      // Kill any inbound tween + play exit.
      gsap.killTweensOf(node);
      const reduced =
        typeof window !== "undefined" &&
        window.matchMedia(PREFERS_REDUCED_MOTION).matches;
      gsap.to(node, {
        opacity: 0,
        y: reduced ? 0 : y,
        duration: reduced ? Math.min(duration, 0.18) : duration,
        ease: EASE.feedback,
        onComplete: () => {
          // Double-check user didn't flip back to visible mid-exit.
          if (!visibleRef.current) setMounted(false);
        },
      });
    }
  }, [visible, mounted, duration, y]);

  // Mount entrance — same as useCardMotion but inline because we own
  // the ref lifecycle here.
  useGSAP(
    () => {
      if (!ref.current || !mounted) return;
      const mm = gsap.matchMedia();
      mm.add(PREFERS_FULL_MOTION, () => {
        gsap.from(ref.current, {
          opacity: 0,
          y: 12,
          duration: DUR.card,
          ease: EASE.entrance,
          clearProps: "transform,opacity",
        });
      });
      mm.add(PREFERS_REDUCED_MOTION, () => {
        gsap.from(ref.current, {
          opacity: 0,
          duration: 0.18,
          ease: "none",
          clearProps: "opacity",
        });
      });
    },
    { scope: ref, dependencies: [mounted] },
  );

  return { mounted, ref };
}

/**
 * useHeightCollapse — controlled open/close with GSAP height tween.
 * Replaces the native `<details>` element which can't animate. Used
 * by ToolStrip body and ThoughtCollapse.
 *
 * Pattern:
 *   - When `open` flips true: measure scrollHeight → tween to it +
 *     fade content in.
 *   - When `open` flips false: tween height to 0 + fade content out.
 *   - After settle, drop inline height so further content changes
 *     auto-size correctly.
 *
 * Returns a ref to attach to the collapsing element.
 */
export function useHeightCollapse(open: boolean): MotionRef {
  const ref = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    const reduced =
      typeof window !== "undefined" &&
      window.matchMedia(PREFERS_REDUCED_MOTION).matches;

    gsap.killTweensOf(node);

    if (open) {
      // Set to height 0 first if currently 0 / unset, then animate.
      const target = node.scrollHeight;
      gsap.fromTo(
        node,
        { height: 0, opacity: 0 },
        {
          height: target,
          opacity: 1,
          duration: reduced ? 0.15 : DUR.collapse,
          ease: EASE.entrance,
          onComplete: () => {
            // Drop inline height so subsequent content changes
            // resize naturally.
            node.style.height = "auto";
          },
        },
      );
    } else {
      const current = node.scrollHeight || node.getBoundingClientRect().height;
      gsap.fromTo(
        node,
        { height: current, opacity: 1 },
        {
          height: 0,
          opacity: 0,
          duration: reduced ? 0.12 : DUR.collapse * 0.85,
          ease: EASE.feedback,
        },
      );
    }
  }, [open]);

  return ref;
}
