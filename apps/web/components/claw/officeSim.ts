"use client";

import { useEffect, useRef, useState } from "react";

import { WORKERS } from "./workers";
import { STATIONS_V12, MEETING_V12, LOUNGE_V12, WAYPOINTS_V12, LANES_V12 } from "./officeArt";
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
  rowYs: [40, 64] as const, // (legacy — v12 layout drives stations directly)
  lanes: LANES_V12 as readonly number[], // walk corridors (v12: functional-pod lanes)
  speedX: 0.85, // % per tick
  speedY: 0.6,
  tickMs: 90,
  pauseChance: 0.006, // mid-walk "stop and look around"
  pauseTicks: 7,
  meetingMs: 15000, // walk-in (~5s from the far lounge) + a real discussion
  stroll: {
    idleMin: 16000,
    idleVar: 18000,
    workingMin: 26000,
    workingVar: 22000,
    doneMin: 18000,
    doneVar: 16000,
    lingerMin: 3400,
    lingerVar: 2600,
    workingLinger: 2300,
    visitChance: 0.25,
    maxAway: 2, // at most this many workers out strolling/visiting at once
  },
  storage: {
    chat: "claw-chat-open",
    dock: "claw-dock-anchor",
    wardrobe: "claw-wardrobe-v1",
  },
};

type P = { x: number; y: number };

// ── Static geometry — v12 去 AI 味 layout (functional pods, see officeArt) ──
export const STATIONS: Record<string, P> = STATIONS_V12;

export const MEETING_TABLE = { x: MEETING_V12.tableX, y: MEETING_V12.tableY, w: 30, h: 8 };
const MEET_SLOTS: P[] = MEETING_V12.seats;

const LOUNGE_SLOTS: P[] = LOUNGE_V12;

const WP_GESTURE: Record<string, string> = { coffee: "coffee", plant: "think", whiteboard: "look" };
export const WAYPOINTS: { x: number; y: number; g: string; slots: number }[] = WAYPOINTS_V12.map((w) => ({
  x: w.x,
  y: w.y,
  g: WP_GESTURE[w.kind] ?? "look",
  slots: 2,
}));

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
  /** who sits at the head slot + leads the talk; defaults to the coordinator. */
  meetingChair?: string;
  /** short UI-driven gesture overrides (交接击掌 etc.) — read each tick. */
  pulses?: { current: Record<string, { g: string; until: number }> };
  /** forced visits (锤人 etc.): worker → walk to host's desk for the window. */
  visits?: { current: Record<string, { host: string; until: number }> };
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
    const candidatesFor = (key: string, site: string, chair: string): { id: string; p: P }[] => {
      if (site === "desk") return [{ id: `desk:${key}`, p: STATIONS[key] }];
      if (site === "meet") {
        const ids =
          key === chair
            ? [0]
            : MEET_SLOTS.map((_, i) => i).filter((i) => i !== 0);
        return ids.map((i) => ({ id: `meet:${i}`, p: MEET_SLOTS[i] }));
      }
      if (site === "lounge") {
        // each worker prefers a FIXED lounge seat (their index, rotated) so the
        // off-duty arrangement is tidy and nobody re-shuffles spots mid-party
        const wi = Math.max(0, WORKERS.findIndex((w) => w.key === key));
        return LOUNGE_SLOTS.map((_, i) => {
          const j = (wi + i) % LOUNGE_SLOTS.length;
          return { id: `lounge:${j}`, p: LOUNGE_SLOTS[j] };
        });
      }
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

    // 完工庆祝 — when a worker flips working→done, desk neighbours turn and
    // cheer for a moment. Tracked in the tick closure.
    const lastStatus: Record<string, WorkerStatus> = {};
    const cheers: Record<string, { until: number; tx: number }> = {};

    const timer = setInterval(() => {
      const now = Date.now();
      const { views, offDuty, meetingUntil } = inputRef.current;
      const meeting = meetingUntil > now;
      const chair = inputRef.current.meetingChair || WORKERS[0].key;
      const out: Record<string, SimView> = {};
      const c = OFFICE_CONFIG;

      for (const w of WORKERS) {
        const st: WorkerStatus = views[w.key]?.status ?? "idle";
        if (lastStatus[w.key] === "working" && st === "done") {
          const fs = STATIONS[w.key];
          for (const n of WORKERS) {
            if (n.key === w.key) continue;
            const ns = STATIONS[n.key];
            if (ns.y === fs.y && Math.abs(ns.x - fs.x) < 26) cheers[n.key] = { until: now + 2400, tx: fs.x };
          }
        }
        lastStatus[w.key] = st;
      }

      // who is hosting a guest this tick (visitor arrived at their visit slot)
      const guests: Record<string, string> = {};
      // how many workers are currently out on a trip (calm-office cap)
      let away = 0;
      for (const w of WORKERS) {
        const a = agents[w.key];
        if (a.site.startsWith("visit:") && a.path.length === 0) guests[a.site.split(":")[1]] = w.key;
        if (a.site.startsWith("way:") || a.site.startsWith("visit:")) away += 1;
      }

      WORKERS.forEach((w, wi) => {
        const a = agents[w.key];
        const status: WorkerStatus = views[w.key]?.status ?? "idle";

        // 1) desired site
        let want: string;
        const fv = inputRef.current.visits?.current?.[w.key];
        if (meeting) want = "meet";
        else if (fv && fv.until > now && fv.host !== w.key)
          want = `visit:${fv.host}`; // forced visit (锤人 etc.) — works even off-duty
        else if (offDuty) want = "lounge";
        else {
          // calm office: everyone's home is their OWN DESK (idle workers sit
          // there with lounge-y gestures); the lounge is for off-duty only.
          const home = "desk";
          const onTrip = a.site.startsWith("way:") || a.site.startsWith("visit:");
          if (onTrip && now < a.lingerUntil) want = a.site; // keep lingering
          else if (!onTrip && now > a.nextStrollAt && a.path.length === 0 && away < c.stroll.maxAway) {
            want = pickStroll(w.key, status);
            away += 1;
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
          const got = book(w.key, candidatesFor(w.key, want, chair));
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
            gesture = w.key === chair ? beat(MEET_LEAD, 1700) : beat(MEET_ATTEND);
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
            const ch = cheers[w.key];
            if (ch && ch.until > now) {
              // 完工庆祝: turn toward the finisher and cheer
              a.facing = ch.tx >= a.x ? 1 : -1;
              gesture = "cheer";
            } else {
              a.facing = 1; // desks face right (prop sits to the right)
              const v = views[w.key];
              gesture =
                status === "working" ? v?.gesture || beat(w.actions, 1700) : status === "done" ? beat(["nod", "look", "stretch", "write"], 3600) : beat(LOUNGE, 4100);
            }
          }
        }

        // 结果反应 — a live outcome reaction (facepalm / eureka) overrides the
        // cycling gesture for its short window, wherever the worker is standing.
        if (!walking) {
          const v = views[w.key];
          if (v?.reactionUntil && v.reactionUntil > now && v.reactionGesture) gesture = v.reactionGesture;
          // UI pulse (交接击掌 etc.) wins over everything for its moment
          const p = inputRef.current.pulses?.current?.[w.key];
          if (p && p.until > now) gesture = p.g;
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
