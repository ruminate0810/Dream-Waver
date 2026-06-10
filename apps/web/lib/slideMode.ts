"use client";

import { useEffect, useState } from "react";

// The slide-generation path the user picks at create time:
//   svg   — flagship: every slide is a bespoke ppt-master-grade <svg>
//           (gradients, vector icons, computed charts). Best visuals. Default.
//   agent — ToolCallAgent over templated HTML layouts: chat-editable, wizard
//           + per-slide edits, richer conversation, lower visual ceiling.
export type SlideMode = "svg" | "agent";

const KEY = "dw_slide_mode";
export const DEFAULT_SLIDE_MODE: SlideMode = "svg";

// useSlideMode is a tiny persisted preference shared by the home Hero and
// /slides/new so a pick on one surface carries to the other. SSR-safe:
// renders the default on the server, hydrates from localStorage on mount
// (a one-frame correction, no layout shift since the toggle is small).
export function useSlideMode(): [SlideMode, (m: SlideMode) => void] {
  const [mode, set] = useState<SlideMode>(DEFAULT_SLIDE_MODE);

  useEffect(() => {
    try {
      const v = localStorage.getItem(KEY);
      if (v === "agent" || v === "svg") set(v);
    } catch {
      /* localStorage blocked (private mode / SSR) — keep the default */
    }
  }, []);

  const setMode = (m: SlideMode) => {
    set(m);
    try {
      localStorage.setItem(KEY, m);
    } catch {
      /* ignore */
    }
  };

  return [mode, setMode];
}
