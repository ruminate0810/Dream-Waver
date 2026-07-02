"use client";

import clsx from "clsx";

import { INK, SKIN, type WorkerDef } from "./workers";

// ChibiWorker — the cute pixel worker. Two motion systems, each picked for what
// it does best:
//   • WALK  → a true frame strip: hand-posed cells snapped with steps() (no
//     tweening), which is what gives pixel walks their crisp, natural read.
//   • POSED (idle / sit / any gesture) → a single drawing whose head/arms/body
//     are classed groups, animated with light CSS secondary motion (head turns,
//     nods, waves, typing, breath). Secondary motion is exactly where CSS
//     shines — it adds 灵动 without the rubber-band feel that made the old
//     all-CSS walk look fake.
// Front-facing chibi (big head, sparkle eyes, blush); left/right via flip.

type Leg = "plant" | "lift";
type Pose = { bob: number; dx: number; legL: Leg; legR: Leg; armL: number; armR: number };

// Walk = a 4-frame bouncy waddle: lift-left → plant → lift-right → plant. The
// figure rises on the lift frames (bob) and shifts its weight (dx) onto the
// planted leg — that side-to-side waddle is what makes a front-facing chibi
// read as actually walking rather than marching in place.
const WALK: Pose[] = [
  { bob: -1, dx: 1, legL: "lift", legR: "plant", armL: -1, armR: 1 },
  { bob: 0, dx: 0, legL: "plant", legR: "plant", armL: 0, armR: 0 },
  { bob: -1, dx: -1, legL: "plant", legR: "lift", armL: 1, armR: -1 },
  { bob: 0, dx: 0, legL: "plant", legR: "plant", armL: 0, armR: 0 },
];

export type ChibiState = "walk" | "idle" | "sit";

function Leg({ x, lifted, pants }: { x: number; lifted: boolean; pants: string }) {
  if (lifted) {
    return (
      <g>
        <rect x={x} y={22} width={3} height={2} fill={pants} />
        <rect x={x} y={24} width={3} height={1} fill={pants} />
        <rect x={x - 1} y={25} width={4} height={2} fill="#26221c" />
        <rect x={x - 1} y={25} width={4} height={1} fill="#3a342b" />
      </g>
    );
  }
  return (
    <g>
      <rect x={x} y={23} width={3} height={2} fill={pants} />
      <rect x={x} y={25} width={3} height={2} fill={pants} />
      <rect x={x - 1} y={27} width={4} height={2} fill="#26221c" />
      <rect x={x - 1} y={27} width={4} height={1} fill="#3a342b" />
    </g>
  );
}

// Held tools. Two rules keep them from 穿模 (clipping the body/face):
//  1. each prop is COMPACT and hugs the hand (drawn around x20–23,y19–23, the
//     forearm/hand box) so it never sticks far out to sweep across the torso;
//  2. the prop rides the armR group, and the gestures that swing the arm are
//     tuned (in globals.css) to moderate angles so hand+prop land naturally.
// PROP_FOR maps a gesture → which tool shows; object gestures add their own.
const PROP_FOR: Record<string, string> = {
  write: "pen",
  draw: "brush",
  coffee: "mug",
  phone: "phone",
  film: "clapper",
  magnify: "glass",
  read: "doc",
  point: "pen",
  eureka: "bulb",
  // office-object gestures
  browse: "book",
  snack: "snack",
  whiteboard: "marker",
  water: "wateringcan",
};
function HeldProp({ kind, def }: { kind: string; def: WorkerDef }) {
  switch (kind) {
    case "pen":
      return (
        <g>
          <rect x={20} y={20} width={1} height={3} fill="#3a6ea5" />
          <rect x={20} y={19} width={1} height={1} fill="#e3b23a" />
          <rect x={20} y={23} width={1} height={1} fill={INK} />
        </g>
      );
    case "brush":
      return (
        <g>
          <rect x={20} y={20} width={1} height={2} fill="#8a5a2b" />
          <rect x={20} y={22} width={1} height={1} fill={def.shirt} />
        </g>
      );
    case "mug":
      return (
        <g>
          <rect x={20} y={20} width={3} height={2} fill="#d97a4a" />
          <rect x={20} y={20} width={3} height={1} fill="#fff" opacity={0.5} />
          <rect x={23} y={20} width={1} height={1} fill="#b35a30" />
        </g>
      );
    case "phone":
      return (
        <g>
          <rect x={20} y={19} width={2} height={3} fill={INK} />
          <rect x={20.3} y={19.5} width={1.4} height={1.8} fill="#74c4e6" />
        </g>
      );
    case "clapper":
      return (
        <g>
          <rect x={20} y={20} width={3} height={2} fill={INK} />
          <rect x={20} y={19} width={3} height={1} fill="#fbfaf2" />
          <rect x={21} y={19} width={1} height={1} fill={INK} />
        </g>
      );
    case "glass":
      return (
        <g>
          <circle cx={21} cy={21} r={1.6} fill="#bfe3f2" opacity={0.5} stroke="#2a9d8f" strokeWidth={0.8} />
          <rect x={22} y={22} width={1.6} height={0.9} fill="#2a9d8f" transform="rotate(45 22 22)" />
        </g>
      );
    case "doc":
      return (
        <g>
          <rect x={20} y={19} width={3} height={4} fill="#fbfaf2" stroke={INK} strokeWidth={0.4} />
          <rect x={20.7} y={20.5} width={1.6} height={0.7} fill="#c9c3b5" />
          <rect x={20.7} y={22} width={1.6} height={0.7} fill="#c9c3b5" />
        </g>
      );
    case "bulb":
      return (
        <g>
          <rect x={20} y={18} width={2} height={2} fill="#ffe27a" />
          <rect x={20} y={20} width={2} height={1} fill="#b8860b" />
        </g>
      );
    case "book":
      return (
        <g>
          <rect x={19} y={20} width={4} height={3} fill="#b03d3d" />
          <rect x={21} y={20} width={1} height={3} fill="#fbfaf2" />
          <rect x={19} y={20} width={4} height={1} fill="#8a2e2e" />
        </g>
      );
    case "snack":
      return (
        <g>
          <rect x={20} y={20} width={2} height={3} fill="#e0a73a" />
          <rect x={20} y={20} width={2} height={1} fill="#f3cd7a" />
          <rect x={20} y={22} width={2} height={1} fill="#c98a1d" />
        </g>
      );
    case "marker":
      return (
        <g>
          <rect x={20} y={20} width={1} height={3} fill="#2a9d8f" />
          <rect x={20} y={23} width={1} height={1} fill={INK} />
        </g>
      );
    case "wateringcan":
      return (
        <g>
          <rect x={20} y={20} width={3} height={2} fill="#6aa0c4" />
          <rect x={23} y={19} width={1} height={1} fill="#6aa0c4" />
          <rect x={19} y={20} width={1} height={2} fill="#4f7d9c" />
        </g>
      );
    default:
      return null;
  }
}

function Arm({
  x,
  swing,
  def,
  skin,
  cls,
  prop,
}: {
  x: number;
  swing?: number;
  def: WorkerDef;
  skin: string;
  cls?: string;
  prop?: string;
}) {
  const dy = swing ?? 0;
  return (
    <g className={cls}>
      <rect x={x} y={16} width={3} height={3} fill={def.shirt} />
      <g transform={`translate(0,${dy})`}>
        <rect x={x} y={19} width={3} height={1} fill={def.shirtDark ?? def.shirt} />
        <rect x={x} y={20} width={3} height={2} fill={skin} />
        {prop && <HeldProp kind={prop} def={def} />}
      </g>
    </g>
  );
}

function Head({ def, skin, cls }: { def: WorkerDef; skin: string; cls?: string }) {
  const shade = "#16140f";
  return (
    <g className={cls}>
      <rect x={8} y={1} width={12} height={1} fill={INK} />
      <rect x={7} y={2} width={1} height={12} fill={INK} />
      <rect x={20} y={2} width={1} height={12} fill={INK} />
      <rect x={8} y={14} width={12} height={1} fill={INK} />
      <rect x={8} y={2} width={12} height={12} fill={skin} />
      <rect x={18} y={2} width={2} height={12} fill={shade} opacity={0.08} />
      {def.hairKind !== "none" && (
        <>
          <rect x={8} y={2} width={12} height={3} fill={def.hair} />
          <rect x={8} y={5} width={3} height={1} fill={def.hair} />
          <rect x={13} y={5} width={2} height={1} fill={def.hair} />
          <rect x={17} y={5} width={2} height={1} fill={def.hair} />
          <rect x={8} y={5} width={1} height={3} fill={def.hair} />
          <rect x={19} y={5} width={1} height={3} fill={def.hair} />
          <rect x={10} y={2} width={4} height={1} fill="#fff" opacity={0.18} />
        </>
      )}
      {def.hairKind === "long" && (
        <>
          <rect x={8} y={5} width={2} height={8} fill={def.hair} />
          <rect x={18} y={5} width={2} height={8} fill={def.hair} />
        </>
      )}
      <rect x={10} y={8} width={2} height={3} fill={INK} />
      <rect x={16} y={8} width={2} height={3} fill={INK} />
      <rect x={10} y={8} width={1} height={1} fill="#fff" />
      <rect x={16} y={8} width={1} height={1} fill="#fff" />
      <rect x={9} y={11} width={2} height={1} fill="#f2a3a3" opacity={0.5} />
      <rect x={17} y={11} width={2} height={1} fill="#f2a3a3" opacity={0.5} />
      <rect x={13} y={11} width={2} height={1} fill={shade} opacity={0.25} />
      <g dangerouslySetInnerHTML={{ __html: def.acc }} />
    </g>
  );
}

function Body({ def, skin }: { def: WorkerDef; skin: string }) {
  return (
    <g>
      <rect x={9} y={15} width={10} height={8} fill={INK} />
      <rect x={10} y={16} width={8} height={6} fill={def.shirt} />
      <rect x={16} y={16} width={2} height={6} fill={def.shirtDark ?? def.shirt} />
      <rect x={12} y={15} width={4} height={1} fill={skin} />
      <rect x={11} y={16} width={2} height={1} fill="#fff" opacity={0.7} />
      <rect x={15} y={16} width={2} height={1} fill="#fff" opacity={0.7} />
      {def.badge && <rect x={13} y={18} width={2} height={2} fill={def.badge} />}
    </g>
  );
}

// One walk-cycle cell (no gesture; locomotion only).
function WalkCell({ def, pose }: { def: WorkerDef; pose: Pose }) {
  const skin = def.skin ?? SKIN;
  const pants = def.pants ?? INK;
  return (
    <svg width={28} height={30} viewBox="0 0 28 30" shapeRendering="crispEdges">
      <g transform={`translate(${pose.dx},${pose.bob})`}>
        <Leg x={10} lifted={pose.legL === "lift"} pants={pants} />
        <Leg x={15} lifted={pose.legR === "lift"} pants={pants} />
        <Arm x={6} swing={pose.armL} def={def} skin={skin} />
        <Body def={def} skin={skin} />
        <Arm x={19} swing={pose.armR} def={def} skin={skin} />
        <Head def={def} skin={skin} />
      </g>
    </svg>
  );
}

// Standing / sitting base pose with classed groups for CSS secondary motion.
function PosedSvg({ def, sit, gesture }: { def: WorkerDef; sit?: boolean; gesture?: string }) {
  const skin = def.skin ?? SKIN;
  const pants = def.pants ?? INK;
  const gClass = gesture ? `chibi-g-${gesture}` : undefined;
  const prop = gesture ? PROP_FOR[gesture] : undefined;
  return (
    <svg
      width={28}
      height={30}
      viewBox="0 0 28 30"
      shapeRendering="crispEdges"
      className={clsx("chibi-pose", sit && "chibi-sit", gClass)}
    >
      {sit ? (
        // 坐姿:身体明显下沉坐到凳子上 + 小腿在凳前自然垂下。大头 Q 版没法画椅背
        // (头就贴在肩上),所以靠「整体压低 + 看得见的凳子 + 垂腿」来读出「坐着」。
        <g className="chibi-figure">
          {/* 凳子:座板 + 两条腿 */}
          <rect x={7} y={26} width={14} height={2} fill="#7a5630" />
          <rect x={7} y={26} width={14} height={1} fill="#8f6838" />
          <rect x={8} y={28} width={2} height={2} fill="#543a1c" />
          <rect x={18} y={28} width={2} height={2} fill="#543a1c" />
          {/* 垂在凳前的小腿 + 鞋 */}
          <rect x={10} y={27} width={3} height={2} fill={pants} />
          <rect x={15} y={27} width={3} height={2} fill={pants} />
          <rect x={9} y={29} width={4} height={1} fill="#26221c" />
          <rect x={15} y={29} width={4} height={1} fill="#26221c" />
          {/* 上半身整体下沉,坐到座板上 */}
          <g transform="translate(0,4)">
            <Arm x={6} def={def} skin={skin} cls="chibi-armL" />
            <Body def={def} skin={skin} />
            <Arm x={19} def={def} skin={skin} cls="chibi-armR" prop={prop} />
            <Head def={def} skin={skin} cls="chibi-head" />
          </g>
        </g>
      ) : (
        <g className="chibi-figure">
          <Leg x={10} lifted={false} pants={pants} />
          <Leg x={15} lifted={false} pants={pants} />
          <Arm x={6} def={def} skin={skin} cls="chibi-armL" />
          <Body def={def} skin={skin} />
          <Arm x={19} def={def} skin={skin} cls="chibi-armR" prop={prop} />
          <Head def={def} skin={skin} cls="chibi-head" />
        </g>
      )}
    </svg>
  );
}

export function ChibiWorker({
  def,
  facing,
  state,
  gesture,
  size = 54,
}: {
  def: WorkerDef;
  facing: 1 | -1;
  state: ChibiState;
  /** resolved act for this tick (talk/nod/point/type/think/wave/cheer…). */
  gesture?: string;
  size?: number;
}) {
  const scale = size / 28;
  const flip = facing < 0;
  return (
    <div className="chibi-view" style={{ width: size, height: (size * 30) / 28 }}>
      <div style={{ width: "100%", height: "100%", transform: flip ? "scaleX(-1)" : undefined }}>
        <div className="chibi-scaler" style={{ width: 28, height: 30, transform: `scale(${scale})` }}>
          {state === "walk" ? (
            <div
              className="chibi-strip chibi-anim"
              style={{ width: WALK.length * 28, animation: `chibi-cycle 0.6s steps(${WALK.length}) infinite` }}
            >
              {WALK.map((p, i) => (
                <div key={i} style={{ flex: "0 0 28px", width: 28, height: 30 }}>
                  <WalkCell def={def} pose={p} />
                </div>
              ))}
            </div>
          ) : (
            <PosedSvg def={def} sit={state === "sit"} gesture={gesture} />
          )}
        </div>
      </div>
    </div>
  );
}
