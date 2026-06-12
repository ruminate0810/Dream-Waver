"use client";

import { useEffect, useRef, useState } from "react";
import { Rnd } from "react-rnd";
import { X, Maximize2, Minimize2 } from "lucide-react";
import clsx from "clsx";

// PixelWindow is a draggable + resizable, maximizable OS-style window that
// floats over the office scene (office = desktop, this = an app window). The
// drag/resize/bounds math is delegated to react-rnd; we keep the pixel chrome
// and the maximize toggle. Geometry is px within the nearest positioned parent.
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
  const rnd = useRef<Rnd>(null);
  const [geo, setGeo] = useState<{ x: number; y: number; width: number; height: number } | null>(null);
  const [maxed, setMaxed] = useState(false);
  // geometry remembered before maximize, so 还原 returns to where it was
  const restore = useRef<{ x: number; y: number; width: number; height: number } | null>(null);

  // Resolve fractional initial geometry against the parent once mounted.
  useEffect(() => {
    const p = rnd.current?.getParentSize();
    if (!p) return;
    setGeo({
      x: Math.round(p.width * initial.left),
      y: Math.round(p.height * initial.top),
      width: Math.round(p.width * initial.width),
      height: Math.round(p.height * initial.height),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const toggleMax = () => {
    const p = rnd.current?.getParentSize();
    if (!p) return;
    if (maxed) {
      setGeo(restore.current ?? geo);
      setMaxed(false);
    } else {
      restore.current = geo;
      setGeo({ x: 8, y: 8, width: p.width - 16, height: p.height - 16 });
      setMaxed(true);
    }
  };

  if (!geo) return <Rnd ref={rnd} default={{ x: 0, y: 0, width: 0, height: 0 }} className="hidden" />;

  return (
    <Rnd
      ref={rnd}
      size={{ width: geo.width, height: geo.height }}
      position={{ x: geo.x, y: geo.y }}
      onDragStop={(_e, d) => setGeo((g) => (g ? { ...g, x: d.x, y: d.y } : g))}
      onResizeStop={(_e, _dir, el, _delta, pos) =>
        setGeo({ x: pos.x, y: pos.y, width: el.offsetWidth, height: el.offsetHeight })
      }
      bounds="parent"
      minWidth={260}
      minHeight={180}
      dragHandleClassName="pw-drag"
      disableDragging={maxed}
      enableResizing={!maxed}
      // react-rnd hardcodes inline `display: inline-block` on its root, which
      // beats the `flex` class and silently collapses the whole height chain
      // (flex-1 → h-full → overflow-y-auto) — long window content was clipped
      // unscrollably. User style spreads LAST in rnd's merge, so flex here wins.
      style={{ zIndex: z, display: "flex", flexDirection: "column" }}
      onMouseDown={onFocus}
      className="overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel"
    >
      {/* title bar — drag handle (pw-drag) */}
      <div
        className={clsx(
          "pw-drag flex flex-none touch-none select-none items-center gap-2 border-b-2 border-ink bg-surface-2 px-3 py-2",
          !maxed && "cursor-grab active:cursor-grabbing",
        )}
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
            onClick={toggleMax}
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
    </Rnd>
  );
}
