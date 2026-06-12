"use client";

import { useEffect, useRef, useState } from "react";

import { WORKERS } from "./workers";
import type { WorkerStatus, WorkerView } from "./useWorkerStates";

// officeSim is the central "Sims" engine: ONE tick loop moves every worker
// along corridor (Manhattan) paths at constant speed, books destination
// SLOTS so two characters never stand on the same spot, flips facing from
// the walk direction, pairs up face-to-face chats when someone visits a
// colleague, and orchestrates the special modes (kickoff MEETING around the
// conference table, off-duty lounge PARTY). Components render what the sim
// says — no per-worker movement timers anywhere else.

// ── Tunables — the office is configured here, not hard-coded in JSX ──────
export const OFFICE_CONFIG = {
  spriteSize: 58,
  deskSize: 66,
  rowYs: [40, 64] as const, // desk rows (feet line, % of scene)
  lanes: [52, 76] as const, // walk corridors between/below the rows
  speedX: 0.85, // % per tick
  speedY: 0.6,
  tickMs: 90,
  pauseChance: 0.006, // mid-walk "stop and look around"
  pauseTicks: 7,
  meetingMs: 15000, // walk-in (~5s from the far lounge) + a real discussion
  stroll: {
    idleMin: 7000,
    idleVar: 8000,
    workingMin: 26000,
    workingVar: 22000,
    doneMin: 13000,
    doneVar: 13000,
    lingerMin: 3400,
    lingerVar: 2600,
    workingLinger: 2300,
    visitChance: 0.45,
  },
  storage: {
    chat: "claw-chat-open",
    dock: "claw-dock-anchor",
    wardrobe: "claw-wardrobe-v1",
  },
};

type P = { x: number; y: number };

// ── Static geometry, derived from the registry ──────────────────────────
export const STATIONS: Record<string, P> = (() => {
  const out: Record<string, P> = {};
  const backCount = Math.ceil(WORKERS.length / 2);
  const rows = [WORKERS.slice(0, backCount), WORKERS.slice(backCount)];
  rows.forEach((row, r) => {
    row.forEach((w, i) => {
      const x = row.length === 1 ? 42 : 5 + (i * 70) / (row.length - 1);
      out[w.key] = { x: Math.round(x * 10) / 10, y: OFFICE_CONFIG.rowYs[r] };
    });
  });
  return out;
})();

export const MEETING_TABLE = { x: 60, y: 88, w: 24, h: 7 }; // front-right floor
const MEET_SLOTS: P[] = [
  { x: 54, y: 88 }, // head — the coordinator chairs the meeting
  { x: 61, y: 82 },
  { x: 67, y: 82 },
  { x: 73, y: 82 },
  { x: 61, y: 94 },
  { x: 67, y: 94 },
  { x: 73, y: 94 },
];

const LOUNGE_SLOTS: P[] = [
  { x: 4, y: 90 },
  { x: 9, y: 87 },
  { x: 14, y: 90 },
  { x: 19, y: 87 },
  { x: 6, y: 95 },
  { x: 11, y: 94 },
  { x: 16, y: 95 },
  { x: 21, y: 93 },
];

export const WAYPOINTS: { x: number; y: number; g: string; slots: number }[] = [
  { x: 3, y: 88, g: "coffee", slots: 2 }, // coffee machine
  { x: 92, y: 70, g: "think", slots: 2 }, // plant corner
  { x: 47, y: 38, g: "look", slots: 2 }, // whiteboard
];

// ── Sim state ────────────────────────────────────────────────────────────
type Agent = {
  x: number;
  y: number;
  facing: 1 | -1;
  path: P[];
  paused: number;
  slotId: string | null;
  site: string; // "desk" | "lounge" | "meet" | "way:<i>" | "visit:<key>"
  lingerUntil: number;
  nextStrollAt: number;
};

export type SimView = {
  x: number;
  y: number;
  facing: 1 | -1;
  walking: boolean;
  gesture: string; // resolved act for this tick ("" = none)
  site: string;
};

const dist = (a: P, b: P) => Math.abs(a.x - b.x) + Math.abs(a.y - b.y);

function manhattanPath(from: P, to: P): P[] {
  if (Math.abs(from.y - to.y) < 3) return [to]; // same band → straight line
  const lane = OFFICE_CONFIG.lanes.reduce((best, l) =>
    Math.abs(l - from.y) + Math.abs(l - to.y) < Math.abs(best - from.y) + Math.abs(best - to.y) ? l : best,
  );
  const path: P[] = [];
  if (Math.abs(from.y - lane) > 1) path.push({ x: from.x, y: lane });
  if (Math.abs(from.x - to.x) > 1) path.push({ x: to.x, y: lane });
  path.push(to);
  return path;
}

// gesture pools per situation (the sim owns ALL timing so crowds stagger)
const MEET_LEAD = ["talk", "point", "write", "talk"];
const MEET_ATTEND = ["nod", "think", "write", "talk", "look"];
const PARTY = ["cheer", "dance", "coffee", "talk", "dance", "doze"];
const LOUNGE = ["coffee", "talk", "doze", "look", "stretch", "think"];

export function useOfficeSim(inputs: {
  views: Record<string, WorkerView>;
  offDuty: boolean;
  meetingUntil: number;
}): Record<string, SimView> {
  const inputRef = useRef(inputs);
  inputRef.current = inputs;

  const agentsRef = useRef<Record<string, Agent> | null>(null);
  const slotsRef = useRef<Map<string, string>>(new Map());
  const [viewsOut, setViewsOut] = useState<Record<string, SimView>>({});

  useEffect(() => {
    // init agents at the lounge
    if (!agentsRef.current) {
      const init: Record<string, Agent> = {};
      WORKERS.forEach((w, i) => {
        const p = LOUNGE_SLOTS[i % LOUNGE_SLOTS.length];
        init[w.key] = {
          x: p.x,
          y: p.y,
          facing: 1,
          path: [],
          paused: 0,
          slotId: null,
          site: "lounge",
          lingerUntil: 0,
          nextStrollAt: Date.now() + 4000 + i * 1500,
        };
      });
      agentsRef.current = init;
    }
    const agents = agentsRef.current;
    const slots = slotsRef.current;

    const book = (key: string, candidates: { id: string; p: P }[]): { id: string; p: P } | null => {
      for (const c of candidates) {
        const owner = slots.get(c.id);
        if (!owner || owner === key) {
          slots.set(c.id, key);
          return c;
        }
      }
      return null;
    };
    const release = (key: string) => {
      const a = agents[key];
      if (a.slotId && slots.get(a.slotId) === key) slots.delete(a.slotId);
      a.slotId = null;
    };
    const slotPos = (id: string): P => {
      const [kind, arg, arg2] = id.split(":");
      if (kind === "desk") return STATIONS[arg];
      if (kind === "visit") return { x: Math.max(1, STATIONS[arg].x - 6), y: STATIONS[arg].y };
      if (kind === "meet") return MEET_SLOTS[Number(arg)];
      if (kind === "lounge") return LOUNGE_SLOTS[Number(arg)];
      if (kind === "way") {
        const w = WAYPOINTS[Number(arg)];
        return { x: w.x + Number(arg2) * 4.5, y: w.y };
      }
      return STATIONS[arg] ?? LOUNGE_SLOTS[0];
    };
    const candidatesFor = (key: string, site: string): { id: string; p: P }[] => {
      if (site === "desk") return [{ id: `desk:${key}`, p: STATIONS[key] }];
      if (site === "meet") {
        const ids =
          key === WORKERS[0].key
            ? [0]
            : MEET_SLOTS.map((_, i) => i).filter((i) => i !== 0);
        return ids.map((i) => ({ id: `meet:${i}`, p: MEET_SLOTS[i] }));
      }
      if (site === "lounge")
        return LOUNGE_SLOTS.map((p, i) => ({ id: `lounge:${i}`, p })).sort(() => Math.random() - 0.5);
      if (site.startsWith("way:")) {
        const wi = Number(site.split(":")[1]);
        const w = WAYPOINTS[wi];
        return Array.from({ length: w.slots }, (_, s) => ({ id: `way:${wi}:${s}`, p: slotPos(`way:${wi}:${s}`) }));
      }
      if (site.startsWith("visit:")) {
        const host = site.split(":")[1];
        return [{ id: `visit:${host}`, p: slotPos(`visit:${host}`) }];
      }
      return [];
    };

    const pickStroll = (key: string, status: WorkerStatus): string => {
      if (status === "working") return "way:0"; // quick coffee run only
      const c = OFFICE_CONFIG.stroll;
      if (Math.random() < c.visitChance) {
        const hosts = WORKERS.filter(
          (w) => w.key !== key && (inputRef.current.views[w.key]?.status ?? "idle") !== "idle" && !slots.has(`visit:${w.key}`),
        );
        if (hosts.length > 0) return `visit:${hosts[Math.floor(Math.random() * hosts.length)].key}`;
      }
      return `way:${Math.floor(Math.random() * WAYPOINTS.length)}`;
    };

    const timer = setInterval(() => {
      const now = Date.now();
      const { views, offDuty, meetingUntil } = inputRef.current;
      const meeting = meetingUntil > now;
      const out: Record<string, SimView> = {};
      const c = OFFICE_CONFIG;

      // who is hosting a guest this tick (visitor arrived at their visit slot)
      const guests: Record<string, string> = {};
      for (const w of WORKERS) {
        const a = agents[w.key];
        if (a.site.startsWith("visit:") && a.path.length === 0) guests[a.site.split(":")[1]] = w.key;
      }

      WORKERS.forEach((w, wi) => {
        const a = agents[w.key];
        const status: WorkerStatus = views[w.key]?.status ?? "idle";

        // 1) desired site
        let want: string;
        if (meeting) want = "meet";
        else if (offDuty) want = "lounge";
        else {
          const home = status === "idle" ? "lounge" : "desk";
          const onTrip = a.site.startsWith("way:") || a.site.startsWith("visit:");
          if (onTrip && now < a.lingerUntil) want = a.site; // keep lingering
          else if (!onTrip && now > a.nextStrollAt && a.path.length === 0) {
            want = pickStroll(w.key, status);
            const s = c.stroll;
            a.lingerUntil =
              now + 1200 + (status === "working" ? s.workingLinger : s.lingerMin + Math.random() * s.lingerVar);
          } else want = home;
          if (onTrip && want === home) {
            const s = c.stroll;
            const base = status === "working" ? s.workingMin : status === "idle" ? s.idleMin : s.doneMin;
            const vari = status === "working" ? s.workingVar : status === "idle" ? s.idleVar : s.doneVar;
            a.nextStrollAt = now + base + Math.random() * vari;
          }
        }

        // 2) (re)book slot when the site changes
        if (a.site !== want || !a.slotId) {
          release(w.key);
          const got = book(w.key, candidatesFor(w.key, want));
          if (got) {
            a.slotId = got.id;
            a.site = want;
            a.path = manhattanPath({ x: a.x, y: a.y }, got.p);
          }
          // no free slot → stay put, retry next tick
        }

        // 3) advance along path
        let walking = false;
        if (a.paused > 0) {
          a.paused -= 1;
          walking = false;
        } else if (a.path.length > 0) {
          walking = true;
          if (Math.random() < c.pauseChance) a.paused = c.pauseTicks;
          const t = a.path[0];
          const dx = t.x - a.x;
          const dy = t.y - a.y;
          if (Math.abs(dx) > 0.3) a.facing = dx > 0 ? 1 : -1;
          a.x += Math.abs(dx) <= c.speedX ? dx : Math.sign(dx) * c.speedX;
          a.y += Math.abs(dy) <= c.speedY ? dy : Math.sign(dy) * c.speedY;
          if (Math.abs(t.x - a.x) < 0.2 && Math.abs(t.y - a.y) < 0.2) a.path.shift();
        }

        // 4) facing + gesture at destination
        const arrived = a.path.length === 0 && a.paused === 0;
        let gesture = "";
        if (a.paused > 0) gesture = "look";
        else if (!walking && arrived) {
          const beat = (pool: string[], ms = 1900) => pool[(Math.floor(now / ms) + wi) % pool.length];
          if (meeting && a.site === "meet") {
            // everyone faces the table centre
            a.facing = MEETING_TABLE.x + MEETING_TABLE.w / 2 >= a.x ? 1 : -1;
            gesture = w.key === WORKERS[0].key ? beat(MEET_LEAD, 1700) : beat(MEET_ATTEND);
          } else if (a.site.startsWith("visit:")) {
            const host = agents[a.site.split(":")[1]];
            if (host) a.facing = host.x >= a.x ? 1 : -1;
            gesture = "talk";
          } else if (guests[w.key]) {
            const guest = agents[guests[w.key]];
            if (guest) a.facing = guest.x >= a.x ? 1 : -1;
            gesture = "talk";
          } else if (a.site.startsWith("way:")) {
            gesture = WAYPOINTS[Number(a.site.split(":")[1])]?.g ?? "look";
          } else if (offDuty) {
            gesture = beat(PARTY, 2300);
          } else if (a.site === "lounge") {
            gesture = beat(LOUNGE, 4100);
          } else if (a.site === "desk") {
            a.facing = 1; // desks face right (prop sits to the right)
            const v = views[w.key];
            gesture =
              status === "working" ? v?.gesture || beat(w.actions, 1700) : status === "done" ? beat(["nod", "look", "stretch", "write"], 3600) : beat(LOUNGE, 4100);
          }
        }

        // 结果反应 — a live outcome reaction (facepalm / eureka) overrides the
        // cycling gesture for its short window, wherever the worker is standing.
        if (!walking) {
          const v = views[w.key];
          if (v?.reactionUntil && v.reactionUntil > now && v.reactionGesture) gesture = v.reactionGesture;
        }

        out[w.key] = { x: a.x, y: a.y, facing: a.facing, walking, gesture, site: a.site };
      });

      setViewsOut(out);
    }, c0());

    function c0() {
      return OFFICE_CONFIG.tickMs;
    }

    return () => {
      clearInterval(timer);
      slotsRef.current.clear();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return viewsOut;
}
