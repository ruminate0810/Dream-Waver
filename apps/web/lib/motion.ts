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

import { gsap } from "gsap";

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
