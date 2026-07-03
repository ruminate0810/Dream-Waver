"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Rnd } from "react-rnd";
import * as Popover from "@radix-ui/react-popover";
import { FileText, Image as ImageIcon, Film, Layers, Users, X, GripVertical, Shuffle, ChevronLeft, ChevronRight, Plug, Check } from "lucide-react";
import clsx from "clsx";

import { getClawRoles, type ClawRun, type ClawRolesConfig, type ClawTask } from "@/lib/api";
import { TaskPlanCard } from "./TaskPlanCard";
import { narrationFor } from "./narrate";
import { directivesFor } from "./sceneGrammar";
import { fmtDuration } from "@/components/chat/ToolProgress";
import { useAgentEventStream, type AgentEvent } from "@/components/chat/transport";
import { ACT, EMO, TOOL_ACTION, WORKERS, type WorkerDef, type WorkerPhase } from "./workers";
import { ChibiWorker } from "./ChibiWorker";
import { PixelWindow } from "./PixelWindow";
import { ArtifactBody, type WorkTab } from "./ArtifactPanel";
import { useWorkerStates, type WorkerView } from "./useWorkerStates";
import { useOfficeSim, OFFICE_CONFIG, STATIONS, MEETING_TABLE, type SimView } from "./officeSim";
import { SPRITES, DESK_VARIANT, STATION_PROP, ZONES_V12, LOUNGE_V12, WAYPOINTS_V12, MEETING_V12 } from "./officeArt";
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

type Flight = {
  id: number;
  from: { x: number; y: number };
  to: { x: number; y: number };
  fromKey: string;
  toKey: string;
  delay: number;
};
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

export function ClawOffice({
  run,
  onAsk,
}: {
  run: ClawRun;
  /** Dispatch a follow-up message through the chat (拖图派活/空间指派). */
  onAsk?: (text: string) => void;
}) {
  const stream = useAgentEventStream();
  const { views, lastActivity } = useWorkerStates(run.plan ?? []);
  const { defs: wardrobeDefs, outfitOf, setOutfit } = useWardrobe();
  const [focus, setFocus] = useState<string | null>(null);
  const [light, setLight] = useState<"day" | "dusk" | "night">("day");

  // ── 空间可操作化 — drag a figure/report from the work package onto a
  // worker to hand them the job (drops become real follow-up turns via
  // onAsk). dragKind tracks the payload type while a drag is in flight so
  // valid recipients glow; native DnD events bubble up to window.
  const [dragKind, setDragKind] = useState<"figure" | "report" | null>(null);
  useEffect(() => {
    const onStart = (e: DragEvent) => {
      const t = e.dataTransfer?.types ?? [];
      if (t.includes("application/x-claw-figure")) setDragKind("figure");
      else if (t.includes("application/x-claw-report")) setDragKind("report");
    };
    const onEnd = () => setDragKind(null);
    window.addEventListener("dragstart", onStart);
    window.addEventListener("dragend", onEnd);
    window.addEventListener("drop", onEnd);
    return () => {
      window.removeEventListener("dragstart", onStart);
      window.removeEventListener("dragend", onEnd);
      window.removeEventListener("drop", onEnd);
    };
  }, []);
  // the designer offers two edits — a drop opens a tiny pick menu at the desk
  const [dropMenu, setDropMenu] = useState<{ index: number; x: number; y: number } | null>(null);
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
  // 桌面气泡 — agents speak ABOVE their heads in the office (not just in chat):
  // narrationFor turns events into first-person lines; we show the latest per
  // worker for ~5.5s. Sim re-renders (~90ms) expire them, no extra timer.
  const [speech, setSpeech] = useState<Record<string, { text: string; until: number }>>({});
  const officeSaidRef = useRef<Set<string>>(new Set());
  // v21 — scene staging is interpreted from SCENE_GRAMMAR (the semantic-
  // binding table as data). Once the stream shows a first-class claw.phase
  // event, meetings key off REAL stage boundaries; the old inference rules
  // (plan arrival / critic tool activity) only serve pre-v21 streams.
  const phaseAwareRef = useRef(false);
  useEffect(
    () =>
      stream.subscribe((ev: AgentEvent) => {
        const line = narrationFor(ev, officeSaidRef.current);
        if (line && line.worker) {
          setSpeech((prev) => ({ ...prev, [line.worker]: { text: line.text, until: Date.now() + 5500 } }));
        }
        if (ev.kind === "claw.phase") phaseAwareRef.current = true;
        for (const d of directivesFor(ev, phaseAwareRef.current)) {
          if (d.type === "meeting") {
            const now = Date.now();
            if (d.op === "close") {
              // grace window so walk-outs read as walking, not teleporting
              setMeetingUntil((prev) => Math.min(prev, now + OFFICE_CONFIG.meetingGraceMs));
            } else {
              setMeetingKind(d.kind);
              setMeetingUntil(now + OFFICE_CONFIG.meetingMs);
            }
          } else if (d.type === "debate") {
            try {
              const dd = JSON.parse(ev.data?.claw_debate_json ?? "{}");
              const roles: string[] = (dd.proposals ?? []).map((p: { role: string }) => p.role).filter(Boolean);
              if (roles.length >= 2) setDebate({ roles, until: Date.now() + 10_000 });
            } catch {
              /* malformed debate payload — skip the staging */
            }
          }
          // "celebrate" is staged from the run-status flip below (the bell
          // effect) — declared in the grammar, executed by that effect.
        }
      }),
    [stream],
  );
  const meeting = meetingUntil > Date.now();
  const meetingChair = meetingKind === "review" ? "critic" : WORKERS[0].key;

  // the sim drives every body in the room
  // gesture pulses — short UI-driven gesture overrides (交接击掌 etc.) the
  // sim reads each tick; cheap one-way channel, no re-renders.
  const pulses = useRef<Record<string, { g: string; until: number }>>({});
  // forced visits (锤人): bonker walks to the offender's desk for the window
  const visits = useRef<Record<string, { host: string; until: number }>>({});
  // live cat position, written by OfficeCat — lets the room react to the cat
  const catPos = useRef<{ x: number; y: number; sleeping: boolean } | null>(null);
  const sim = useOfficeSim({ views, offDuty, meetingUntil, meetingChair, pulses, visits });

  // which payload each worker accepts (only on finished runs — drops become
  // follow-up turns, and the chips-era gate applies: figures/report must exist)
  const dropSpec = useCallback(
    (key: string): "figure" | "report" | null => {
      if (run.status !== "finished" || !onAsk || disabledSet.has(key)) return null;
      if ((key === "designer" || key === "videographer") && (run.figures?.length ?? 0) > 0) return "figure";
      if (key === "producer" && (run.artifact_version ?? 0) > 0) return "report";
      return null;
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [run.status, run.figures?.length, run.artifact_version, onAsk, rolesCfg],
  );

  // a drop = the worker acknowledges in place + the ask goes out as a turn
  const spatialAsk = useCallback(
    (worker: string, ack: string, text: string) => {
      setSpeech((prev) => ({ ...prev, [worker]: { text: ack, until: Date.now() + 6000 } }));
      pulses.current[worker] = { g: "cheer", until: Date.now() + 1600 };
      onAsk?.(text);
    },
    [onAsk],
  );

  const onDropPayload = useCallback(
    (key: string, kind: "figure" | "report", index: number) => {
      const n = index + 1;
      if (key === "videographer" && kind === "figure") {
        spatialAsk(
          key,
          `上三脚架!第 ${n} 张图这就开拍。`,
          `让视频师把第 ${n} 张配图做成一段 720p、5 秒的短视频,加点运镜和光影动态。`,
        );
      } else if (key === "producer" && kind === "report") {
        spatialAsk(key, "报告收到,今晚必须出片!", "把这份报告做成一份幻灯片 deck。");
      } else if (key === "designer" && kind === "figure") {
        const s = sim[key];
        setDropMenu({ index, x: s?.x ?? 50, y: s?.y ?? 50 });
      }
    },
    [sim, spatialAsk],
  );

  // 锤人 — when a worker's tool errors (facepalm reaction), the 评审员 (or
  // the 调度员, if the critic is the offender) marches over and bonks them
  // with a comedy hammer. Cooldown per offender so a flaky tool doesn't turn
  // the office into a boxing ring.
  const [bonk, setBonk] = useState<{ host: string; bonker: string; until: number } | null>(null);
  const bonkCd = useRef<Record<string, number>>({});
  const triggerBonk = useCallback((host: string) => {
    const now = Date.now();
    const bonker = host === "critic" ? "coordinator" : "critic";
    // long enough to walk across the whole office (lounge → far desk ≈ 11s)
    const until = now + 14_000;
    visits.current[bonker] = { host, until };
    pulses.current[bonker] = { g: "point", until };
    setBonk({ host, bonker, until });
    setTimeout(() => setBonk((b) => (b && b.until === until ? null : b)), 14_200);
  }, []);
  useEffect(() => {
    if (offDuty || meeting) return;
    const now = Date.now();
    for (const w of WORKERS) {
      const v = views[w.key];
      if (!v?.reactionUntil || v.reactionUntil < now || v.reactionGesture !== "facepalm") continue;
      if ((bonkCd.current[w.key] ?? 0) > now) continue;
      bonkCd.current[w.key] = now + 30_000;
      triggerBonk(w.key);
      break;
    }
  }, [views, offDuty, meeting, triggerBonk]);

  // 辩论 — v6.4's real kickoff debate (claw.debate) gets staged visually:
  // the participants trade 💢/💬 around the meeting table while it lasts.
  const [debate, setDebate] = useState<{ roles: string[]; until: number } | null>(null);

  // 敲钟 — 纳斯达克 style: the moment the run finishes, the office bell
  // rings (swing + confetti) and the whole team cheers before heading to
  // the lounge party.
  const [bellUntil, setBellUntil] = useState(0);
  const prevRunStatus = useRef(run.status);
  useEffect(() => {
    if (prevRunStatus.current !== "finished" && run.status === "finished") {
      const until = Date.now() + 7000;
      setBellUntil(until);
      WORKERS.forEach((w, i) => {
        pulses.current[w.key] = { g: "cheer", until: until - i * 200 };
      });
    }
    prevRunStatus.current = run.status;
  }, [run.status]);
  const bellRinging = bellUntil > Date.now();

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
        if (from) add.push({ id: flightSeq++, from, to, fromKey: src, toKey: w.key, delay: i * 220 });
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
      className="relative isolate h-full min-h-[420px] overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel"
    >
      {/* ── the room ───────────────────────────────────────────────── */}
      <div className="absolute inset-0">
        {/* ── wall: warm wallpaper + faint stripe + wainscot + skirting ── */}
        <div
          className="absolute inset-x-0 top-0 h-[30%]"
          style={{ background: "linear-gradient(180deg, #e3d2ad 0%, #dccaa0 46%, #d2bc90 46.001%, #c9b07f 100%)" }}
        />
        <div
          className="absolute inset-x-0 top-0 h-[30%]"
          style={{
            backgroundImage:
              "repeating-linear-gradient(90deg,rgba(120,92,50,0.09) 0 2px,transparent 2px 18px)",
          }}
        />
        {/* picture-rail line across the wall */}
        <div
          className="pointer-events-none absolute inset-x-0 top-0 h-[30%]"
          style={{
            backgroundImage:
              "linear-gradient(180deg, rgba(120,92,50,0.55) 0 1px, rgba(255,244,214,0.6) 1px 2px, rgba(120,92,50,0.30) 2px 3px, transparent 3px)",
            backgroundPosition: "0 18%",
            backgroundSize: "100% 100%",
            backgroundRepeat: "no-repeat",
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
        {/* skirting board (wall↔floor join) — beveled with highlight */}
        <div
          className="absolute inset-x-0"
          style={{
            top: "30%",
            height: "7px",
            transform: "translateY(-7px)",
            background: "linear-gradient(180deg, #a37b42 0%, #8a6534 40%, #6f4f28 100%)",
            boxShadow: "inset 0 1px 0 rgba(255,244,214,0.7), 0 1px 1px rgba(40,26,10,0.35)",
          }}
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
        {/* plank-to-plank color variation — old-wood-floor look */}
        <div
          className="pointer-events-none absolute inset-x-0 bottom-0 top-[30%]"
          style={{
            backgroundImage:
              "repeating-linear-gradient(90deg, rgba(74,48,18,0.07) 0 130px, rgba(255,242,205,0.05) 130px 260px, transparent 260px 390px, rgba(58,38,14,0.06) 390px 520px)",
          }}
        />
        {/* sparse wood knots */}
        <div
          className="pointer-events-none absolute inset-x-0 bottom-0 top-[30%]"
          style={{
            backgroundImage:
              "radial-gradient(circle at 18% 22%, rgba(74,48,18,0.34) 0 1.4px, transparent 2.2px)," +
              "radial-gradient(circle at 62% 58%, rgba(74,48,18,0.30) 0 1.6px, transparent 2.4px)," +
              "radial-gradient(circle at 84% 34%, rgba(74,48,18,0.26) 0 1.3px, transparent 2.1px)",
            backgroundSize: "230px 190px, 310px 260px, 270px 220px",
          }}
        />
        {/* zone rugs — soft area rugs that delineate the functional pods
            (执行工坊 / 稿件区 / 后期棚 / 茶水休息区), the去-AI-味 anchors */}
        {ZONES_V12.map((z) => (
          <div
            key={z.name}
            className="pointer-events-none absolute"
            style={{
              left: `${z.rugX}%`,
              top: `${z.rugY}%`,
              width: `${z.rugW}%`,
              height: `${z.rugH}%`,
              borderRadius: "10px",
              background: z.rugColor,
              opacity: 0.16,
              boxShadow: `inset 0 0 0 3px ${z.rugColor}, inset 0 0 0 6px rgba(255,244,214,0.12)`,
            }}
          />
        ))}
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
          <Sprite name="meeting-table" style={{ transform: "translateX(-50%)" }} />
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
                {(() => {
                  const dv = DESK_VARIANT[def.key] ?? "desk-a";
                  const dsp = SPRITES[dv];
                  return (
                    <div className="relative" style={{ width: dsp.w, height: dsp.h }}>
                      <Sprite name={dv} style={{ position: "absolute", inset: 0 }} />
                      {/* monitor (deskTool) overlaid on the same 32×28 grid */}
                      <svg
                        className="claw-sprite absolute inset-0"
                        width={dsp.w}
                        height={dsp.h}
                        viewBox="0 0 32 28"
                        shapeRendering="crispEdges"
                      >
                        <g dangerouslySetInnerHTML={{ __html: def.deskTool }} />
                      </svg>
                    </div>
                  );
                })()}
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
                        : // SA L3 — an idle-but-assigned worker's plate shows
                          // what they'll pick up next (first pending todo)
                          (() => {
                            const nx = todos?.find((t) => t.status === "pending");
                            return nx ? `▷ 接下来:${nx.title}` : "";
                          })()}
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
            say={speech[def.key] && speech[def.key].until > Date.now() ? speech[def.key].text : undefined}
            acceptKind={dropSpec(def.key)}
            dragKind={dragKind}
            onDropPayload={(kind, index) => onDropPayload(def.key, kind, index)}
            onClick={(shift) =>
              shift ? triggerBonk(def.key) : setFocus((f) => (f === def.key ? null : def.key))
            }
          />
        ))}

        {/* 拖图给设计师 — 精修方式二选一(高清化/抠图),点外面取消 */}
        {dropMenu && (
          <div
            className="claw-popover absolute z-[110] -translate-x-1/2 rounded-pixel border-2 border-ink bg-surface p-1.5 shadow-pixel"
            style={{ left: `${dropMenu.x}%`, top: `${Math.max(dropMenu.y - 16, 4)}%` }}
          >
            <p className="mb-1 px-1 font-mono text-[9.5px] font-bold text-muted">第 {dropMenu.index + 1} 张图 · 怎么修?</p>
            <div className="flex gap-1">
              <button
                type="button"
                className="rounded-pixel border-2 border-ink bg-paper px-2 py-1 font-mono text-[10px] text-ink shadow-pixel-sm hover:-translate-y-0.5"
                onClick={() => {
                  spatialAsk(
                    "designer",
                    "这张交给我,高清直出 ✦",
                    `让设计师把第 ${dropMenu.index + 1} 张配图高清化(enhance),让细节更清晰。`,
                  );
                  setDropMenu(null);
                }}
              >
                高清化
              </button>
              <button
                type="button"
                className="rounded-pixel border-2 border-ink bg-paper px-2 py-1 font-mono text-[10px] text-ink shadow-pixel-sm hover:-translate-y-0.5"
                onClick={() => {
                  spatialAsk(
                    "designer",
                    "抠图去背景,主体给你留干净。",
                    `让设计师把第 ${dropMenu.index + 1} 张配图抠图(remove_bg),留下透明背景的主体。`,
                  );
                  setDropMenu(null);
                }}
              >
                抠图去背景
              </button>
              <button
                type="button"
                aria-label="取消"
                className="grid w-6 place-items-center rounded-pixel border border-ink/40 font-mono text-[10px] text-ink-2 hover:text-ink"
                onClick={() => setDropMenu(null)}
              >
                ✕
              </button>
            </div>
          </div>
        )}

        {/* handoff flights — on landing both ends do a quick 击掌 cheer */}
        {flights.map((f) => (
          <FlyingDoc
            key={f.id}
            flight={f}
            onDone={(id) => {
              const until = Date.now() + 1300;
              pulses.current[f.fromKey] = { g: "cheer", until };
              pulses.current[f.toKey] = { g: "cheer", until };
              setFlights((fs) => fs.filter((x) => x.id !== id));
            }}
          />
        ))}

        {/* the office cat — wanders, naps by the lamp, pettable */}
        <OfficeCat posRef={catPos} />

        {/* 🎉 交付完成 — scene-wide confetti + a centered banner on finish */}
        {bellRinging && (
          <div className="pointer-events-none absolute inset-0 z-[90] overflow-hidden">
            {Array.from({ length: 36 }).map((_, i) => {
              const c = ["#ffd23e", "#6a55ff", "#3ea96a", "#f0a3a3", "#9ec3f0", "#e3892b"][i % 6];
              return (
                <span
                  key={i}
                  className="claw-confetti absolute top-[-6%]"
                  style={{
                    left: `${(i * 37 + 11) % 100}%`,
                    width: i % 3 === 0 ? "5px" : "7px",
                    height: i % 3 === 0 ? "9px" : "7px",
                    background: c,
                    animationDelay: `${(i % 9) * 0.18}s`,
                    animationDuration: `${2.4 + (i % 5) * 0.35}s`,
                  }}
                />
              );
            })}
            <div className="claw-celebrate absolute left-1/2 top-[34%] -translate-x-1/2">
              <div className="rounded-pixel border-2 border-ink bg-accent px-5 py-3 text-center shadow-pixel">
                <p className="font-pixel text-[0.85rem] tracking-wide text-white">✦ 交付完成!</p>
                <p className="mt-1 font-mono text-[11px] text-white/90">作品已放进作品包</p>
              </div>
            </div>
          </div>
        )}

        {/* 交付钟 — rings 纳斯达克-style when the run finishes (by the meeting table) */}
        <div className="absolute" style={{ left: `${MEETING_TABLE.x - 17}%`, top: `${MEETING_TABLE.y - 4}%`, zIndex: 77 }}>
          {bellRinging && (
            <>
              <span className="claw-coffee-bub pointer-events-none absolute -top-5 left-1/2 text-[14px]">🎉</span>
              <span className="claw-coffee-bub pointer-events-none absolute -top-4 left-0 text-[12px]" style={{ animationDelay: "0.5s" }}>
                ✨
              </span>
              <span className="claw-coffee-bub pointer-events-none absolute -top-4 left-[80%] text-[12px]" style={{ animationDelay: "1s" }}>
                🎊
              </span>
            </>
          )}
          <svg width="44" height="62" viewBox="0 0 20 28" className="claw-sprite" shapeRendering="crispEdges">
            {/* stand */}
            <rect x="9" y="2" width="2" height="4" fill="#4a3417" />
            <rect x="4" y="24" width="12" height="2" fill="#84591c" />
            <rect x="8" y="20" width="4" height="4" fill="#6a4a23" />
            {/* the bell (swings while ringing) */}
            <g className={bellRinging ? "claw-bell-ring" : undefined}>
              <rect x="8" y="6" width="4" height="2" fill="#e3b23a" />
              <rect x="6" y="8" width="8" height="6" fill="#ffd23e" />
              <rect x="5" y="14" width="10" height="2" fill="#e3a13a" />
              <rect x="9" y="16" width="2" height="2" fill="#84591c" />
              <rect x="7" y="9" width="2" height="3" fill="#ffe79a" />
            </g>
          </svg>
        </div>

        {/* 咖啡偶遇碰杯 — two workers at the coffee waypoint clink */}
        {(() => {
          const at = defs.filter((d) => {
            const s = sim[d.key];
            return s && !s.walking && s.site === "way:0";
          });
          if (at.length < 2) return null;
          const a = sim[at[0].key];
          const b = sim[at[1].key];
          return (
            <span
              className="claw-emote claw-emote-show absolute z-[60] text-[15px]"
              style={{ left: `${(a.x + b.x) / 2 + 1.5}%`, top: `${Math.min(a.y, b.y) - 8}%` }}
            >
              🥂
            </span>
          );
        })()}

        {/* 锤人 — the bonker swings the hammer once they stop walking;
              the offender only sees stars when actually within range */}
        {bonk &&
          bonk.until > Date.now() &&
          (() => {
            const bk = sim[bonk.bonker];
            const hs = sim[bonk.host];
            if (!bk || !hs) return null;
            const arrived = bk.site === `visit:${bonk.host}` && !bk.walking;
            if (!arrived) return null;
            const near = Math.abs(bk.x - hs.x) + Math.abs(bk.y - hs.y) < 14;
            if (!near) {
              // marched to their desk and they're not there — 扑了个空
              return (
                <span className="pointer-events-none absolute z-[70] text-[13px]" style={{ left: `${bk.x + 1}%`, top: `${bk.y - 10}%` }}>
                  💢❓
                </span>
              );
            }
            return (
              <>
                <span
                  className="claw-hammer pointer-events-none absolute z-[70] text-[18px]"
                  style={{ left: `${bk.x + (bk.facing === 1 ? 3 : -3)}%`, top: `${bk.y - 8}%` }}
                >
                  🔨
                </span>
                <span className="pointer-events-none absolute z-[70] text-[12px]" style={{ left: `${bk.x - 2}%`, top: `${bk.y - 10}%` }}>
                  💢
                </span>
                <span className="pointer-events-none absolute z-[70] text-[12px]" style={{ left: `${hs.x + 1}%`, top: `${hs.y - 8}%` }}>
                  💫
                </span>
              </>
            );
          })()}

        {/* 辩论 — participants trade heated bubbles around the table */}
        {debate &&
          debate.until > Date.now() &&
          meeting &&
          debate.roles.map((r, i) => {
            const s = sim[r];
            if (!s) return null;
            const hot = (Math.floor(Date.now() / 1300) + i) % 2 === 0;
            return (
              <span
                key={`db-${r}`}
                className="pointer-events-none absolute z-[70] text-[13px]"
                style={{ left: `${s.x + 1}%`, top: `${s.y - 9}%` }}
              >
                {hot ? "💢" : "💬"}
              </span>
            );
          })}

        {/* 猫来串门 — a worker near the cat gets a little ❤️ */}
        {catPos.current &&
          (() => {
            const c = catPos.current!;
            const near = defs.find((d) => {
              const s = sim[d.key];
              return s && !s.walking && Math.abs(s.x - c.x) + Math.abs(s.y - c.y) < 8;
            });
            if (!near) return null;
            const s = sim[near.key];
            return (
              <span
                className="pointer-events-none absolute z-[60] text-[11px]"
                style={{ left: `${s.x + 2}%`, top: `${s.y - 8}%` }}
              >
                ❤️
              </span>
            );
          })()}

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
        {(bellRinging || coffee || offDuty || meeting || lastActivity || (bonk && bonk.until > Date.now())) && (
          <div className="absolute bottom-2 left-3 z-40 max-w-[48%] truncate rounded-[4px] border border-ink/25 bg-surface/90 px-2 py-[3px] font-mono text-[10px] text-ink-2">
            {bellRinging
              ? "🔔 敲钟!任务交付 — 全员庆祝!"
              : coffee
              ? "☕ 咖啡时间!全员回血中 — 谢谢老板"
              : bonk && bonk.until > Date.now()
                ? `🔨 ${WORKERS.find((w) => w.key === bonk.bonker)?.zh}抄起小锤去找${WORKERS.find((w) => w.key === bonk.host)?.zh}「谈谈」`
                : debate && debate.until > Date.now() && meeting
                  ? "💢 例会激辩中 — 各执一词,调度员正在拍板"
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
        <OfficePlan run={run} />
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
          {/* 人设卡 — title / motto / trait chips */}
          <div className="mb-2 rounded-[5px] border border-ink/20 bg-paper px-2 py-1.5">
            <p className="font-mono text-[10px] font-bold" style={{ color: focusedDef.shirt }}>
              {focusedDef.persona.title}
            </p>
            <p className="mt-0.5 font-mono text-[10px] leading-snug text-ink-2">「{focusedDef.persona.motto}」</p>
            <div className="mt-1 flex flex-wrap gap-1">
              {focusedDef.persona.traits.map((t) => (
                <span key={t} className="rounded-[3px] border border-ink/25 bg-surface px-1 py-px font-mono text-[9px] text-ink-2">
                  {t}
                </span>
              ))}
            </div>
            <p className="mt-1 font-mono text-[9px] text-muted">癖好:{focusedDef.persona.quirk}</p>
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

          {/* 拍拍肩 — the worker answers in place, over their head */}
          <button
            type="button"
            className="mt-2 w-full rounded-pixel border-2 border-ink bg-paper px-2 py-1 font-mono text-[10.5px] font-bold text-ink shadow-pixel-sm transition-transform hover:-translate-y-0.5"
            onClick={() => {
              const text = progressLine(
                focusedView,
                focusedTodos,
                disabledSet.has(focusedDef.key),
              );
              setSpeech((prev) => ({
                ...prev,
                [focusedDef.key]: { text, until: Date.now() + 6500 },
              }));
              pulses.current[focusedDef.key] = { g: "talk", until: Date.now() + 2600 };
              setFocus(null); // step back so the answer bubble is visible
            }}
          >
            拍拍肩 · 问进度
          </button>

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
// 串门对话台词 — 做成真·一问一答:访客发问/提议(ask),主人回应(reply),
// 临走道别(bye)。按 (角色 + 1.6s 时间窗) 确定性取词,一个「回合」里稳定不闪。
const CHAT_ASK = [
  "在忙啥呢?",
  "你看这样行不?",
  "这块能搭把手不?",
  "进度到哪啦?",
  "有空同步下不?",
  "刚冒个新点子!",
  "帮我瞅一眼?",
  "卡了个问题…",
  "咖啡要不要续?",
  "这事儿归谁啊?",
];
const CHAT_REPLY = [
  "嗯嗯有道理!",
  "这块归我!",
  "马上就好~",
  "哈哈确实",
  "收到收到",
  "我看看哈",
  "稳的,放心!",
  "对对对!",
  "好嘞没问题",
  "让我想想…",
];
const CHAT_BYE = ["回头聊!", "加油加油!", "来,击个掌!", "我先回工位啦", "干得漂亮!", "散会散会~"];
function pickChatLine(key: string, kind: "ask" | "reply" | "bye"): string {
  const pool = kind === "bye" ? CHAT_BYE : kind === "reply" ? CHAT_REPLY : CHAT_ASK;
  let h = 0;
  for (let i = 0; i < key.length; i++) h = (h + key.charCodeAt(i)) % 997;
  return pool[(h + Math.floor(Date.now() / 1600)) % pool.length];
}

// progressLine — the in-character answer to 拍拍肩·问进度, synthesized from
// the worker's LIVE view + role-tagged todos (no LLM, no fabrication: every
// number/title comes from real events, per the faithfulness rule).
function progressLine(
  view: WorkerView | undefined,
  todos: { title: string; status: string }[],
  disabled: boolean,
): string {
  if (disabled) return "我这轮被停用了,活都在别人手上。";
  const pending = todos.filter((t) => t.status === "pending");
  const doing = todos.find((t) => t.status === "doing");
  if (view?.status === "working") {
    const what = view.detail || doing?.title || "";
    return `正忙着${what ? `「${what.slice(0, 16)}」` : "手头的活"},已出工 ×${view.calls}${
      pending.length ? `;后面还排着 ${pending.length} 件` : ""
    }。`;
  }
  if (view?.status === "done")
    return `我这边收工了 — 出工 ×${view.calls},用时 ${fmtDuration(view.totalMs)}。成果在作品包里。`;
  if (pending.length) return `还没轮到我 — 排着 ${pending.length} 件,先是「${pending[0].title.slice(0, 18)}」。`;
  return "我在待命,这轮还没派到我的活。";
}

function OfficeWorker({
  def,
  view,
  sim,
  offDuty,
  onClick,
  say,
  acceptKind,
  dragKind,
  onDropPayload,
}: {
  def: WorkerDef;
  view?: WorkerView;
  sim?: SimView;
  offDuty: boolean;
  /** the worker's current spoken line, shown as a speech bubble over the head. */
  say?: string;
  /** shift=true → 老板的小锤 (boss discipline easter egg). */
  onClick: (shift: boolean) => void;
  /** which drag payload this worker takes right now (null = not a target). */
  acceptKind?: "figure" | "report" | null;
  /** the payload kind currently being dragged anywhere on the page. */
  dragKind?: "figure" | "report" | null;
  onDropPayload?: (kind: "figure" | "report", index: number) => void;
}) {
  const status = view?.status ?? "idle";
  const droppable = !!acceptKind && dragKind === acceptKind;
  const [dropHover, setDropHover] = useState(false);
  const [pokedUntil, setPokedUntil] = useState(0);
  const poked = pokedUntil > 0;
  useEffect(() => {
    if (!poked) return;
    const t = setTimeout(() => setPokedUntil(0), 1500);
    return () => clearTimeout(t);
  }, [pokedUntil, poked]);

  // 口头禅 — every so often a standing worker blurts their persona motto
  const [mottoShow, setMottoShow] = useState(false);
  useEffect(() => {
    const id = setInterval(() => {
      if (Math.random() < 0.08) {
        setMottoShow(true);
        setTimeout(() => setMottoShow(false), 3000);
      }
    }, 7000);
    return () => clearInterval(id);
  }, []);

  if (!sim) return null;
  const gesture = poked && !sim.walking ? "wave" : sim.gesture;
  // bubble priority: real narration > 串门闲聊台词. emote glyph is the fallback.
  const narration = !sim.walking ? say : undefined;
  const chatLine = !sim.walking && !narration && sim.chat ? pickChatLine(def.key, sim.chat) : undefined;
  // 物件交互台词(接咖啡/翻书…)排在真叙述与串门之后
  const bubbleText = narration ?? chatLine ?? (!sim.walking ? sim.say : undefined);
  const emoKey = !sim.walking && !bubbleText && gesture ? ACT[gesture]?.emo : undefined;
  const motto = mottoShow && !sim.walking && !poked && !bubbleText;

  return (
    <button
      type="button"
      onClick={(e) => {
        if (!e.shiftKey) setPokedUntil(Date.now());
        onClick(e.shiftKey);
      }}
      onDragOver={(e) => {
        if (!droppable) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
        setDropHover(true);
      }}
      onDragLeave={() => setDropHover(false)}
      onDrop={(e) => {
        setDropHover(false);
        if (!droppable || !acceptKind) return;
        e.preventDefault();
        e.stopPropagation();
        try {
          const raw = e.dataTransfer.getData(`application/x-claw-${acceptKind}`);
          const payload = raw ? (JSON.parse(raw) as { index?: number }) : {};
          onDropPayload?.(acceptKind, payload.index ?? 0);
        } catch {
          onDropPayload?.(acceptKind, 0);
        }
      }}
      className="absolute cursor-pointer border-0 bg-transparent p-0"
      style={{
        left: `${sim.x}%`,
        top: `${sim.y}%`,
        transform: "translateY(-100%)",
        transition: "left 0.12s linear, top 0.12s linear",
        zIndex: droppable ? 95 : Math.round(sim.y) + 1,
      }}
      aria-label={`${def.zh} · ${status}`}
    >
      {/* drop affordance — valid recipients glow while a payload is in flight */}
      {droppable && (
        <span
          className={clsx(
            "pointer-events-none absolute -inset-1.5 z-0 rounded-pixel border-2 border-dashed",
            dropHover ? "animate-pixpulse border-accent bg-accent/15" : "border-accent/60 bg-accent/5",
          )}
        />
      )}
      {bubbleText ? (
        <div className="claw-emote claw-emote-show absolute bottom-full left-1/2 z-20 mb-1.5 w-[150px] -translate-x-1/2 rounded-pixel border-2 border-ink bg-surface px-2 py-1 text-left font-mono text-[10px] leading-snug text-ink shadow-pixel-sm">
          {bubbleText}
          <span className="absolute -bottom-[6px] left-1/2 h-0 w-0 -translate-x-1/2 border-x-[5px] border-t-[6px] border-x-transparent border-t-ink" />
        </div>
      ) : motto ? (
        <div className="claw-emote claw-emote-show absolute -top-7 left-1/2 z-10 flex items-center justify-center whitespace-nowrap rounded-[5px] border-2 border-ink bg-surface px-1.5 py-0.5 font-mono text-[9px] font-bold text-ink shadow-pixel-sm">
          {def.persona.motto}
        </div>
      ) : emoKey ? (
        <div
          key={gesture}
          className="claw-emote claw-emote-show absolute -top-6 left-1/2 z-10 flex h-5 min-w-[18px] items-center justify-center rounded-[5px] border-2 border-ink bg-surface px-1 font-mono text-[10px] font-extrabold text-ink shadow-pixel-sm"
        >
          {EMO[emoKey]}
        </div>
      ) : null}
      <span className="absolute -bottom-[3px] left-1/2 h-[5px] w-[36px] -translate-x-1/2 rounded-[50%] bg-ink/15" />
      {/* v16 — cute chibi rendered as a true frame strip (hand-posed walk/idle/
          sit cells snapped with steps()). Natural motion, keeps the chibi
          charm. facing flips horizontally; sit when working at the desk. */}
      <span
        className="block"
        style={{ filter: status === "idle" && !offDuty && !poked ? "grayscale(0.6) opacity(0.6)" : "none" }}
      >
        <ChibiWorker
          def={def}
          facing={sim.facing}
          // 在自己工位上(没走动)就坐下来干活 —— 不管在忙/待命/做完
          state={sim.walking ? "walk" : sim.site === "desk" ? "sit" : "idle"}
          gesture={sim.walking ? undefined : gesture}
          size={OFFICE_CONFIG.spriteSize + 8}
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

// OfficePlan — the live sub-task checklist, floated at the office top-right
// (moved out of the chat drawer). Seeded from the polled run.plan, driven
// live by claw.plan / claw.task.update; collapsible so it never crowds the
// scene.
function OfficePlan({ run }: { run: ClawRun }) {
  const stream = useAgentEventStream();
  const [plan, setPlan] = useState<ClawTask[]>(run.plan ?? []);
  const [open, setOpen] = useState(true);

  useEffect(() => {
    if (run.plan && run.plan.length > 0) setPlan((cur) => (cur.length === 0 ? run.plan! : cur));
  }, [run.plan]);

  useEffect(() => {
    const handle = (ev: AgentEvent) => {
      if (ev.kind === "claw.plan") {
        const titles = ev.data.task_titles ?? [];
        const roles = ev.data.task_roles ?? [];
        setPlan(titles.map((t, i) => ({ title: t, role: roles[i], status: "pending" as const })));
        setOpen(true);
      } else if (ev.kind === "claw.task.update") {
        const idx = ev.data.task_index ?? 0;
        const status = (ev.data.task_status ?? "pending") as ClawTask["status"];
        setPlan((prev) => {
          if (idx < 1 || idx > prev.length) return prev;
          const next = prev.slice();
          next[idx - 1] = { ...next[idx - 1], status };
          return next;
        });
      }
    };
    return stream.subscribe(handle);
  }, [stream]);

  if (plan.length === 0) return null;
  const done = plan.filter((t) => t.status === "done" || t.status === "skipped").length;

  return (
    <div className="absolute right-3 top-11 z-[98] w-[clamp(210px,24vw,270px)]">
      {open ? (
        <div className="overflow-hidden rounded-pixel border-2 border-ink bg-paper/95 shadow-pixel-sm backdrop-blur-[1px]">
          <button
            type="button"
            onClick={() => setOpen(false)}
            className="flex w-full items-center justify-between border-b-2 border-ink bg-surface-2 px-2 py-1"
            aria-label="收起任务清单"
          >
            <span className="font-pixel text-[0.5rem] tracking-wide text-accent">✦ 任务清单</span>
            <span className="flex items-center gap-1 font-mono text-[10px] tabular-nums text-muted">
              {done}/{plan.length}
              <ChevronRight size={11} strokeWidth={2.4} className="rotate-[-90deg]" />
            </span>
          </button>
          <ol className="max-h-[42vh] divide-y divide-line overflow-y-auto">
            {plan.map((t, i) => (
              <li key={i} className="flex items-start gap-2 px-2.5 py-1.5">
                <span
                  className={clsx(
                    "mt-[1px] grid h-[14px] w-[14px] flex-none place-items-center rounded-[3px] border-2 border-ink text-white",
                    t.status === "done" && "bg-grass",
                    t.status === "doing" && "animate-pixpulse bg-accent",
                    t.status === "pending" && "bg-surface",
                    t.status === "skipped" && "bg-surface-2",
                  )}
                >
                  {t.status === "done" && <Check size={9} strokeWidth={3} />}
                  {t.status === "doing" && <span className="h-[5px] w-[5px] rounded-full bg-white" />}
                </span>
                <span
                  className={clsx(
                    "font-mono text-[11px] leading-snug",
                    t.status === "doing" ? "font-semibold text-ink" : t.status === "skipped" ? "text-muted line-through" : t.status === "pending" ? "text-ink-2" : "text-ink",
                  )}
                >
                  {t.title}
                </span>
              </li>
            ))}
          </ol>
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="ml-auto flex items-center gap-1.5 rounded-pixel border-2 border-ink bg-paper/95 px-2 py-1 shadow-pixel-sm"
        >
          <span className="font-pixel text-[0.5rem] tracking-wide text-accent">✦ 任务</span>
          <span className="font-mono text-[10px] tabular-nums text-muted">{done}/{plan.length}</span>
          <ChevronLeft size={11} strokeWidth={2.4} className="rotate-[-90deg]" />
        </button>
      )}
    </div>
  );
}

function PhaseStrip({ views, finished }: { views: Record<string, WorkerView>; finished: boolean }) {
  const states = PHASES.map((ph) => {
    const sts = ph.workers.map((k) => views[k]?.status ?? "idle");
    const working = sts.includes("working");
    const touched = ph.workers.some((k) => (views[k]?.calls ?? 0) > 0 || views[k]?.status === "done");
    const done = !working && (finished ? touched : touched && sts.every((s) => s !== "working"));
    return { working, done };
  });
  // SA L3 — project the NEXT stage: the first untouched phase after the last
  // active/finished one gets a ▷ marker so "what happens next" is ambient.
  const lastActive = states.reduce((acc, s, i) => (s.working || s.done ? i : acc), -1);
  const nextIdx = finished ? -1 : states.findIndex((s, i) => i > lastActive && !s.working && !s.done);
  return (
    <div className="absolute right-3 top-2 z-40 flex items-center gap-1 rounded-[5px] border border-ink/25 bg-surface/90 px-1.5 py-1">
      {PHASES.map((ph, i) => {
        const { working, done } = states[i];
        const next = i === nextIdx;
        return (
          <span key={ph.zh} className="flex items-center gap-1">
            {i > 0 && <span className="font-mono text-[9px] text-line-2">▸</span>}
            <span
              title={next ? "接下来" : undefined}
              className={clsx(
                "rounded-[3px] px-1.5 py-[1px] font-mono text-[9.5px] font-bold leading-none",
                working
                  ? "animate-pixpulse bg-accent text-white"
                  : done
                    ? "bg-grass/15 text-grass"
                    : next
                      ? "border border-dashed border-accent/60 text-accent"
                      : "text-muted",
              )}
            >
              {next && "▷ "}
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

// Sprite — renders a named v12 pixel asset (officeArt.SPRITES) at its native
// size (× optional scale). The SVG strings already carry class + crispEdges;
// we inject width/height so the browser rasterises at the intended pixel size.
function Sprite({ name, scale = 1, className, style }: { name: string; scale?: number; className?: string; style?: React.CSSProperties }) {
  const s = SPRITES[name];
  if (!s) return null;
  const w = Math.round(s.w * scale);
  const h = Math.round(s.h * scale);
  const html = s.svg.replace("<svg ", `<svg width="${w}" height="${h}" `);
  return <span className={className} style={{ display: "inline-block", lineHeight: 0, ...style }} dangerouslySetInnerHTML={{ __html: html }} />;
}

// Decor v12 — props placed by the去-AI-味 layout (officeArt). Wall fixtures
// hang on the upper band; floor props stand (bottom-anchored, z by depth).
function Decor({
  light = "day",
  onCoffee,
}: {
  light?: "day" | "dusk" | "night";
  onCoffee?: () => void;
}) {
  const glow = light === "night" ? 0.6 : light === "dusk" ? 0.32 : 0.12;
  const zOf = (t: string) => Math.round(parseFloat(t));

  // wall fixtures (top band, top-anchored)
  const WALL: { n: string; l: string; t: string }[] = [
    { n: "window", l: "4%", t: "3%" },
    { n: "corkboard", l: "26%", t: "5%" },
    { n: "whiteboard", l: "44%", t: "2%" },
    { n: "clock", l: "90%", t: "4%" },
    { n: "plant-hang", l: "80%", t: "2%" },
  ];
  // floor props (bottom-anchored at their y), placed near their owning zone
  const FLOOR: { n: string; l: string; t: string; scale?: number }[] = [
    // 茶水休息区 (bottom-left)
    { n: "fridge", l: "15%", t: "84%" },
    { n: "snack-shelf", l: "1%", t: "72%" },
    { n: "beanbag", l: "9%", t: "97%" },
    // plants
    { n: "plant-big", l: "26%", t: "76%" },
    { n: "plant-mid", l: "95%", t: "60%" },
    // 后期棚 + 执行工坊 back wall
    { n: "bookshelf", l: "34%", t: "31%" },
    // flavor props beside their desk (only those with real sprites)
    { n: "server-rack", l: "28%", t: "47%" },
    { n: "easel", l: "3%", t: "62%" },
    { n: "tripod-light", l: "93%", t: "73%" },
    { n: "paper-stack", l: "54%", t: "59%" },
  ];

  return (
    <>
      {WALL.map((p) => (
        <Sprite key={p.n} name={p.n} className="absolute" style={{ left: p.l, top: p.t }} />
      ))}

      {/* wall sconces with a glow that grows after dark */}
      {[{ l: "19%" }, { l: "73%" }].map((s, i) => (
        <div key={`sconce-${i}`} className="absolute" style={{ left: s.l, top: "16%" }}>
          <div
            className="pointer-events-none absolute rounded-full"
            style={{ left: "-28px", top: "-24px", width: "74px", height: "74px", background: `radial-gradient(circle,rgba(255,206,120,${glow}) 0%,transparent 70%)` }}
          />
          <Sprite name="sconce" className="relative" />
        </div>
      ))}

      {FLOOR.map((p) => (
        <Sprite
          key={p.n}
          name={p.n}
          scale={p.scale}
          className="absolute"
          style={{ left: p.l, top: p.t, transform: "translateY(-100%)", zIndex: zOf(p.t) }}
        />
      ))}

      {/* coffee machine — clickable, steaming, by the lounge */}
      <div
        className="absolute cursor-pointer transition-transform hover:scale-105"
        style={{ left: "1%", top: "84%", transform: "translateY(-100%)", zIndex: 84 }}
        onClick={onCoffee}
        title="请大家喝咖啡 ☕"
      >
        <span className="claw-steam absolute left-[16px] top-[6px] h-[7px] w-[3px] rounded-full bg-white/70" />
        <span className="claw-steam absolute left-[23px] top-[9px] h-[6px] w-[3px] rounded-full bg-white/60" style={{ animationDelay: "0.8s" }} />
        <span className="claw-steam absolute left-[29px] top-[7px] h-[5px] w-[2px] rounded-full bg-white/50" style={{ animationDelay: "1.5s" }} />
        <Sprite name="coffee-machine" />
      </div>

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
    </>
  );
}

// ── the office cat ──────────────────────────────────────────────────────
// Wanders the floor on a slow CSS glide, occasionally curls up for a nap by
// the floor lamp, and pops hearts when petted. Purely cosmetic — runs on its
// own timer, independent of the work sim.
const CAT_NAP = { x: 7, y: 60 }; // beside the floor lamp
function OfficeCat({
  posRef,
}: {
  posRef?: React.MutableRefObject<{ x: number; y: number; sleeping: boolean } | null>;
}) {
  const [pos, setPos] = useState({ x: 70, y: 72 });
  const [mode, setMode] = useState<"wander" | "sleep">("wander");
  useEffect(() => {
    if (posRef) posRef.current = { x: pos.x, y: pos.y, sleeping: mode === "sleep" };
  }, [pos, mode, posRef]);
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
