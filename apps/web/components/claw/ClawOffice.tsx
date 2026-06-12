"use client";

import { useEffect, useRef, useState } from "react";
import { Rnd } from "react-rnd";
import * as Popover from "@radix-ui/react-popover";
import { FileText, Image as ImageIcon, Film, Layers, Users, X, GripVertical, Shuffle, ChevronLeft, ChevronRight, Plug } from "lucide-react";
import clsx from "clsx";

import { getClawRoles, type ClawRun, type ClawRolesConfig } from "@/lib/api";
import { fmtDuration } from "@/components/chat/ToolProgress";
import { useAgentEventStream, type AgentEvent } from "@/components/chat/transport";
import { ACT, EMO, TOOL_ACTION, WORKERS, type WorkerDef, type WorkerPhase } from "./workers";
import { WorkerSprite } from "./WorkerSprite";
import { PixelWindow } from "./PixelWindow";
import { ArtifactBody, type WorkTab } from "./ArtifactPanel";
import { useWorkerStates, type WorkerView } from "./useWorkerStates";
import { useOfficeSim, OFFICE_CONFIG, STATIONS, MEETING_TABLE, type SimView } from "./officeSim";
import { useWardrobe, OUTFITS } from "./outfits";
import { BindingsPanel } from "./BindingsPanel";

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

export function ClawOffice({ run }: { run: ClawRun }) {
  const stream = useAgentEventStream();
  const { views, lastActivity } = useWorkerStates(run.plan ?? []);
  const { defs: wardrobeDefs, outfitOf, setOutfit } = useWardrobe();
  const [focus, setFocus] = useState<string | null>(null);
  const [light, setLight] = useState<"day" | "dusk" | "night">("day");
  // 请喝咖啡 — clicking the machine floats a ☕ over everyone (45s cooldown)
  const [coffee, setCoffee] = useState(false);
  const coffeeCd = useRef(0);
  const brewCoffee = () => {
    const now = Date.now();
    if (now < coffeeCd.current) return;
    coffeeCd.current = now + 45_000;
    setCoffee(true);
    setTimeout(() => setCoffee(false), 6500);
  };
  const [winOpen, setWinOpen] = useState(false);
  const [bindOpen, setBindOpen] = useState(false);
  const [tab, setTab] = useState<WorkTab>("report");
  const autoOpened = useRef(false);

  // 真·动态改绑 — live bindings from the backend, merged over the registry so
  // the popover/plates reflect reality; the BindingsPanel edits them.
  const [rolesCfg, setRolesCfg] = useState<ClawRolesConfig | null>(null);
  useEffect(() => {
    getClawRoles().then(setRolesCfg).catch(() => {});
  }, []);
  const defs = wardrobeDefs.map((d) => {
    const r = rolesCfg?.roles.find((x) => x.key === d.key);
    return r ? { ...d, tools: r.tools } : d;
  });
  const disabledSet = new Set(rolesCfg?.roles.filter((r) => !r.enabled).map((r) => r.key) ?? []);

  // 下班 party
  const [offDuty, setOffDuty] = useState(false);
  useEffect(() => {
    if (run.status === "finished") {
      const t = setTimeout(() => setOffDuty(true), 5200);
      return () => clearTimeout(t);
    }
    setOffDuty(false);
  }, [run.status]);

  // 会议室: two real meetings tied to the pipeline —
  //   • kickoff (claw.plan): coordinator chairs, team aligns on the split
  //   • 评审会 (critic activity): the autonomous critic-revise round is staged
  //     live — the 评审员 takes the head seat and leads while the writer revises.
  // The dock button re-convenes a coordinator-chaired standup.
  const [meetingUntil, setMeetingUntil] = useState(0);
  const [meetingKind, setMeetingKind] = useState<"kickoff" | "review">("kickoff");
  useEffect(
    () =>
      stream.subscribe((ev: AgentEvent) => {
        if (ev.kind === "claw.plan") {
          setMeetingKind("kickoff");
          setMeetingUntil(Date.now() + OFFICE_CONFIG.meetingMs);
        } else if (ev.kind === "claw.debate") {
          // consensus reached — keep the kickoff meeting going a beat longer
          setMeetingKind("kickoff");
          setMeetingUntil(Date.now() + OFFICE_CONFIG.meetingMs);
        } else if (ev.data?.agent === "critic" && (ev.kind === "tool.start" || ev.kind === "tool.end")) {
          // critic is reviewing/rewriting → convene (and keep extending) the 评审会
          setMeetingKind("review");
          setMeetingUntil(Date.now() + OFFICE_CONFIG.meetingMs);
        }
      }),
    [stream],
  );
  const meeting = meetingUntil > Date.now();
  const meetingChair = meetingKind === "review" ? "critic" : WORKERS[0].key;

  // the sim drives every body in the room
  const sim = useOfficeSim({ views, offDuty, meetingUntil, meetingChair });

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

  // draggable dock — free placement via react-rnd, position persisted as
  // scene fractions so it survives resize. Drag by the grip handle only.
  const sceneRef = useRef<HTMLDivElement>(null);
  const dockRnd = useRef<Rnd>(null);
  const [dockPos, setDockPos] = useState<{ x: number; y: number } | null>(null);
  useEffect(() => {
    const scene = sceneRef.current;
    if (!scene) return;
    const id = requestAnimationFrame(() => {
      const el = dockRnd.current?.getSelfElement();
      const dw = el?.offsetWidth ?? 380;
      const dh = el?.offsetHeight ?? 46;
      let fx = 0.5;
      let fy = 0.97; // bottom-center default
      try {
        const s = JSON.parse(localStorage.getItem(OFFICE_CONFIG.storage.dock) || "null");
        if (s && typeof s.fx === "number") {
          fx = s.fx;
          fy = s.fy;
        }
      } catch {
        /* legacy/absent value → default */
      }
      setDockPos({
        x: Math.round((scene.clientWidth - dw) * fx),
        y: Math.round((scene.clientHeight - dh) * fy),
      });
    });
    return () => cancelAnimationFrame(id);
  }, []);
  const persistDock = (x: number, y: number) => {
    const scene = sceneRef.current;
    const el = dockRnd.current?.getSelfElement();
    if (!scene || !el) return;
    const spanW = scene.clientWidth - el.offsetWidth;
    const spanH = scene.clientHeight - el.offsetHeight;
    try {
      localStorage.setItem(
        OFFICE_CONFIG.storage.dock,
        JSON.stringify({ fx: spanW > 0 ? x / spanW : 0.5, fy: spanH > 0 ? y / spanH : 0.97 }),
      );
    } catch {
      /* no persistence */
    }
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
        {/* ── wall: warm wallpaper + faint stripe + wainscot + skirting ── */}
        <div
          className="absolute inset-x-0 top-0 h-[30%]"
          style={{ background: "linear-gradient(180deg,#ddc9a2 0%,#d2bc90 100%)" }}
        />
        <div
          className="absolute inset-x-0 top-0 h-[30%]"
          style={{
            backgroundImage:
              "repeating-linear-gradient(90deg,rgba(120,92,50,0.09) 0 2px,transparent 2px 18px)",
          }}
        />
        {/* wainscot paneling band along the foot of the wall */}
        <div
          className="absolute inset-x-0"
          style={{
            top: "22%",
            height: "8%",
            background: "linear-gradient(180deg,#cba978 0%,#bb9560 100%)",
            backgroundImage:
              "repeating-linear-gradient(90deg,rgba(80,52,20,0.22) 0 2px,transparent 2px 34px)",
            boxShadow: "inset 0 2px 0 rgba(255,244,214,0.55)",
          }}
        />
        {/* skirting board (wall↔floor join) */}
        <div
          className="absolute inset-x-0"
          style={{ top: "30%", height: "6px", transform: "translateY(-6px)", background: "#8a6534" }}
        />
        {/* ── floor: warm wood planks (staggered seams + grain) ── */}
        <div
          className="absolute inset-x-0 bottom-0 top-[30%]"
          style={{ background: "linear-gradient(180deg,#cda869 0%,#c0985400 100%), #c8a261" }}
        />
        <div
          className="absolute inset-x-0 bottom-0 top-[30%]"
          style={{
            backgroundImage:
              "repeating-linear-gradient(0deg,rgba(74,48,18,0.20) 0 2px,transparent 2px 32px)," +
              "repeating-linear-gradient(0deg,transparent 0 30px,rgba(255,242,205,0.10) 30px 32px)," +
              "repeating-linear-gradient(90deg,rgba(74,48,18,0.10) 0 2px,transparent 2px 130px)",
          }}
        />
        {/* cozy area rug under the meeting table */}
        <div
          className="absolute"
          style={{
            left: `${MEETING_TABLE.x}%`,
            top: `${MEETING_TABLE.y + 5}%`,
            transform: "translate(calc(-50% + 118px),-55%)",
            width: "300px",
            height: "146px",
            borderRadius: "8px",
            background: "radial-gradient(circle at 50% 50%,#b5503f 0%,#a3402f 62%,#8c3526 100%)",
            border: "4px solid #d98a5a",
            boxShadow: "inset 0 0 0 8px rgba(217,138,90,0.35)",
            opacity: 0.9,
          }}
        />
        {/* ── ambient lighting: golden wash + window beam + vignette ── */}
        <div
          className="pointer-events-none absolute inset-0"
          style={{
            background:
              "radial-gradient(120% 80% at 22% 0%,rgba(255,224,150,0.30) 0%,transparent 55%)",
            mixBlendMode: "soft-light",
          }}
        />
        {/* sunbeam shaft from the top-left window */}
        <div
          className="pointer-events-none absolute"
          style={{
            left: "6%",
            top: "12%",
            width: "215px",
            height: "470px",
            background:
              "linear-gradient(166deg,rgba(255,238,176,0.46) 0%,rgba(255,236,170,0.12) 44%,transparent 66%)",
            transform: "skewX(-20deg) rotate(4deg)",
            transformOrigin: "top left",
            opacity: light === "day" ? 1 : light === "dusk" ? 0.5 : 0,
            filter: "blur(3px)",
          }}
        />
        {/* warm pool where the beam lands on the floor */}
        <div
          className="pointer-events-none absolute"
          style={{
            left: "13%",
            top: "44%",
            width: "240px",
            height: "120px",
            background:
              "radial-gradient(ellipse at 50% 50%,rgba(255,234,165,0.34) 0%,transparent 70%)",
            transform: "skewX(-22deg)",
            opacity: light === "day" ? 1 : light === "dusk" ? 0.45 : 0,
            filter: "blur(4px)",
          }}
        />
        <div
          className="pointer-events-none absolute inset-0"
          style={{ boxShadow: "inset 0 0 120px 28px rgba(40,26,10,0.30)" }}
        />
        {/* time-of-day tint (below the desk z-layer, so sprites stay readable) */}
        {light !== "day" && (
          <div
            className="pointer-events-none absolute inset-0"
            style={
              light === "dusk"
                ? {
                    background:
                      "linear-gradient(180deg,rgba(255,150,60,0.10),rgba(255,116,48,0.20))",
                    mixBlendMode: "soft-light",
                  }
                : {
                    background:
                      "linear-gradient(180deg,rgba(34,44,98,0.34),rgba(12,16,48,0.52))",
                    mixBlendMode: "multiply",
                  }
            }
          />
        )}
        <Decor light={light} onCoffee={brewCoffee} />

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
              {meetingKind === "review" ? "评审会 · 复盘改稿" : "例会中 · 对齐分工"}
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
                  {disabledSet.has(def.key)
                    ? "已停用"
                    : v?.status === "working"
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

        {/* the office cat — wanders, naps by the lamp, pettable */}
        <OfficeCat />

        {/* coffee break: a ☕ floats over every head */}
        {coffee &&
          defs.map((def) => {
            const s = sim[def.key];
            if (!s) return null;
            return (
              <span
                key={`cof-${def.key}`}
                className="claw-coffee-bub pointer-events-none absolute z-[60] text-[15px]"
                style={{ left: `${s.x}%`, top: `${s.y - 6}%` }}
              >
                ☕
              </span>
            );
          })}

        {/* HUD: title, ticker, phase strip */}
        <div className="absolute left-3 top-2 z-40 flex items-center gap-2">
          <span className="font-pixel text-[0.55rem] tracking-wide text-accent">✦ CLAW OFFICE</span>
          <span className="inline-flex items-center gap-1 font-mono text-[10px] text-muted">
            <i className="h-[6px] w-[6px] animate-pixpulse rounded-full bg-grass" />
            LIVE
          </span>
          <button
            onClick={() => setLight((l) => (l === "day" ? "dusk" : l === "dusk" ? "night" : "day"))}
            title="切换昼夜光照"
            className="inline-flex items-center gap-1 rounded-[4px] border border-ink/30 bg-surface/90 px-1.5 py-[1px] font-mono text-[10px] text-ink-2 transition-colors hover:text-ink"
          >
            {light === "day" ? "☀ 白天" : light === "dusk" ? "🌇 黄昏" : "🌙 夜晚"}
          </button>
        </div>
        {(coffee || offDuty || meeting || lastActivity) && (
          <div className="absolute bottom-2 left-3 z-40 max-w-[48%] truncate rounded-[4px] border border-ink/25 bg-surface/90 px-2 py-[3px] font-mono text-[10px] text-ink-2">
            {coffee
              ? "☕ 咖啡时间!全员回血中 — 谢谢老板"
              : meeting
                ? meetingKind === "review"
                  ? "▶ 评审会 — 评审员复盘改稿,撰稿员修订中"
                  : "▶ 开工例会 — 调度员对齐分工中"
                : offDuty
                  ? "✓ 收工!全员下班 — 作品包在 dock"
                  : lastActivity}
          </div>
        )}
        <PhaseStrip views={views} finished={run.status === "finished"} />
      </div>

      {/* ── stats / wardrobe / bindings popover (Radix, anchored to the
            clicked worker; Escape / outside-click dismiss for free) ──── */}
      <Popover.Root open={!!focusedDef} onOpenChange={(o) => { if (!o) setFocus(null); }}>
        {focus && sim[focus] && (
          <Popover.Anchor asChild>
            <div
              className="pointer-events-none absolute"
              style={{ left: `${sim[focus].x}%`, top: `${sim[focus].y}%`, width: 1, height: 1 }}
            />
          </Popover.Anchor>
        )}
        <Popover.Portal>
          <Popover.Content
            side="left"
            align="start"
            sideOffset={30}
            collisionPadding={12}
            onOpenAutoFocus={(e) => e.preventDefault()}
            className="claw-popover z-[120] w-[228px] rounded-pixel border-2 border-ink bg-surface p-3 shadow-pixel"
          >
            {focusedDef && (
            <>
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
            </>
            )}
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>

      {/* ── dock (free-drag via react-rnd, position persisted) ──────── */}
      <Rnd
        ref={dockRnd}
        bounds="parent"
        enableResizing={false}
        dragHandleClassName="dock-drag"
        size={{ width: "auto", height: "auto" }}
        position={dockPos ?? { x: 0, y: 0 }}
        onDragStop={(_e, d) => {
          setDockPos({ x: d.x, y: d.y });
          persistDock(d.x, d.y);
        }}
        style={{ zIndex: 40, opacity: dockPos ? 1 : 0 }}
        className="flex items-center gap-1.5 rounded-pixel border-2 border-ink bg-surface/95 px-1.5 py-1.5 shadow-pixel-sm"
      >
        <span
          className="dock-drag grid h-6 w-4 cursor-grab touch-none place-items-center text-line-2 hover:text-ink-2 active:cursor-grabbing"
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
          icon={<Film size={13} strokeWidth={2} />}
          label={`视频${(run.videos?.length ?? 0) > 0 ? ` ${run.videos!.length}` : ""}`}
          disabled={(run.videos?.length ?? 0) === 0}
          active={winOpen && tab === "video"}
          onClick={() => openTab("video")}
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
          onClick={() => {
            setMeetingKind("kickoff");
            setMeetingUntil(Date.now() + OFFICE_CONFIG.meetingMs);
          }}
        />
        <DockButton
          icon={<Plug size={13} strokeWidth={2} />}
          label="绑定"
          active={bindOpen}
          onClick={() => setBindOpen((o) => !o)}
        />
      </Rnd>

      {/* ── 真·动态改绑 window ───────────────────────────────────────── */}
      {bindOpen && (
        <PixelWindow
          title="团队绑定 · roles ⇄ tools"
          z={110}
          onClose={() => setBindOpen(false)}
          onFocus={() => {}}
          initial={{ left: 0.18, top: 0.1, width: 0.64, height: 0.78 }}
        >
          <BindingsPanel onApplied={setRolesCfg} />
        </PixelWindow>
      )}

      {/* ── the work-package window ────────────────────────────────── */}
      {winOpen && (
        <PixelWindow title={`作品包 · ~/claw/${run.job_id.slice(0, 6)}`} z={100} onClose={() => setWinOpen(false)} onFocus={() => {}}>
          <ArtifactBody
            jobId={run.job_id}
            initialVersion={run.artifact_version ?? 0}
            figures={run.figures ?? []}
            videos={run.videos ?? []}
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
function Decor({
  light = "day",
  onCoffee,
}: {
  light?: "day" | "dusk" | "night";
  onCoffee?: () => void;
}) {
  // lamp/sconce glow grows as the room darkens
  const glow = light === "night" ? 0.6 : light === "dusk" ? 0.32 : 0.12;
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
      {/* coffee machine (lounge) — click to treat the whole team ☕ */}
      <div
        className="absolute cursor-pointer transition-transform hover:scale-105"
        style={{ left: "1%", top: "92%", transform: "translateY(-100%)" }}
        onClick={onCoffee}
        title="请大家喝咖啡 ☕"
      >
        <span className="claw-steam absolute left-[16px] top-[6px] h-[7px] w-[3px] rounded-full bg-white/70" />
        <span className="claw-steam absolute left-[23px] top-[9px] h-[6px] w-[3px] rounded-full bg-white/60" style={{ animationDelay: "0.8s" }} />
        <span className="claw-steam absolute left-[29px] top-[7px] h-[5px] w-[2px] rounded-full bg-white/50" style={{ animationDelay: "1.5s" }} />
        <svg className="claw-sprite" width="52" height="66" viewBox="0 0 23 29" shapeRendering="crispEdges">
          <rect x="2" y="4" width="14" height="20" fill="#403a30" />
          <rect x="3" y="5" width="12" height="7" fill="#16140f" />
          <rect x="4" y="6" width="4" height="2" fill="#74e6a0" />
          <rect x="6" y="14" width="6" height="4" fill="#16140f" />
          <rect x="7" y="18" width="4" height="1" fill="#e3a13a" />
          <rect x="0" y="24" width="20" height="2" fill="#84591c" />
          <rect x="1" y="26" width="2" height="3" fill="#84591c" />
          <rect x="17" y="26" width="2" height="3" fill="#84591c" />
        </svg>
      </div>
      {/* plant */}
      <svg className="absolute claw-sprite" style={{ right: "1.5%", top: "74%", transform: "translateY(-100%)" }} width="46" height="64" viewBox="0 0 20 28" shapeRendering="crispEdges">
        <rect x="7" y="2" width="6" height="6" fill="#3ea96a" />
        <rect x="4" y="6" width="6" height="6" fill="#4fbf7c" />
        <rect x="10" y="7" width="6" height="6" fill="#2f8f58" />
        <rect x="8" y="13" width="4" height="5" fill="#6a4a23" />
        <rect x="5" y="18" width="10" height="8" fill="#b5371e" />
        <rect x="5" y="18" width="10" height="2" fill="#d8654a" />
      </svg>
      {/* framed pictures on the bare wall */}
      <svg className="absolute left-[27%] top-[8%] claw-sprite" width="42" height="34" viewBox="0 0 19 15" shapeRendering="crispEdges">
        <rect x="0" y="0" width="19" height="15" fill="#7a5a2e" />
        <rect x="1.5" y="1.5" width="16" height="12" fill="#fbfaf2" />
        <rect x="3" y="3" width="6" height="5" fill="#8fb8e0" />
        <rect x="11" y="3" width="3" height="3" fill="#e3b23a" />
        <rect x="3" y="10" width="13" height="2.5" fill="#4fa36a" />
      </svg>
      <svg className="absolute left-[72%] top-[7%] claw-sprite" width="40" height="40" viewBox="0 0 18 18" shapeRendering="crispEdges">
        <rect x="0" y="0" width="18" height="18" fill="#7a5a2e" />
        <rect x="1.5" y="1.5" width="15" height="15" fill="#fbfaf2" />
        <rect x="3" y="4" width="12" height="2" fill="#b5503f" />
        <rect x="3" y="8" width="9" height="2" fill="#c98a3a" />
        <rect x="3" y="12" width="11" height="2" fill="#6a8fcf" />
      </svg>
      {/* wall sconces with a glow that grows after dark */}
      {["21%", "83%"].map((lx, i) => (
        <div key={`sconce-${i}`} className="absolute" style={{ left: lx, top: "16%" }}>
          <div
            className="pointer-events-none absolute rounded-full"
            style={{
              left: "-30px",
              top: "-26px",
              width: "78px",
              height: "78px",
              background: `radial-gradient(circle,rgba(255,206,120,${glow}) 0%,transparent 70%)`,
            }}
          />
          <svg className="claw-sprite relative" width="20" height="26" viewBox="0 0 9 12" shapeRendering="crispEdges">
            <rect x="3" y="6" width="3" height="5" fill="#4a3417" />
            <rect x="1" y="2" width="7" height="5" fill="#ffd874" />
            <rect x="2" y="1" width="5" height="2" fill="#ffe7a6" />
          </svg>
        </div>
      ))}
      {/* floor lamp (left-mid) with a warm glow */}
      <div className="absolute" style={{ left: "2.5%", top: "52%" }}>
        <div
          className="pointer-events-none absolute rounded-full"
          style={{
            left: "-26px",
            top: "-30px",
            width: "96px",
            height: "96px",
            background: `radial-gradient(circle,rgba(255,214,130,${glow + 0.12}) 0%,transparent 70%)`,
          }}
        />
        <svg className="claw-sprite relative" width="30" height="78" viewBox="0 0 13 34" shapeRendering="crispEdges">
          <rect x="2" y="1" width="9" height="2" fill="#4a3417" />
          <rect x="3" y="2" width="7" height="5" fill="#ffd874" />
          <rect x="2" y="2" width="9" height="2" fill="#ffe7a6" />
          <rect x="6" y="7" width="1" height="23" fill="#6a4a23" />
          <rect x="3" y="30" width="7" height="2" fill="#4a3417" />
        </svg>
      </div>
      {/* small potted plant near the lounge */}
      <svg className="absolute claw-sprite" style={{ left: "9%", top: "94%", transform: "translateY(-100%)" }} width="40" height="54" viewBox="0 0 18 24" shapeRendering="crispEdges">
        <rect x="6" y="2" width="5" height="6" fill="#4fbf7c" />
        <rect x="3" y="6" width="6" height="6" fill="#3ea96a" />
        <rect x="9" y="7" width="6" height="6" fill="#2f8f58" />
        <rect x="6" y="13" width="5" height="4" fill="#6a4a23" />
        <rect x="4" y="17" width="9" height="6" fill="#c98a3a" />
        <rect x="4" y="17" width="9" height="2" fill="#e0a85a" />
      </svg>
      {/* string lights swooping along the top of the wall */}
      <div
        className="pointer-events-none absolute inset-x-[3%] top-[1.2%] h-[10px]"
        style={{ borderTop: "2px solid rgba(74,52,23,0.45)", borderRadius: "0 0 50% 50%/0 0 100% 100%" }}
      >
        <div className="flex h-full items-end justify-between px-2">
          {Array.from({ length: 16 }).map((_, i) => {
            const c = ["#ffd874", "#8fd6a0", "#f0a3a3", "#9ec3f0"][i % 4];
            return (
              <span
                key={i}
                className={light !== "day" ? "claw-twinkle" : undefined}
                style={{
                  display: "block",
                  width: "7px",
                  height: "7px",
                  borderRadius: "9999px",
                  background: c,
                  boxShadow: light !== "day" ? `0 0 9px 2.5px ${c}88` : "none",
                  animationDelay: `${(i % 5) * 0.4}s`,
                }}
              />
            );
          })}
        </div>
      </div>
      {/* bookshelf against the wall (right side) */}
      <svg className="absolute claw-sprite" style={{ right: "11%", top: "30.5%", transform: "translateY(-100%)" }} width="84" height="100" viewBox="0 0 36 43" shapeRendering="crispEdges">
        <rect x="0" y="0" width="36" height="43" fill="#6a4a23" />
        <rect x="2" y="2" width="32" height="11" fill="#4a3417" />
        <rect x="2" y="15" width="32" height="11" fill="#4a3417" />
        <rect x="2" y="28" width="32" height="11" fill="#4a3417" />
        {/* spines, shelf 1 */}
        <rect x="4" y="4" width="3" height="9" fill="#b5503f" />
        <rect x="8" y="5" width="3" height="8" fill="#3e7fa3" />
        <rect x="12" y="4" width="4" height="9" fill="#e3b23a" />
        <rect x="17" y="6" width="3" height="7" fill="#4fa36a" />
        <rect x="21" y="4" width="3" height="9" fill="#8a5fc9" />
        <rect x="25" y="5" width="4" height="8" fill="#c97a3a" />
        {/* spines, shelf 2 */}
        <rect x="4" y="18" width="4" height="8" fill="#4fa36a" />
        <rect x="9" y="17" width="3" height="9" fill="#e3b23a" />
        <rect x="13" y="19" width="3" height="7" fill="#b5503f" />
        <rect x="17" y="17" width="4" height="9" fill="#3e7fa3" />
        <rect x="22" y="18" width="3" height="8" fill="#c97a3a" />
        <rect x="26" y="17" width="3" height="9" fill="#8a5fc9" />
        {/* shelf 3: books + a tiny plant */}
        <rect x="4" y="31" width="3" height="8" fill="#3e7fa3" />
        <rect x="8" y="30" width="4" height="9" fill="#b5503f" />
        <rect x="13" y="32" width="3" height="7" fill="#e3b23a" />
        <rect x="24" y="33" width="6" height="3" fill="#4fbf7c" />
        <rect x="25" y="36" width="4" height="3" fill="#c98a3a" />
      </svg>
    </>
  );
}

// ── the office cat ──────────────────────────────────────────────────────
// Wanders the floor on a slow CSS glide, occasionally curls up for a nap by
// the floor lamp, and pops hearts when petted. Purely cosmetic — runs on its
// own timer, independent of the work sim.
const CAT_NAP = { x: 7, y: 60 }; // beside the floor lamp
function OfficeCat() {
  const [pos, setPos] = useState({ x: 70, y: 72 });
  const [mode, setMode] = useState<"wander" | "sleep">("wander");
  const [dir, setDir] = useState(1); // 1 → facing right
  const [hearts, setHearts] = useState<number[]>([]);
  const heartSeq = useRef(0);

  useEffect(() => {
    const id = setInterval(() => {
      setMode((m) => {
        if (m === "sleep") {
          // 30% chance to wake each tick
          if (Math.random() < 0.3) return "wander";
          return m;
        }
        // 22% chance to head off for a nap
        if (Math.random() < 0.22) {
          setPos((p) => {
            setDir(CAT_NAP.x >= p.x ? 1 : -1);
            return CAT_NAP;
          });
          return "sleep";
        }
        const next = { x: 8 + Math.random() * 80, y: 38 + Math.random() * 52 };
        setPos((p) => {
          setDir(next.x >= p.x ? 1 : -1);
          return next;
        });
        return m;
      });
    }, 5200);
    return () => clearInterval(id);
  }, []);

  const pet = () => {
    const id = ++heartSeq.current;
    setHearts((hs) => [...hs, id]);
    setTimeout(() => setHearts((hs) => hs.filter((h) => h !== id)), 1100);
    if (mode === "sleep") setMode("wander");
  };

  return (
    <div
      className="absolute cursor-pointer"
      style={{
        left: `${pos.x}%`,
        top: `${pos.y}%`,
        zIndex: Math.round(pos.y),
        transition: "left 4.6s linear, top 4.6s linear",
      }}
      onClick={pet}
      title="撸猫"
    >
      {hearts.map((h) => (
        <span key={h} className="claw-heart pointer-events-none absolute -top-4 left-1/2 text-[12px]">
          ❤️
        </span>
      ))}
      {mode === "sleep" && (
        <span className="claw-zz pointer-events-none absolute -top-3 left-[70%] font-mono text-[10px] font-bold text-ink-2">
          z
        </span>
      )}
      {mode === "wander" ? (
        <svg width="40" height="30" viewBox="0 0 20 15" className="claw-sprite" shapeRendering="crispEdges" style={{ transform: `scaleX(${dir})` }}>
          {/* tail (flicks) */}
          <g className="claw-cat-tail">
            <rect x="0" y="5" width="2" height="5" fill="#d98a3a" />
          </g>
          {/* body */}
          <rect x="2" y="7" width="10" height="5" fill="#e8a04a" />
          <rect x="3" y="8" width="3" height="2" fill="#d98a3a" />
          <rect x="8" y="8" width="2" height="2" fill="#d98a3a" />
          {/* legs */}
          <rect x="3" y="12" width="2" height="2" fill="#d98a3a" />
          <rect x="9" y="12" width="2" height="2" fill="#d98a3a" />
          {/* head + ears */}
          <rect x="11" y="3" width="6" height="6" fill="#e8a04a" />
          <rect x="11" y="1" width="2" height="2" fill="#e8a04a" />
          <rect x="15" y="1" width="2" height="2" fill="#e8a04a" />
          <rect x="12" y="2" width="1" height="1" fill="#f0bfa0" />
          <rect x="15" y="2" width="1" height="1" fill="#f0bfa0" />
          {/* eyes + nose */}
          <rect x="13" y="5" width="1" height="1" fill="#16140f" />
          <rect x="16" y="5" width="1" height="1" fill="#16140f" />
          <rect x="14.5" y="7" width="1" height="1" fill="#b5503f" />
          {/* chest patch */}
          <rect x="11" y="9" width="2" height="3" fill="#fbf3e4" />
        </svg>
      ) : (
        <svg width="36" height="24" viewBox="0 0 18 12" className="claw-sprite" shapeRendering="crispEdges">
          {/* curled-up loaf */}
          <rect x="2" y="4" width="12" height="6" fill="#e8a04a" />
          <rect x="3" y="3" width="10" height="2" fill="#e8a04a" />
          <rect x="4" y="5" width="3" height="2" fill="#d98a3a" />
          <rect x="9" y="6" width="3" height="2" fill="#d98a3a" />
          {/* tail wrapped around */}
          <rect x="1" y="8" width="13" height="2" fill="#d98a3a" />
          {/* ears poking up */}
          <rect x="11" y="1" width="2" height="2" fill="#e8a04a" />
          <rect x="14" y="2" width="2" height="2" fill="#e8a04a" />
          {/* closed eyes */}
          <rect x="11" y="5" width="2" height="1" fill="#16140f" />
        </svg>
      )}
    </div>
  );
}
