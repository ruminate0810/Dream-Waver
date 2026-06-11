"use client";

import { useEffect, useRef, useState } from "react";
import { FileText, Image as ImageIcon, Layers, X } from "lucide-react";
import clsx from "clsx";

import type { ClawRun } from "@/lib/api";
import { fmtDuration } from "@/components/chat/ToolProgress";
import { ACT, EMO, WORKERS, type WorkerDef, type WorkerPhase } from "./workers";
import { WorkerSprite } from "./WorkerSprite";
import { PixelWindow } from "./PixelWindow";
import { ArtifactBody, type WorkTab } from "./ArtifactPanel";
import { useWorkerStates, type WorkerView } from "./useWorkerStates";

// ClawOffice is the Marvis-style shared office: ONE big pixel room filling
// the right column, where all seven workers live. Idle workers hang out at
// the coffee lounge; when their agent starts working they WALK to their desk
// and loop the gesture of the real tool they're running; done workers stay
// at their desk with a ✓. The work package (report / figures / deck) opens
// as a draggable OS-style window OVER the office from the dock — so the
// report finally gets full reading room. Clicking a worker shows their live
// stats.

// Station coordinates are DERIVED from the WORKERS registry (two desk rows,
// evenly spaced) so adding an agent = adding a workers.ts entry, zero layout
// edits. (x, y) is the FEET line the character + desk share.
const STATIONS: Record<string, { x: number; y: number }> = (() => {
  const out: Record<string, { x: number; y: number }> = {};
  const backCount = Math.ceil(WORKERS.length / 2);
  const rows = [WORKERS.slice(0, backCount), WORKERS.slice(backCount)];
  const rowY = [42, 68];
  rows.forEach((row, r) => {
    row.forEach((w, i) => {
      const x = row.length === 1 ? 42 : 6 + (i * 72) / (row.length - 1);
      out[w.key] = { x: Math.round(x * 10) / 10, y: rowY[r] };
    });
  });
  return out;
})();
const LOUNGE = { x: 5, y: 94 };

// Wander spots — places a worker may stroll to between (or during) jobs.
const WAYPOINTS: { x: number; y: number; g: string }[] = [
  { x: 3, y: 88, g: "coffee" }, // coffee machine
  { x: 88, y: 90, g: "think" }, // plant corner
  { x: 47, y: 40, g: "look" }, // whiteboard
];

// The pipeline stages, grouped from each worker's declared phase.
const PHASE_ORDER: WorkerPhase[] = ["plan", "exec", "write", "review", "produce"];
const PHASE_ZH: Record<WorkerPhase, string> = {
  plan: "规划",
  exec: "执行",
  write: "撰写",
  review: "评审",
  produce: "制片",
};
const PHASES = PHASE_ORDER.map((p) => ({
  zh: PHASE_ZH[p],
  workers: WORKERS.filter((w) => w.phase === p).map((w) => w.key),
})).filter((p) => p.workers.length > 0);

// A handoff flight: a pixel document flying desk → desk when one agent's
// output feeds the next (assignment 📋 from the coordinator, drafts to the
// writer, the draft to the critic, the report to the producer). Driven by
// REAL status transitions, not animation loops.
type Flight = { id: number; from: { x: number; y: number }; to: { x: number; y: number }; delay: number };

let flightSeq = 1;

// Sources are derived from the phase ordering: when a worker starts, the
// document flies from the nearest EARLIER phase's workers (preferring ones
// that are done). Works for any registry shape — no role names hard-coded.
function flightSources(key: string, status: (k: string) => string): string[] {
  const me = WORKERS.find((w) => w.key === key);
  if (!me) return [];
  for (let i = PHASE_ORDER.indexOf(me.phase) - 1; i >= 0; i--) {
    const prev = WORKERS.filter((w) => w.phase === PHASE_ORDER[i]).map((w) => w.key);
    if (prev.length === 0) continue;
    const done = prev.filter((k) => status(k) === "done");
    return done.length > 0 ? done : prev.slice(0, 1);
  }
  return [];
}

export function ClawOffice({ run }: { run: ClawRun }) {
  const { views, lastActivity } = useWorkerStates(run.plan ?? []);
  const [focus, setFocus] = useState<string | null>(null);
  const [winOpen, setWinOpen] = useState(false);
  const [tab, setTab] = useState<WorkTab>("report");
  const autoOpened = useRef(false);

  // Agent↔agent handoffs: when a worker STARTS working, fly a document from
  // its upstream agent's desk to its own. Skipped on the very first sample
  // (cold load of an in-flight run shouldn't replay a burst of flights).
  const [flights, setFlights] = useState<Flight[]>([]);
  const prevStatus = useRef<Record<string, string> | null>(null);

  // 下班模式: a few seconds after the run finishes, everyone strolls to the
  // coffee lounge for a little party (dance/cheer/coffee/doze) — the office
  // celebrates the delivered work package.
  const [offDuty, setOffDuty] = useState(false);
  useEffect(() => {
    if (run.status === "finished") {
      const t = setTimeout(() => setOffDuty(true), 5200);
      return () => clearTimeout(t);
    }
    setOffDuty(false);
  }, [run.status]);
  useEffect(() => {
    const cur: Record<string, string> = {};
    for (const w of WORKERS) cur[w.key] = views[w.key]?.status ?? "idle";
    const prev = prevStatus.current;
    prevStatus.current = cur;
    if (!prev) return;
    const started = WORKERS.filter((w) => prev[w.key] !== "working" && cur[w.key] === "working");
    if (started.length === 0) return;
    const add: Flight[] = [];
    for (const w of started) {
      const to = STATIONS[w.key];
      if (!to) continue;
      flightSources(w.key, (k) => prev[k] ?? "idle").forEach((src, i) => {
        const from = STATIONS[src];
        if (from) add.push({ id: flightSeq++, from, to, delay: i * 220 });
      });
    }
    if (add.length > 0) setFlights((f) => [...f, ...add]);
  }, [views]);

  // Auto-open the report window the first time a report lands.
  useEffect(() => {
    if (!autoOpened.current && (run.artifact_version ?? 0) > 0) {
      autoOpened.current = true;
      setTab("report");
      setWinOpen(true);
    }
  }, [run.artifact_version]);

  const openTab = (t: WorkTab) => {
    setTab(t);
    setWinOpen(true);
  };

  // Per-agent todo lists, straight from the role-tagged plan. Drives the
  // nameplate badge (done/total) and the popover checklist.
  const todosByRole = (() => {
    const m: Record<string, { title: string; status: string }[]> = {};
    for (const t of run.plan ?? []) {
      if (t.role) (m[t.role] ??= []).push({ title: t.title, status: t.status });
    }
    return m;
  })();

  const focusedDef = focus ? WORKERS.find((w) => w.key === focus) : undefined;
  const focusedView = focus ? views[focus] : undefined;
  const focusedTodos = focus ? (todosByRole[focus] ?? []) : [];

  return (
    <div className="relative h-full min-h-[420px] overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel">
      {/* ── the room ───────────────────────────────────────────────── */}
      <div className="absolute inset-0">
        {/* wall + floor */}
        <div className="absolute inset-x-0 top-0 h-[34%] bg-surface-2" />
        <div className="absolute inset-x-0 top-[34%] h-[3px] bg-ink/15" />
        <div
          className="absolute inset-x-0 bottom-0 top-[34%] bg-[#e7dfcd]"
          style={{
            backgroundImage:
              "repeating-linear-gradient(0deg, rgba(22,20,15,0.05) 0 1px, transparent 1px 26px)",
          }}
        />
        <Decor />

        {/* desks + nameplates */}
        {WORKERS.map((def) => {
          const st = STATIONS[def.key];
          if (!st) return null;
          const v = views[def.key];
          return (
            <div
              key={def.key}
              className="absolute"
              style={{ left: `${st.x}%`, top: `${st.y}%`, zIndex: Math.round(st.y) }}
            >
              {v?.status === "working" && (
                <div className="absolute -left-[14px] top-[-8px] h-[10px] w-[124px] rounded-[4px] bg-accent/20" />
              )}
              <div
                className={clsx("absolute bottom-0 left-[52px]", v?.status === "working" && "claw-desk-on")}
                style={{ transform: "translateY(2px)" }}
              >
                <svg width="58" height="50" viewBox="0 0 32 28" className="claw-sprite" shapeRendering="crispEdges">
                  <g dangerouslySetInnerHTML={{ __html: def.deskTool }} />
                  <rect x="1" y="18" width="30" height="3" fill="#a9742b" />
                  <rect x="1" y="17" width="30" height="1" fill="#c4915a" />
                  <rect x="3" y="21" width="2" height="6" fill="#84591c" />
                  <rect x="27" y="21" width="2" height="6" fill="#84591c" />
                </svg>
              </div>
              <div className="absolute left-[8px] top-[6px] w-[120px]">
                <div className="inline-flex items-center gap-1.5 rounded-[4px] border border-ink/30 bg-surface/90 px-1.5 py-[1px]">
                  <span
                    className={clsx(
                      "h-[5px] w-[5px] rounded-full",
                      v?.status === "working" ? "animate-pixpulse bg-accent" : v?.status === "done" ? "bg-grass" : "bg-line-2",
                    )}
                  />
                  <span className="font-mono text-[10px] font-bold leading-none text-ink">{def.zh}</span>
                  {(todosByRole[def.key]?.length ?? 0) > 0 && (
                    <span className="font-mono text-[9px] leading-none text-muted">
                      {todosByRole[def.key].filter((t) => t.status === "done").length}/{todosByRole[def.key].length}
                    </span>
                  )}
                </div>
                <div
                  className={clsx(
                    "mt-[2px] max-w-[118px] truncate font-mono text-[9px] leading-tight",
                    v?.status === "working" ? "text-accent" : v?.status === "done" ? "text-grass" : "text-muted",
                  )}
                >
                  {v?.status === "working"
                    ? v.detail || "工作中"
                    : v?.status === "done"
                      ? `✓ ×${v.calls} · ${fmtDuration(v.totalMs)}`
                      : ""}
                </div>
              </div>
            </div>
          );
        })}

        {/* characters */}
        {WORKERS.map((def, i) => (
          <OfficeWorker
            key={def.key}
            def={def}
            view={views[def.key]}
            loungeIndex={i}
            offDuty={offDuty}
            onClick={() => setFocus((f) => (f === def.key ? null : def.key))}
          />
        ))}

        {/* handoff flights — documents flying between desks */}
        {flights.map((f) => (
          <FlyingDoc key={f.id} flight={f} onDone={(id) => setFlights((fs) => fs.filter((x) => x.id !== id))} />
        ))}

        {/* live ticker + phase strip */}
        <div className="absolute left-3 top-2 z-40 flex items-center gap-2">
          <span className="font-pixel text-[0.55rem] tracking-wide text-accent">✦ CLAW OFFICE</span>
          <span className="inline-flex items-center gap-1 font-mono text-[10px] text-muted">
            <i className="h-[6px] w-[6px] animate-pixpulse rounded-full bg-grass" />
            LIVE
          </span>
        </div>
        <PhaseStrip views={views} finished={run.status === "finished"} />
        {(offDuty || lastActivity) && (
          <div className="absolute bottom-2 left-3 z-40 max-w-[55%] truncate rounded-[4px] border border-ink/25 bg-surface/90 px-2 py-[3px] font-mono text-[10px] text-ink-2">
            {offDuty ? "✓ 收工!全员下班 — 作品包在下方 dock" : lastActivity}
          </div>
        )}
      </div>

      {/* ── worker stats popover ───────────────────────────────────── */}
      {focusedDef && (
        <div className="absolute right-3 top-9 z-50 w-[210px] rounded-pixel border-2 border-ink bg-surface p-3 shadow-pixel">
          <div className="mb-1.5 flex items-center justify-between">
            <span className="font-mono text-[12px] font-bold text-ink">
              {focusedDef.zh} <span className="text-[10px] font-normal text-muted">{focusedDef.name}</span>
            </span>
            <button
              type="button"
              onClick={() => setFocus(null)}
              className="grid h-5 w-5 place-items-center rounded-pixel border border-ink/40 text-ink-2 hover:text-ink"
              aria-label="关闭"
            >
              <X size={10} strokeWidth={2.4} />
            </button>
          </div>
          <dl className="space-y-1 font-mono text-[11px] text-ink-2">
            <div className="flex justify-between">
              <dt className="text-muted">状态</dt>
              <dd>
                {focusedView?.status === "working" ? "工作中" : focusedView?.status === "done" ? "已完成" : "待命"}
              </dd>
            </div>
            {focusedView?.detail && (
              <div className="flex justify-between gap-2">
                <dt className="flex-none text-muted">在做</dt>
                <dd className="truncate text-right">{focusedView.detail}</dd>
              </div>
            )}
            <div className="flex justify-between">
              <dt className="text-muted">出工</dt>
              <dd>
                ×{focusedView?.calls ?? 0} · {fmtDuration(focusedView?.totalMs ?? 0)}
              </dd>
            </div>
          </dl>
          {focusedTodos.length > 0 && (
            <div className="mt-2 border-t-2 border-line pt-1.5">
              <p className="mb-1 font-mono text-[10px] font-bold tracking-wide text-muted">待办清单</p>
              <ul className="space-y-1">
                {focusedTodos.map((t, i) => (
                  <li key={i} className="flex items-start gap-1.5 font-mono text-[10.5px] leading-snug">
                    <span
                      className={clsx(
                        "mt-[1px] flex-none",
                        t.status === "done"
                          ? "text-grass"
                          : t.status === "doing"
                            ? "animate-pixpulse text-accent"
                            : t.status === "skipped"
                              ? "text-line-2"
                              : "text-muted",
                      )}
                    >
                      {t.status === "done" ? "■✓" : t.status === "doing" ? "▶" : t.status === "skipped" ? "⨯" : "□"}
                    </span>
                    <span
                      className={clsx(
                        t.status === "done" ? "text-ink-2" : t.status === "skipped" ? "text-line-2 line-through" : "text-ink",
                      )}
                    >
                      {t.title}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      {/* ── dock ───────────────────────────────────────────────────── */}
      <div className="absolute bottom-2 left-1/2 z-40 flex -translate-x-1/2 items-center gap-1.5 rounded-pixel border-2 border-ink bg-surface/95 px-2 py-1.5 shadow-pixel-sm">
        <DockButton
          icon={<FileText size={13} strokeWidth={2} />}
          label={`报告${(run.artifact_version ?? 0) > 0 ? ` v${run.artifact_version}` : ""}`}
          active={winOpen && tab === "report"}
          onClick={() => openTab("report")}
        />
        <DockButton
          icon={<ImageIcon size={13} strokeWidth={2} />}
          label={`配图${(run.figures?.length ?? 0) > 0 ? ` ${run.figures!.length}` : ""}`}
          disabled={(run.figures?.length ?? 0) === 0}
          active={winOpen && tab === "figures"}
          onClick={() => openTab("figures")}
        />
        <DockButton
          icon={<Layers size={13} strokeWidth={2} />}
          label="Deck"
          disabled={!run.deck}
          active={winOpen && tab === "deck"}
          onClick={() => openTab("deck")}
        />
      </div>

      {/* ── the work-package window ────────────────────────────────── */}
      {winOpen && (
        <PixelWindow
          title={`作品包 · ~/claw/${run.job_id.slice(0, 6)}`}
          z={100}
          onClose={() => setWinOpen(false)}
          onFocus={() => {}}
        >
          <ArtifactBody
            jobId={run.job_id}
            initialVersion={run.artifact_version ?? 0}
            figures={run.figures ?? []}
            deck={run.deck ?? null}
            tab={tab}
            onTabChange={setTab}
          />
        </PixelWindow>
      )}
    </div>
  );
}

// OfficeWorker is one living character: walks lounge ↔ desk on status
// changes, loops its action pool while working (the running tool's signature
// gesture wins), pops emote bubbles, greyed when idle.
function OfficeWorker({
  def,
  view,
  loungeIndex,
  offDuty,
  onClick,
}: {
  def: WorkerDef;
  view?: WorkerView;
  loungeIndex: number;
  offDuty: boolean;
  onClick: () => void;
}) {
  const status = view?.status ?? "idle";
  const atDesk = status !== "idle" && !offDuty;
  const st = STATIONS[def.key] ?? LOUNGE;
  const lounge = { x: LOUNGE.x + loungeIndex * 4.4, y: LOUNGE.y - (loungeIndex % 2) * 3.5 };

  // Wandering: a worker is not glued to its spot. Between (and briefly
  // during) jobs it strolls to a waypoint — coffee machine, plant,
  // whiteboard, or a colleague's desk for a chat — lingers with a fitting
  // gesture, then walks back. Working workers only take quick coffee runs.
  const [trip, setTrip] = useState<{ x: number; y: number; g: string } | null>(null);
  useEffect(() => {
    if (offDuty) {
      setTrip(null);
      return;
    }
    let dead = false;
    const timers: ReturnType<typeof setTimeout>[] = [];
    const later = (fn: () => void, ms: number) => {
      const t = setTimeout(() => !dead && fn(), ms);
      timers.push(t);
    };
    const pick = (): { x: number; y: number; g: string } => {
      // working → always a quick coffee; otherwise waypoint or colleague visit
      if (status === "working") return WAYPOINTS[0];
      if (Math.random() < 0.45) {
        const others = WORKERS.filter((w) => w.key !== def.key);
        const mate = others[Math.floor(Math.random() * others.length)];
        const ms = STATIONS[mate.key] ?? LOUNGE;
        return { x: Math.max(1, ms.x - 5.5), y: ms.y, g: "talk" };
      }
      return WAYPOINTS[Math.floor(Math.random() * WAYPOINTS.length)];
    };
    const schedule = () => {
      const delay =
        status === "working"
          ? 26000 + Math.random() * 22000
          : status === "idle"
            ? 8000 + Math.random() * 9000
            : 14000 + Math.random() * 14000;
      later(() => {
        setTrip(pick());
        const linger = status === "working" ? 2400 : 3600 + Math.random() * 2600;
        later(() => {
          setTrip(null);
          later(schedule, 500);
        }, 1150 + linger);
      }, delay);
    };
    schedule();
    return () => {
      dead = true;
      timers.forEach(clearTimeout);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, offDuty]);

  const pos = trip ?? (atDesk ? st : lounge);

  const [walking, setWalking] = useState(false);
  const [cycle, setCycle] = useState<string>("look");
  const [pokedUntil, setPokedUntil] = useState(0);
  const walkTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cycleTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  // Any position change (desk↔lounge↔waypoint) plays the walk cycle.
  useEffect(() => {
    setWalking(true);
    if (walkTimer.current) clearTimeout(walkTimer.current);
    walkTimer.current = setTimeout(() => setWalking(false), 1150);
  }, [pos.x, pos.y]);

  useEffect(() => {
    if (cycleTimer.current) clearInterval(cycleTimer.current);
    if (offDuty) {
      // 下班派对 — everyone celebrates at the lounge, staggered so it reads
      // as a crowd, not synchronized clones.
      let i = 0;
      const party = ["cheer", "dance", "coffee", "talk", "dance", "doze"];
      setCycle(party[loungeIndex % party.length]);
      cycleTimer.current = setInterval(() => {
        i += 1;
        setCycle(party[(loungeIndex + i) % party.length]);
      }, 2400);
    } else if (status === "working") {
      let i = 0;
      setCycle(def.actions[0]);
      cycleTimer.current = setInterval(() => {
        i += 1;
        setCycle(def.actions[i % def.actions.length]);
      }, 1700);
    } else if (status === "idle") {
      // lounge idling: coffee, gossip, the occasional nap
      let i = 0;
      const loungePool = ["coffee", "talk", "doze", "look", "dance", "think"];
      setCycle(loungePool[loungeIndex % loungePool.length]);
      cycleTimer.current = setInterval(() => {
        i += 1;
        setCycle(loungePool[(loungeIndex + i) % loungePool.length]);
      }, 4200);
    } else {
      setCycle("nod");
    }
    return () => {
      if (cycleTimer.current) clearInterval(cycleTimer.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, offDuty]);

  // Click = poke: the worker waves back (♥) for a beat, on top of whatever
  // it was doing. Pure fun.
  const poked = pokedUntil > 0;
  useEffect(() => {
    if (!poked) return;
    const t = setTimeout(() => setPokedUntil(0), 1500);
    return () => clearTimeout(t);
  }, [pokedUntil, poked]);

  const baseGesture = walking
    ? ""
    : trip
      ? trip.g
      : status === "working" && !offDuty
        ? view?.gesture || cycle
        : cycle;
  const gesture = poked && !walking ? "wave" : baseGesture;
  const emoKey =
    !walking && gesture && (poked || status === "working" || offDuty || status === "idle")
      ? ACT[gesture]?.emo
      : undefined;

  return (
    <button
      type="button"
      onClick={() => {
        setPokedUntil(Date.now());
        onClick();
      }}
      className="absolute cursor-pointer border-0 bg-transparent p-0"
      style={{
        left: `${pos.x}%`,
        top: `${pos.y}%`,
        transform: "translateY(-100%)",
        transition: "left 1.1s cubic-bezier(0.45,0.05,0.3,1), top 1.1s cubic-bezier(0.45,0.05,0.3,1)",
        zIndex: Math.round(pos.y) + 1,
      }}
      aria-label={`${def.zh} · ${status}`}
    >
      {emoKey && (
        <div
          key={gesture}
          className="claw-emote claw-emote-show absolute -top-6 left-1/2 z-10 flex h-5 min-w-[18px] items-center justify-center rounded-[5px] border-2 border-ink bg-surface px-1 font-mono text-[10px] font-extrabold text-ink shadow-pixel-sm"
        >
          {EMO[emoKey]}
        </div>
      )}
      <span className="absolute -bottom-[3px] left-1/2 h-[5px] w-[32px] -translate-x-1/2 rounded-[50%] bg-ink/15" />
      <WorkerSprite
        def={def}
        gesture={gesture}
        walking={walking}
        grey={status === "idle" && !offDuty && !poked}
        size={46}
      />
    </button>
  );
}

// FlyingDoc animates one handoff: a pixel document glides from the upstream
// agent's desk to the downstream one, then despawns.
function FlyingDoc({ flight, onDone }: { flight: Flight; onDone: (id: number) => void }) {
  const [pos, setPos] = useState(flight.from);
  const [go, setGo] = useState(false);

  useEffect(() => {
    const start = setTimeout(() => {
      setGo(true);
      setPos(flight.to);
    }, 60 + flight.delay);
    const end = setTimeout(() => onDone(flight.id), 1280 + flight.delay);
    return () => {
      clearTimeout(start);
      clearTimeout(end);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div
      className="pointer-events-none absolute z-[60]"
      style={{
        left: `calc(${pos.x}% + 56px)`,
        top: `${pos.y - 9}%`,
        transition: go ? "left 1.05s cubic-bezier(0.4,0,0.45,1), top 1.05s cubic-bezier(0.4,0,0.45,1)" : undefined,
      }}
    >
      <div className="claw-doc-fly">
        <svg width="16" height="18" viewBox="0 0 8 9" shapeRendering="crispEdges">
          <rect x="0.5" y="0.5" width="7" height="8" fill="#fbfaf2" stroke="#16140f" strokeWidth="0.8" />
          <rect x="2" y="2" width="4" height="0.9" fill="#6a55ff" />
          <rect x="2" y="4" width="4" height="0.8" fill="#c9c3b5" />
          <rect x="2" y="5.6" width="3" height="0.8" fill="#c9c3b5" />
        </svg>
      </div>
    </div>
  );
}

// PhaseStrip is the pipeline at a glance: 规划 → 执行 → 撰写 → 评审 → 制片,
// each stage lit from the real worker states (working = pulsing accent,
// done = green check, pending = grey).
function PhaseStrip({ views, finished }: { views: Record<string, WorkerView>; finished: boolean }) {
  return (
    <div className="absolute right-3 top-2 z-40 flex items-center gap-1 rounded-[5px] border border-ink/25 bg-surface/90 px-1.5 py-1">
      {PHASES.map((ph, i) => {
        const sts = ph.workers.map((k) => views[k]?.status ?? "idle");
        const working = sts.includes("working");
        const touched = ph.workers.some((k) => (views[k]?.calls ?? 0) > 0 || views[k]?.status === "done");
        const done = !working && (finished ? touched : touched && sts.every((s) => s !== "working"));
        return (
          <span key={ph.zh} className="flex items-center gap-1">
            {i > 0 && <span className="font-mono text-[9px] text-line-2">▸</span>}
            <span
              className={clsx(
                "rounded-[3px] px-1.5 py-[1px] font-mono text-[9.5px] font-bold leading-none",
                working
                  ? "animate-pixpulse bg-accent text-white"
                  : done
                    ? "bg-grass/15 text-grass"
                    : "text-muted",
              )}
            >
              {ph.zh}
              {done && " ✓"}
            </span>
          </span>
        );
      })}
    </div>
  );
}

function DockButton({
  icon,
  label,
  active,
  disabled,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  active?: boolean;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={clsx(
        "inline-flex items-center gap-1.5 rounded-pixel border-2 px-2.5 py-1 font-mono text-[11px] font-semibold transition-colors",
        active
          ? "border-ink bg-accent text-white shadow-pixel-sm"
          : disabled
            ? "cursor-not-allowed border-line-2 bg-surface text-line-2"
            : "border-ink bg-surface text-ink-2 hover:text-ink",
      )}
    >
      {icon}
      {label}
    </button>
  );
}

// Decor — the props that make the room read as an office: windows with sky,
// a whiteboard, a clock, a plant, and the coffee lounge bottom-left.
function Decor() {
  return (
    <>
      <svg className="absolute left-[10%] top-[5%] claw-sprite" width="74" height="56" viewBox="0 0 37 28" shapeRendering="crispEdges">
        <rect x="0" y="0" width="37" height="28" fill="#16140f" />
        <rect x="2" y="2" width="15" height="11" fill="#bfe3f2" />
        <rect x="20" y="2" width="15" height="11" fill="#bfe3f2" />
        <rect x="2" y="15" width="15" height="11" fill="#cfeaf6" />
        <rect x="20" y="15" width="15" height="11" fill="#cfeaf6" />
        <rect x="5" y="4" width="6" height="2" fill="#fff" />
        <rect x="24" y="7" width="7" height="2" fill="#fff" />
      </svg>
      <svg className="absolute left-[42%] top-[7%] claw-sprite" width="120" height="52" viewBox="0 0 60 26" shapeRendering="crispEdges">
        <rect x="0" y="0" width="60" height="26" fill="#16140f" />
        <rect x="1" y="1" width="58" height="24" fill="#fbfaf2" />
        <rect x="5" y="5" width="28" height="2" fill="#6a55ff" />
        <rect x="5" y="10" width="40" height="1.6" fill="#c9c3b5" />
        <rect x="5" y="14" width="34" height="1.6" fill="#c9c3b5" />
        <rect x="5" y="18" width="38" height="1.6" fill="#c9c3b5" />
        <rect x="46" y="14" width="9" height="6" fill="#3ea96a" />
      </svg>
      <svg className="absolute right-[6%] top-[6%] claw-sprite" width="34" height="34" viewBox="0 0 17 17" shapeRendering="crispEdges">
        <rect x="0" y="0" width="17" height="17" fill="#16140f" />
        <rect x="1.5" y="1.5" width="14" height="14" fill="#fbfaf2" />
        <rect x="8" y="4" width="1.4" height="5" fill="#16140f" />
        <rect x="8" y="8" width="4" height="1.4" fill="#b5371e" />
      </svg>
      {/* coffee lounge */}
      <svg className="absolute claw-sprite" style={{ left: "1.5%", top: "94%", transform: "translateY(-100%)" }} width="46" height="58" viewBox="0 0 23 29" shapeRendering="crispEdges">
        <rect x="2" y="4" width="14" height="20" fill="#403a30" />
        <rect x="3" y="5" width="12" height="7" fill="#16140f" />
        <rect x="4" y="6" width="4" height="2" fill="#74e6a0" />
        <rect x="6" y="14" width="6" height="4" fill="#16140f" />
        <rect x="7" y="18" width="4" height="1" fill="#e3a13a" />
        <rect x="0" y="24" width="20" height="2" fill="#84591c" />
        <rect x="1" y="26" width="2" height="3" fill="#84591c" />
        <rect x="17" y="26" width="2" height="3" fill="#84591c" />
      </svg>
      {/* plant */}
      <svg className="absolute claw-sprite" style={{ right: "2%", top: "96%", transform: "translateY(-100%)" }} width="40" height="56" viewBox="0 0 20 28" shapeRendering="crispEdges">
        <rect x="7" y="2" width="6" height="6" fill="#3ea96a" />
        <rect x="4" y="6" width="6" height="6" fill="#4fbf7c" />
        <rect x="10" y="7" width="6" height="6" fill="#2f8f58" />
        <rect x="8" y="13" width="4" height="5" fill="#6a4a23" />
        <rect x="5" y="18" width="10" height="8" fill="#b5371e" />
        <rect x="5" y="18" width="10" height="2" fill="#d8654a" />
      </svg>
    </>
  );
}
