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
          {/* legs — two-bone (thigh → shin+foot at the knee) for a real stride */}
          <g className="claw-legL">
            <rect x="10" y="23" width="3" height="2" fill={pants} />
            <g className="claw-shinL">
              <rect x="10" y="25" width="3" height="2" fill={pants} />
              <rect x="9" y="27" width="4" height="2" fill="#26221c" />
              <rect x="9" y="27" width="4" height="1" fill="#3a342b" />
            </g>
          </g>
          <g className="claw-legR">
            <rect x="15" y="23" width="3" height="2" fill={pants} />
            <g className="claw-shinR">
              <rect x="15" y="25" width="3" height="2" fill={pants} />
              <rect x="15" y="27" width="4" height="2" fill="#26221c" />
              <rect x="15" y="27" width="4" height="1" fill="#3a342b" />
            </g>
          </g>
          {/* arms — two-bone (upper arm → forearm+hand at the elbow) */}
          <g className="claw-armL">
            <rect x="6" y="16" width="3" height="3" fill={def.shirt} />
            <g className="claw-forearmL">
              <rect x="6" y="19" width="3" height="1" fill={def.shirtDark ?? def.shirt} />
              <rect x="6" y="20" width="3" height="2" fill={skin} />
            </g>
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
            <g className="claw-forearmR">
            <rect x="19" y="19" width="3" height="1" fill={def.shirtDark ?? def.shirt} />
            <rect x="19" y="20" width="3" height="2" fill={skin} />
            {/* held tools — hidden by default, revealed per working gesture by
                CSS (.claw-act-<g> .claw-prop-<x>); held in the right hand so
                they move with the forearm and sell "真正在干活". */}
            <g className="claw-prop claw-prop-pen">
              <rect x="21" y="22" width="1" height="5" fill="#3a6ea5" />
              <rect x="21" y="21" width="1" height="1" fill="#e3b23a" />
              <rect x="21" y="27" width="1" height="1" fill="#16140f" />
            </g>
            <g className="claw-prop claw-prop-brush">
              <rect x="21" y="21" width="1" height="4" fill="#8a5a2b" />
              <rect x="20" y="25" width="2" height="2" fill={def.shirt} />
              <rect x="20" y="27" width="2" height="1" fill={def.shirtDark ?? def.shirt} />
            </g>
            <g className="claw-prop claw-prop-glass">
              <rect x="20" y="24" width="2" height="1" fill="#6a4a23" />
              <rect x="21" y="20" width="3" height="1" fill="#4a3417" />
              <rect x="21" y="23" width="3" height="1" fill="#4a3417" />
              <rect x="21" y="20" width="1" height="3" fill="#4a3417" />
              <rect x="23" y="20" width="1" height="3" fill="#4a3417" />
              <rect x="22" y="21" width="1" height="2" fill="#bfe3f2" opacity="0.8" />
            </g>
            <g className="claw-prop claw-prop-clap">
              <rect x="20" y="22" width="5" height="3" fill="#16140f" />
              <rect x="20" y="22" width="5" height="1" fill="#fbfaf2" />
              <rect x="20" y="20" width="5" height="1" fill="#16140f" />
              <rect x="21" y="20" width="1" height="1" fill="#fbfaf2" />
              <rect x="23" y="20" width="1" height="1" fill="#fbfaf2" />
            </g>
            </g>
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
