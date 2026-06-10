// Pixel sprite library — hand-authored SVG mascots/icons drawn on an
// integer grid with crisp edges (no anti-aliasing). Self-contained (colors
// baked in) so they drop in anywhere. Scale freely; they stay sharp.
//
//   <Sprite name="robot" size={42} />
//
// Add a new sprite: pick a viewBox, add it to VIEWBOX, add a case to body().

import clsx from "clsx";

export type SpriteName =
  | "boombox"
  | "robot"
  | "designer"
  | "typewriter"
  | "clapper"
  | "folder"
  | "file"
  | "video-file"
  | "image-file"
  | "audio-file";

const VIEWBOX: Record<SpriteName, [number, number]> = {
  boombox: [20, 12],
  robot: [16, 16],
  designer: [16, 16],
  typewriter: [16, 16],
  clapper: [16, 16],
  folder: [12, 12],
  file: [12, 12],
  "video-file": [12, 12],
  "image-file": [12, 12],
  "audio-file": [12, 12],
};

const INK = "#16140f";
const ACCENT = "#6a55ff";

export function Sprite({
  name,
  size = 32,
  className,
}: {
  name: SpriteName;
  size?: number;
  className?: string;
}) {
  const [vw, vh] = VIEWBOX[name];
  const width = Math.round((size * vw) / vh);
  return (
    <svg
      className={clsx("pixel-edges", className)}
      width={width}
      height={size}
      viewBox={`0 0 ${vw} ${vh}`}
      fill="none"
      role="img"
      aria-hidden="true"
    >
      {body(name)}
    </svg>
  );
}

function body(name: SpriteName) {
  switch (name) {
    case "boombox":
      return (
        <>
          <rect x="6" y="0" width="8" height="1" fill={INK} />
          <rect x="6" y="1" width="1" height="1" fill={INK} />
          <rect x="13" y="1" width="1" height="1" fill={INK} />
          <rect x="1" y="2" width="18" height="9" fill={INK} />
          <rect x="2" y="3" width="16" height="7" fill="#2a2620" />
          <rect x="3" y="4" width="4" height="4" fill="#4a4439" />
          <rect x="4" y="5" width="2" height="2" fill={ACCENT} />
          <rect x="13" y="4" width="4" height="4" fill="#4a4439" />
          <rect x="14" y="5" width="2" height="2" fill={ACCENT} />
          <rect x="9" y="4" width="2" height="1" fill="#d9d3c5" />
          <rect x="9" y="6" width="2" height="1" fill="#d9d3c5" />
          <rect x="9" y="8" width="2" height="1" fill={ACCENT} />
        </>
      );
    case "robot":
      return (
        <>
          <rect x="7" y="0" width="2" height="2" fill={ACCENT} />
          <rect x="7" y="2" width="2" height="1" fill={INK} />
          <rect x="2" y="3" width="12" height="8" fill={INK} />
          <rect x="3" y="4" width="10" height="6" fill="#dfe6f2" />
          <rect x="1" y="6" width="1" height="3" fill={INK} />
          <rect x="14" y="6" width="1" height="3" fill={INK} />
          <rect x="5" y="6" width="2" height="2" fill={ACCENT} />
          <rect x="9" y="6" width="2" height="2" fill={ACCENT} />
          <rect x="5" y="9" width="6" height="1" fill={INK} />
          <rect x="4" y="11" width="8" height="4" fill={INK} />
          <rect x="5" y="12" width="6" height="2" fill="#c9d2e2" />
        </>
      );
    case "designer":
      return (
        <>
          <rect x="3" y="2" width="9" height="2" fill={ACCENT} />
          <rect x="11" y="1" width="2" height="1" fill={ACCENT} />
          <rect x="3" y="4" width="9" height="7" fill={INK} />
          <rect x="4" y="4" width="7" height="6" fill="#ffded1" />
          <rect x="5" y="6" width="1" height="2" fill={INK} />
          <rect x="8" y="6" width="1" height="2" fill={INK} />
          <rect x="5" y="8" width="1" height="1" fill="#ff9d9d" />
          <rect x="9" y="8" width="1" height="1" fill="#ff9d9d" />
          <rect x="6" y="9" width="3" height="1" fill={INK} />
          <rect x="4" y="11" width="7" height="4" fill={ACCENT} />
          <rect x="5" y="11" width="5" height="1" fill="#fff" />
          <rect x="12" y="8" width="1" height="4" fill="#a9742b" />
          <rect x="12" y="7" width="2" height="1" fill="#ff8a8a" />
        </>
      );
    case "typewriter":
      return (
        <>
          <rect x="5" y="0" width="6" height="4" fill="#fbfaf2" />
          <rect x="6" y="2" width="4" height="1" fill="#c9c3b5" />
          <rect x="2" y="4" width="12" height="2" fill="#403a30" />
          <rect x="1" y="6" width="14" height="6" fill={INK} />
          <rect x="2" y="6" width="12" height="5" fill="#4a4439" />
          <rect x="3" y="8" width="2" height="1" fill="#d9d3c5" />
          <rect x="6" y="8" width="2" height="1" fill="#d9d3c5" />
          <rect x="9" y="8" width="2" height="1" fill="#d9d3c5" />
          <rect x="4" y="10" width="2" height="1" fill="#d9d3c5" />
          <rect x="7" y="10" width="2" height="1" fill={ACCENT} />
          <rect x="10" y="10" width="2" height="1" fill="#d9d3c5" />
          <rect x="1" y="12" width="14" height="1" fill={INK} />
        </>
      );
    case "clapper":
      return (
        <>
          <rect x="2" y="2" width="12" height="2" fill={INK} />
          <rect x="3" y="2" width="1" height="2" fill="#fff" />
          <rect x="5" y="2" width="1" height="2" fill="#fff" />
          <rect x="7" y="2" width="1" height="2" fill="#fff" />
          <rect x="9" y="2" width="1" height="2" fill="#fff" />
          <rect x="11" y="2" width="1" height="2" fill="#fff" />
          <rect x="2" y="4" width="12" height="9" fill={INK} />
          <rect x="3" y="5" width="10" height="7" fill="#2a2620" />
          <rect x="4" y="7" width="8" height="1" fill="#6b6557" />
          <rect x="4" y="9" width="6" height="1" fill="#6b6557" />
          <rect x="11" y="10" width="1" height="1" fill={ACCENT} />
        </>
      );
    case "folder":
      return (
        <>
          <rect x="1" y="3" width="10" height="7" fill={ACCENT} />
          <rect x="1" y="2" width="5" height="2" fill={ACCENT} />
          <rect x="2" y="4" width="8" height="5" fill="#ece8ff" />
        </>
      );
    case "file":
      return (
        <>
          <rect x="2" y="1" width="6" height="10" fill="#fff" stroke={INK} strokeWidth="1" />
          <rect x="7" y="1" width="3" height="3" fill={INK} />
          <rect x="3" y="5" width="4" height="1" fill="#c9c3b5" />
          <rect x="3" y="7" width="4" height="1" fill="#c9c3b5" />
        </>
      );
    case "video-file":
      return (
        <>
          <rect x="1" y="3" width="10" height="7" fill={INK} />
          <rect x="2" y="4" width="8" height="5" fill="#71384a" />
          <rect x="3" y="6" width="3" height="2" fill="#ff8a8a" />
        </>
      );
    case "image-file":
      return (
        <>
          <rect x="2" y="2" width="8" height="8" fill="#fff" stroke={INK} strokeWidth="1" />
          <rect x="4" y="4" width="4" height="4" fill={ACCENT} />
        </>
      );
    case "audio-file":
      return (
        <>
          <rect x="7" y="1" width="1.4" height="6" fill={INK} />
          <rect x="3" y="7" width="3" height="2" fill="#3ea96a" />
          <rect x="7" y="6" width="2" height="1.4" fill="#3ea96a" />
        </>
      );
  }
}
