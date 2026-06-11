"use client";

import clsx from "clsx";

import { INK, SKIN, type WorkerDef } from "./workers";

// WorkerSprite v3 — the chibi rig. Big expressive head (rounded ink ring,
// two-tone hair with bangs + highlight, sparkle eyes, blush, mouth), small
// body with collar + shaded shirt, trousers and laced shoes. Parts stay the
// same independently-animatable groups (claw-head/armL/armR/body/legL/legR)
// so every gesture in globals.css keeps working. Position/walking is owned
// by the parent.
export function WorkerSprite({
  def,
  gesture,
  walking,
  grey,
  size = 46,
}: {
  def: WorkerDef;
  gesture?: string; // claw-act-<gesture>
  walking?: boolean;
  grey?: boolean;
  size?: number;
}) {
  const skin = def.skin ?? SKIN;
  const pants = def.pants ?? INK;
  const shade = "#16140f";

  return (
    <div
      className={clsx("claw-guy", walking && "claw-walking", !walking && gesture && `claw-act-${gesture}`)}
      style={{
        width: size,
        transition: "filter 0.3s",
        filter: grey ? "grayscale(0.7) opacity(0.55)" : "none",
      }}
    >
      <div className="claw-rig">
        <svg
          width={size}
          height={Math.round((size * 30) / 28)}
          viewBox="0 0 28 30"
          className="claw-sprite"
          shapeRendering="crispEdges"
        >
          <g className="claw-legL">
            <rect x="10" y="23" width="3" height="4" fill={pants} />
            <rect x="9" y="27" width="4" height="2" fill="#26221c" />
            <rect x="9" y="27" width="4" height="1" fill="#3a342b" />
          </g>
          <g className="claw-legR">
            <rect x="15" y="23" width="3" height="4" fill={pants} />
            <rect x="15" y="27" width="4" height="2" fill="#26221c" />
            <rect x="15" y="27" width="4" height="1" fill="#3a342b" />
          </g>
          <g className="claw-armL">
            <rect x="6" y="16" width="3" height="3" fill={def.shirt} />
            <rect x="6" y="19" width="3" height="1" fill={def.shirtDark ?? def.shirt} />
            <rect x="6" y="20" width="3" height="2" fill={skin} />
          </g>
          <g className="claw-body">
            <rect x="9" y="15" width="10" height="8" fill={INK} />
            <rect x="10" y="16" width="8" height="6" fill={def.shirt} />
            <rect x="16" y="16" width="2" height="6" fill={def.shirtDark ?? def.shirt} />
            <rect x="12" y="15" width="4" height="1" fill={skin} />
            <rect x="11" y="16" width="2" height="1" fill="#fff" opacity="0.7" />
            <rect x="15" y="16" width="2" height="1" fill="#fff" opacity="0.7" />
            {def.badge && <rect x="13" y="18" width="2" height="2" fill={def.badge} />}
          </g>
          <g className="claw-armR">
            <rect x="19" y="16" width="3" height="3" fill={def.shirt} />
            <rect x="19" y="19" width="3" height="1" fill={def.shirtDark ?? def.shirt} />
            <rect x="19" y="20" width="3" height="2" fill={skin} />
          </g>
          <g className="claw-head">
            {/* rounded ink ring */}
            <rect x="8" y="1" width="12" height="1" fill={INK} />
            <rect x="7" y="2" width="1" height="12" fill={INK} />
            <rect x="20" y="2" width="1" height="12" fill={INK} />
            <rect x="8" y="14" width="12" height="1" fill={INK} />
            {/* face */}
            <rect x="8" y="2" width="12" height="12" fill={skin} />
            <rect x="18" y="2" width="2" height="12" fill={shade} opacity="0.08" />
            {/* hair */}
            {def.hairKind !== "none" && (
              <>
                <rect x="8" y="2" width="12" height="3" fill={def.hair} />
                <rect x="8" y="5" width="3" height="1" fill={def.hair} />
                <rect x="13" y="5" width="2" height="1" fill={def.hair} />
                <rect x="17" y="5" width="2" height="1" fill={def.hair} />
                <rect x="8" y="5" width="1" height="3" fill={def.hair} />
                <rect x="19" y="5" width="1" height="3" fill={def.hair} />
                <rect x="10" y="2" width="4" height="1" fill="#fff" opacity="0.18" />
              </>
            )}
            {def.hairKind === "long" && (
              <>
                <rect x="8" y="5" width="2" height="8" fill={def.hair} />
                <rect x="18" y="5" width="2" height="8" fill={def.hair} />
              </>
            )}
            {/* eyes with sparkle */}
            <rect x="10" y="8" width="2" height="3" fill={INK} />
            <rect x="16" y="8" width="2" height="3" fill={INK} />
            <rect x="10" y="8" width="1" height="1" fill="#fff" />
            <rect x="16" y="8" width="1" height="1" fill="#fff" />
            {/* blush + mouth */}
            <rect x="9" y="11" width="2" height="1" fill="#f2a3a3" opacity="0.5" />
            <rect x="17" y="11" width="2" height="1" fill="#f2a3a3" opacity="0.5" />
            <rect x="13" y="11" width="2" height="1" fill={shade} opacity="0.25" />
            <g dangerouslySetInnerHTML={{ __html: def.acc }} />
          </g>
        </svg>
      </div>
    </div>
  );
}
