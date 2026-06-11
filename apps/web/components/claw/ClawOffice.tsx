"use client";

import { useEffect, useRef, useState } from "react";
import { FileText, Image as ImageIcon, Layers, Users, X, GripVertical, Shuffle, ChevronLeft, ChevronRight } from "lucide-react";
import clsx from "clsx";

import type { ClawRun } from "@/lib/api";
import { fmtDuration } from "@/components/chat/ToolProgress";
import { useAgentEventStream, type AgentEvent } from "@/components/chat/transport";
import { ACT, EMO, TOOL_ACTION, WORKERS, type WorkerDef, type WorkerPhase } from "./workers";
import { WorkerSprite } from "./WorkerSprite";
import { PixelWindow } from "./PixelWindow";
import { ArtifactBody, type WorkTab } from "./ArtifactPanel";
import { useWorkerStates, type WorkerView } from "./useWorkerStates";
import { useOfficeSim, OFFICE_CONFIG, STATIONS, MEETING_TABLE, type SimView } from "./officeSim";
import { useWardrobe, OUTFITS } from "./outfits";

// ClawOffice is the full-page Sims-style office. A central tick engine
// (officeSim) moves every worker along corridor paths, books slots so nobody
// overlaps, pairs face-to-face chats, and stages the kickoff MEETING (on
// claw.plan or the dock button) plus the off-duty lounge party. This
// component is the stage: room, desks, meeting table, dock (draggable,
// snaps to anchors), worker stats/wardrobe popover, and the work-package
// window floating on top.

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

type Flight = { id: number; from: { x: number; y: number }; to: { x: number; y: number }; delay: number };
let flightSeq = 1;

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

type DockAnchor = "bc" | "br" | "tr";
const DOCK_ANCHORS: Record<DockAnchor, string> = {
  bc: "bottom-2 left-1/2 -translate-x-1/2",
  br: "bottom-2 right-3",
  tr: "top-9 right-3",
};

export function ClawOffice({ run }: { run: ClawRun }) {
  const stream = useAgentEventStream();
  const { views, lastActivity } = useWorkerStates(run.plan ?? []);
  const { defs, outfitOf, setOutfit } = useWardrobe();
  const [focus, setFocus] = useState<string | null>(null);
  const [winOpen, setWinOpen] = useState(false);
  const [tab, setTab] = useState<WorkTab>("report");
  const autoOpened = useRef(false);

  // 下班 party
  const [offDuty, setOffDuty] = useState(false);
  useEffect(() => {
    if (run.status === "finished") {
      const t = setTimeout(() => setOffDuty(true), 5200);
      return () => clearTimeout(t);
    }
    setOffDuty(false);
  }, [run.status]);

  // 会议室: kickoff meeting when the plan lands; dock button re-convenes.
  const [meetingUntil, setMeetingUntil] = useState(0);
  useEffect(
    () =>
      stream.subscribe((ev: AgentEvent) => {
        if (ev.kind === "claw.plan") setMeetingUntil(Date.now() + OFFICE_CONFIG.meetingMs);
      }),
    [stream],
  );
  const meeting = meetingUntil > Date.now();

  // the sim drives every body in the room
  const sim = useOfficeSim({ views, offDuty, meetingUntil });

  // handoff flights on real start transitions
  const [flights, setFlights] = useState<Flight[]>([]);
  const prevStatus = useRef<Record<string, string> | null>(null);
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

  // auto-open the report window once
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

  // draggable dock with persisted anchor
  const [dockAnchor, setDockAnchor] = useState<DockAnchor>("bc");
  useEffect(() => {
    const saved = localStorage.getItem(OFFICE_CONFIG.storage.dock) as DockAnchor | null;
    if (saved && DOCK_ANCHORS[saved]) setDockAnchor(saved);
  }, []);
  const sceneRef = useRef<HTMLDivElement>(null);
  const onDockDrag = (e: React.PointerEvent) => {
    const scene = sceneRef.current;
    if (!scene) return;
    const move = (ev: PointerEvent) => ev.preventDefault();
    const up = (ev: PointerEvent) => {
      const r = scene.getBoundingClientRect();
      const px = (ev.clientX - r.left) / r.width;
      const py = (ev.clientY - r.top) / r.height;
      const next: DockAnchor = py < 0.4 ? "tr" : px > 0.66 ? "br" : "bc";
      setDockAnchor(next);
      try {
        localStorage.setItem(OFFICE_CONFIG.storage.dock, next);
      } catch {
        /* no persistence */
      }
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    e.preventDefault();
  };

  // per-agent todos from the role-tagged plan
  const todosByRole = (() => {
    const m: Record<string, { title: string; status: string }[]> = {};
    for (const t of run.plan ?? []) {
      if (t.role) (m[t.role] ??= []).push({ title: t.title, status: t.status });
    }
    return m;
  })();

  const focusedDef = focus ? defs.find((w) => w.key === focus) : undefined;
  const focusedView = focus ? views[focus] : undefined;
  const focusedTodos = focus ? (todosByRole[focus] ?? []) : [];

  return (
    <div
      ref={sceneRef}
      className="relative h-full min-h-[420px] overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel"
    >
      {/* ── the room ───────────────────────────────────────────────── */}
      <div className="absolute inset-0">
        <div className="absolute inset-x-0 top-0 h-[30%] bg-surface-2" />
        <div className="absolute inset-x-0 top-[30%] h-[3px] bg-ink/15" />
        <div
          className="absolute inset-x-0 bottom-0 top-[30%] bg-[#e7dfcd]"
          style={{
            backgroundImage: "repeating-linear-gradient(0deg, rgba(22,20,15,0.05) 0 1px, transparent 1px 26px)",
          }}
        />
        <Decor />

        {/* meeting table (between the two slot rows for depth) */}
        <div
          className="absolute"
          style={{
            left: `${MEETING_TABLE.x}%`,
            top: `${MEETING_TABLE.y}%`,
            transform: "translateY(-58%)",
            zIndex: Math.round(MEETING_TABLE.y),
          }}
        >
          <svg width="240" height="64" viewBox="0 0 96 26" className="claw-sprite" shapeRendering="crispEdges">
            <rect x="2" y="4" width="92" height="12" fill="#a9742b" />
            <rect x="2" y="3" width="92" height="2" fill="#c4915a" />
            <rect x="6" y="16" width="4" height="9" fill="#84591c" />
            <rect x="86" y="16" width="4" height="9" fill="#84591c" />
            <rect x="22" y="6" width="8" height="5" fill="#fbfaf2" />
            <rect x="46" y="7" width="9" height="4" fill="#fbfaf2" />
            <rect x="68" y="6" width="7" height="5" fill="#fbfaf2" />
          </svg>
          {meeting && (
            <span className="absolute -top-5 left-1/2 -translate-x-1/2 whitespace-nowrap rounded-[4px] border border-ink/40 bg-accent px-1.5 py-[1px] font-mono text-[9.5px] font-bold text-white">
              例会中 · 对齐分工
            </span>
          )}
        </div>

        {/* desks + nameplates */}
        {defs.map((def) => {
          const st = STATIONS[def.key];
          if (!st) return null;
          const v = views[def.key];
          const todos = todosByRole[def.key];
          return (
            <div key={def.key} className="absolute" style={{ left: `${st.x}%`, top: `${st.y}%`, zIndex: Math.round(st.y) }}>
              {v?.status === "working" && (
                <div className="absolute -left-[14px] top-[-8px] h-[10px] w-[148px] rounded-[4px] bg-accent/20" />
              )}
              <div
                className={clsx("absolute bottom-0 left-[62px]", v?.status === "working" && "claw-desk-on")}
                style={{ transform: "translateY(2px)" }}
              >
                <svg
                  width={OFFICE_CONFIG.deskSize}
                  height={Math.round(OFFICE_CONFIG.deskSize * 0.875)}
                  viewBox="0 0 32 28"
                  className="claw-sprite"
                  shapeRendering="crispEdges"
                >
                  <g dangerouslySetInnerHTML={{ __html: def.deskTool }} />
                  <rect x="1" y="18" width="30" height="3" fill="#a9742b" />
                  <rect x="1" y="17" width="30" height="1" fill="#c4915a" />
                  <rect x="3" y="21" width="2" height="6" fill="#84591c" />
                  <rect x="27" y="21" width="2" height="6" fill="#84591c" />
                </svg>
              </div>
              <div className="absolute left-[10px] top-[8px] w-[132px]">
                <div className="inline-flex items-center gap-1.5 rounded-[4px] border border-ink/30 bg-surface/90 px-1.5 py-[1px]">
                  <span
                    className={clsx(
                      "h-[5px] w-[5px] rounded-full",
                      v?.status === "working" ? "animate-pixpulse bg-accent" : v?.status === "done" ? "bg-grass" : "bg-line-2",
                    )}
                  />
                  <span className="font-mono text-[10.5px] font-bold leading-none text-ink">{def.zh}</span>
                  {(todos?.length ?? 0) > 0 && (
                    <span className="font-mono text-[9px] leading-none text-muted">
                      {todos.filter((t) => t.status === "done").length}/{todos.length}
                    </span>
                  )}
                </div>
                <div
                  className={clsx(
                    "mt-[2px] max-w-[128px] truncate font-mono text-[9.5px] leading-tight",
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

        {/* characters — positions/facing/gesture come from the sim */}
        {defs.map((def) => (
          <OfficeWorker
            key={def.key}
            def={def}
            view={views[def.key]}
            sim={sim[def.key]}
            offDuty={offDuty}
            onClick={() => setFocus((f) => (f === def.key ? null : def.key))}
          />
        ))}

        {/* handoff flights */}
        {flights.map((f) => (
          <FlyingDoc key={f.id} flight={f} onDone={(id) => setFlights((fs) => fs.filter((x) => x.id !== id))} />
        ))}

        {/* HUD: title, ticker, phase strip */}
        <div className="absolute left-3 top-2 z-40 flex items-center gap-2">
          <span className="font-pixel text-[0.55rem] tracking-wide text-accent">✦ CLAW OFFICE</span>
          <span className="inline-flex items-center gap-1 font-mono text-[10px] text-muted">
            <i className="h-[6px] w-[6px] animate-pixpulse rounded-full bg-grass" />
            LIVE
          </span>
        </div>
        {(offDuty || meeting || lastActivity) && (
          <div className="absolute bottom-2 left-3 z-40 max-w-[48%] truncate rounded-[4px] border border-ink/25 bg-surface/90 px-2 py-[3px] font-mono text-[10px] text-ink-2">
            {meeting ? "▶ 开工例会 — 调度员对齐分工中" : offDuty ? "✓ 收工!全员下班 — 作品包在 dock" : lastActivity}
          </div>
        )}
        <PhaseStrip views={views} finished={run.status === "finished"} />
      </div>

      {/* ── stats / wardrobe / bindings popover ─────────────────────── */}
      {focusedDef && (
        <div className="absolute right-3 top-9 z-50 w-[228px] rounded-pixel border-2 border-ink bg-surface p-3 shadow-pixel">
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
              <dd>{focusedView?.status === "working" ? "工作中" : focusedView?.status === "done" ? "已完成" : "待命"}</dd>
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
                    <span className={clsx(t.status === "done" ? "text-ink-2" : t.status === "skipped" ? "text-line-2 line-through" : "text-ink")}>
                      {t.title}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* 换装 */}
          <div className="mt-2 border-t-2 border-line pt-1.5">
            <p className="mb-1 font-mono text-[10px] font-bold tracking-wide text-muted">换装</p>
            <div className="flex items-center gap-1">
              <button
                type="button"
                aria-label="上一套"
                onClick={() => setOutfit(focusedDef.key, (outfitOf(focusedDef.key) + OUTFITS.length) % (OUTFITS.length + 1) - 1)}
                className="grid h-5 w-5 place-items-center rounded-pixel border border-ink/40 text-ink-2 hover:text-ink"
              >
                <ChevronLeft size={11} strokeWidth={2.4} />
              </button>
              <span className="flex-1 truncate text-center font-mono text-[11px] text-ink">
                {outfitOf(focusedDef.key) < 0 ? "默认制服" : OUTFITS[outfitOf(focusedDef.key)].name}
              </span>
              <button
                type="button"
                aria-label="下一套"
                onClick={() => setOutfit(focusedDef.key, outfitOf(focusedDef.key) + 1 >= OUTFITS.length ? -1 : outfitOf(focusedDef.key) + 1)}
                className="grid h-5 w-5 place-items-center rounded-pixel border border-ink/40 text-ink-2 hover:text-ink"
              >
                <ChevronRight size={11} strokeWidth={2.4} />
              </button>
              <button
                type="button"
                aria-label="随机换装"
                onClick={() => setOutfit(focusedDef.key, Math.floor(Math.random() * OUTFITS.length))}
                className="grid h-5 w-5 place-items-center rounded-pixel border border-ink/40 text-ink-2 hover:text-ink"
              >
                <Shuffle size={10} strokeWidth={2.2} />
              </button>
            </div>
          </div>

          {/* 绑定 — role ↔ phase ↔ tools, in the open */}
          <div className="mt-2 border-t-2 border-line pt-1.5">
            <p className="mb-1 font-mono text-[10px] font-bold tracking-wide text-muted">绑定</p>
            <div className="flex flex-wrap gap-1">
              <span className="rounded-[3px] bg-accent-soft px-1.5 py-[1px] font-mono text-[9.5px] font-bold text-accent">
                {PHASE_ZH[focusedDef.phase]}
              </span>
              {focusedDef.tools.length === 0 && (
                <span className="rounded-[3px] bg-surface-2 px-1.5 py-[1px] font-mono text-[9.5px] text-muted">事件归属</span>
              )}
              {focusedDef.tools.map((t) => (
                <span key={t} className="rounded-[3px] border border-line-2 bg-surface-2 px-1.5 py-[1px] font-mono text-[9.5px] text-ink-2">
                  {t}
                  {TOOL_ACTION[t] && ACT[TOOL_ACTION[t]] ? ` · ${ACT[TOOL_ACTION[t]].zh}` : ""}
                </span>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* ── dock (draggable, snaps to anchors) ──────────────────────── */}
      <div className={clsx("absolute z-40 flex items-center gap-1.5 rounded-pixel border-2 border-ink bg-surface/95 px-1.5 py-1.5 shadow-pixel-sm", DOCK_ANCHORS[dockAnchor])}>
        <span
          onPointerDown={onDockDrag}
          className="grid h-6 w-4 cursor-grab touch-none place-items-center text-line-2 hover:text-ink-2 active:cursor-grabbing"
          aria-label="拖动 dock"
        >
          <GripVertical size={12} strokeWidth={2} />
        </span>
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
        <DockButton
          icon={<Users size={13} strokeWidth={2} />}
          label="例会"
          active={meeting}
          onClick={() => setMeetingUntil(Date.now() + OFFICE_CONFIG.meetingMs)}
        />
      </div>

      {/* ── the work-package window ────────────────────────────────── */}
      {winOpen && (
        <PixelWindow title={`作品包 · ~/claw/${run.job_id.slice(0, 6)}`} z={100} onClose={() => setWinOpen(false)} onFocus={() => {}}>
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

// OfficeWorker is now purely presentational: the sim hands position/facing/
// walking/gesture; this renders the sprite (with outer facing-flip wrapper —
// NOT on .claw-rig, which gestures animate), shadow, emote, and the poke.
function OfficeWorker({
  def,
  view,
  sim,
  offDuty,
  onClick,
}: {
  def: WorkerDef;
  view?: WorkerView;
  sim?: SimView;
  offDuty: boolean;
  onClick: () => void;
}) {
  const status = view?.status ?? "idle";
  const [pokedUntil, setPokedUntil] = useState(0);
  const poked = pokedUntil > 0;
  useEffect(() => {
    if (!poked) return;
    const t = setTimeout(() => setPokedUntil(0), 1500);
    return () => clearTimeout(t);
  }, [pokedUntil, poked]);

  if (!sim) return null;
  const gesture = poked && !sim.walking ? "wave" : sim.gesture;
  const emoKey = !sim.walking && gesture ? ACT[gesture]?.emo : undefined;

  return (
    <button
      type="button"
      onClick={() => {
        setPokedUntil(Date.now());
        onClick();
      }}
      className="absolute cursor-pointer border-0 bg-transparent p-0"
      style={{
        left: `${sim.x}%`,
        top: `${sim.y}%`,
        transform: "translateY(-100%)",
        transition: "left 0.12s linear, top 0.12s linear",
        zIndex: Math.round(sim.y) + 1,
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
      <span className="absolute -bottom-[3px] left-1/2 h-[5px] w-[36px] -translate-x-1/2 rounded-[50%] bg-ink/15" />
      <span className="block" style={{ transform: `scaleX(${sim.facing})` }}>
        <WorkerSprite
          def={def}
          gesture={gesture}
          walking={sim.walking}
          grey={status === "idle" && !offDuty && !poked}
          size={OFFICE_CONFIG.spriteSize}
        />
      </span>
    </button>
  );
}

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
        left: `calc(${pos.x}% + 64px)`,
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
                working ? "animate-pixpulse bg-accent text-white" : done ? "bg-grass/15 text-grass" : "text-muted",
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
        active ? "border-ink bg-accent text-white shadow-pixel-sm" : disabled ? "cursor-not-allowed border-line-2 bg-surface text-line-2" : "border-ink bg-surface text-ink-2 hover:text-ink",
      )}
    >
      {icon}
      {label}
    </button>
  );
}

// Decor — windows, whiteboard, clock, coffee machine (lounge), plant.
function Decor() {
  return (
    <>
      <svg className="absolute left-[8%] top-[4%] claw-sprite" width="84" height="62" viewBox="0 0 37 28" shapeRendering="crispEdges">
        <rect x="0" y="0" width="37" height="28" fill="#16140f" />
        <rect x="2" y="2" width="15" height="11" fill="#bfe3f2" />
        <rect x="20" y="2" width="15" height="11" fill="#bfe3f2" />
        <rect x="2" y="15" width="15" height="11" fill="#cfeaf6" />
        <rect x="20" y="15" width="15" height="11" fill="#cfeaf6" />
        <rect x="5" y="4" width="6" height="2" fill="#fff" />
        <rect x="24" y="7" width="7" height="2" fill="#fff" />
      </svg>
      <svg className="absolute left-[40%] top-[6%] claw-sprite" width="132" height="56" viewBox="0 0 60 26" shapeRendering="crispEdges">
        <rect x="0" y="0" width="60" height="26" fill="#16140f" />
        <rect x="1" y="1" width="58" height="24" fill="#fbfaf2" />
        <rect x="5" y="5" width="28" height="2" fill="#6a55ff" />
        <rect x="5" y="10" width="40" height="1.6" fill="#c9c3b5" />
        <rect x="5" y="14" width="34" height="1.6" fill="#c9c3b5" />
        <rect x="5" y="18" width="38" height="1.6" fill="#c9c3b5" />
        <rect x="46" y="14" width="9" height="6" fill="#3ea96a" />
      </svg>
      <svg className="absolute right-[5%] top-[5%] claw-sprite" width="38" height="38" viewBox="0 0 17 17" shapeRendering="crispEdges">
        <rect x="0" y="0" width="17" height="17" fill="#16140f" />
        <rect x="1.5" y="1.5" width="14" height="14" fill="#fbfaf2" />
        <rect x="8" y="4" width="1.4" height="5" fill="#16140f" />
        <rect x="8" y="8" width="4" height="1.4" fill="#b5371e" />
      </svg>
      {/* coffee machine (lounge) */}
      <svg className="absolute claw-sprite" style={{ left: "1%", top: "92%", transform: "translateY(-100%)" }} width="52" height="66" viewBox="0 0 23 29" shapeRendering="crispEdges">
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
      <svg className="absolute claw-sprite" style={{ right: "1.5%", top: "74%", transform: "translateY(-100%)" }} width="46" height="64" viewBox="0 0 20 28" shapeRendering="crispEdges">
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
