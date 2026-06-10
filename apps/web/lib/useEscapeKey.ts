"use client";

import { useEffect } from "react";

// useEscapeKey — invoke `onEscape` when Escape is pressed while `active`.
// For closing popovers / modals / overlays from the keyboard. The listener
// only attaches while active, so it doesn't intercept Esc elsewhere.
export function useEscapeKey(active: boolean, onEscape: () => void) {
  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onEscape();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [active, onEscape]);
}
