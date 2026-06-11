"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { X, Maximize2, Minimize2 } from "lucide-react";
import clsx from "clsx";

// PixelWindow is a draggable, maximizable OS-style window that floats over
// the office scene (the office = desktop, this = an app window). Positioned
// in px within the nearest relative parent; the title bar drags, the buttons
// maximize/close. Pointer events (works for mouse + touch).
export function PixelWindow({
  title,
  onClose,
  onFocus,
  z,
  children,
  initial = { left: 0.06, top: 0.05, width: 0.88, height: 0.88 }, // fractions of parent
}: {
  title: string;
  onClose: () => void;
  onFocus: () => void;
  z: number;
  children: React.ReactNode;
  initial?: { left: number; top: number; width: number; height: number };
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [rect, setRect] = useState<{ left: number; top: number; width: number; height: number } | null>(null);
  const [maxed, setMaxed] = useState(false);
  const drag = useRef<{ startX: number; startY: number; left: number; top: number } | null>(null);

  // Resolve the fractional initial geometry against the parent once mounted.
  useEffect(() => {
    const parent = ref.current?.parentElement;
    if (!parent) return;
    const pw = parent.clientWidth;
    const ph = parent.clientHeight;
    setRect({
      left: Math.round(pw * initial.left),
      top: Math.round(ph * initial.top),
      width: Math.round(pw * initial.width),
      height: Math.round(ph * initial.height),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      onFocus();
      if (maxed || !rect) return;
      drag.current = { startX: e.clientX, startY: e.clientY, left: rect.left, top: rect.top };
      (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
    },
    [maxed, rect, onFocus],
  );

  const onPointerMove = useCallback(
    (e: React.PointerEvent) => {
      if (!drag.current || !rect) return;
      const parent = ref.current?.parentElement;
      if (!parent) return;
      const dx = e.clientX - drag.current.startX;
      const dy = e.clientY - drag.current.startY;
      const left = Math.min(Math.max(drag.current.left + dx, -rect.width * 0.5), parent.clientWidth - 60);
      const top = Math.min(Math.max(drag.current.top + dy, 0), parent.clientHeight - 40);
      setRect((r) => (r ? { ...r, left, top } : r));
    },
    [rect],
  );

  const endDrag = useCallback(() => {
    drag.current = null;
  }, []);

  if (!rect) return <div ref={ref} className="hidden" />;

  return (
    <div
      ref={ref}
      className="absolute flex flex-col overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel"
      style={
        maxed
          ? { inset: 8, zIndex: z }
          : { left: rect.left, top: rect.top, width: rect.width, height: rect.height, zIndex: z }
      }
      onPointerDown={onFocus}
    >
      {/* title bar — drag handle */}
      <div
        className={clsx(
          "flex flex-none touch-none select-none items-center gap-2 border-b-2 border-ink bg-surface-2 px-3 py-2",
          !maxed && "cursor-grab active:cursor-grabbing",
        )}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      >
        <span className="flex gap-1.5">
          <i className="h-2.5 w-2.5 rounded-full border-[1.5px] border-ink bg-[#ff8a8a]" />
          <i className="h-2.5 w-2.5 rounded-full border-[1.5px] border-ink bg-gold" />
          <i className="h-2.5 w-2.5 rounded-full border-[1.5px] border-ink bg-grass" />
        </span>
        <span className="ml-1 font-pixel text-[0.58rem] tracking-wide text-accent">{title}</span>
        <span className="ml-auto flex items-center gap-1.5">
          <button
            type="button"
            onClick={() => setMaxed((m) => !m)}
            className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface text-ink-2 shadow-pixel-sm transition-transform hover:-translate-y-0.5 hover:text-ink"
            aria-label={maxed ? "还原" : "最大化"}
          >
            {maxed ? <Minimize2 size={11} strokeWidth={2.2} /> : <Maximize2 size={11} strokeWidth={2.2} />}
          </button>
          <button
            type="button"
            onClick={onClose}
            className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface text-ink-2 shadow-pixel-sm transition-transform hover:-translate-y-0.5 hover:text-ink"
            aria-label="关闭"
          >
            <X size={12} strokeWidth={2.4} />
          </button>
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">{children}</div>
    </div>
  );
}
