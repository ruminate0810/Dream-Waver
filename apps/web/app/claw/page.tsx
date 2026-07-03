"use client";

import { useEffect, useMemo, useState } from "react";
import { ArrowLeft, Plus } from "lucide-react";
import clsx from "clsx";

import { listClawRuns, type ClawRunSummary } from "@/lib/api";
import { WORKERS } from "@/components/claw/workers";
import { StatusChip, type PixelStatus } from "@/components/ui/pixel";

// /claw — the studio home (v25 工作室第一版). The office stops being a
// per-run stage and becomes a persistent workplace: past runs are 案卷 on
// the shelf, the team has cross-run dossiers, and new work goes in from
// the front desk. Dossier numbers are LEDGER facts aggregated from the
// runs' role-tagged plans — no levels, no XP, nothing uncheckable; every
// number expands to the task list that produced it.

const STATUS_MAP: Record<string, { s: PixelStatus; label: string }> = {
  running: { s: "working", label: "工作中" },
  awaiting_input: { s: "queued", label: "待补充" },
  finished: { s: "done", label: "已完成" },
  error: { s: "error", label: "出错" },
};

type DossierRow = {
  key: string;
  zh: string;
  assigned: number;
  done: number;
  skipped: number;
  runs: number;
  tasks: { title: string; status: string; runTitle: string; jobId: string }[];
};

export default function ClawStudioPage() {
  const [runs, setRuns] = useState<ClawRunSummary[] | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);

  useEffect(() => {
    listClawRuns()
      .then(setRuns)
      .catch(() => setRuns([]));
  }, []);

  const dossiers = useMemo<DossierRow[]>(() => {
    const rows: Record<string, DossierRow> = {};
    for (const w of WORKERS) rows[w.key] = { key: w.key, zh: w.zh, assigned: 0, done: 0, skipped: 0, runs: 0, tasks: [] };
    for (const run of runs ?? []) {
      const touched = new Set<string>();
      for (const t of run.plan ?? []) {
        const row = t.role ? rows[t.role] : undefined;
        if (!row) continue;
        row.assigned++;
        if (t.status === "done") row.done++;
        if (t.status === "skipped") row.skipped++;
        row.tasks.push({ title: t.title, status: t.status, runTitle: run.title || run.prompt.slice(0, 24), jobId: run.job_id });
        touched.add(t.role!);
      }
      for (const key of touched) rows[key].runs++;
    }
    return Object.values(rows);
  }, [runs]);

  return (
    <main className="dot-grid min-h-[100dvh] bg-paper text-ink antialiased">
      <header className="border-b-2 border-ink bg-paper/85">
        <div className="mx-auto flex max-w-[1200px] items-center justify-between px-6 py-3">
          <a href="/" className="inline-flex items-center gap-2 font-mono text-[11px] font-semibold text-ink-2 hover:text-ink">
            <span className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm">
              <ArrowLeft size={11} strokeWidth={2} />
            </span>
            DREAM-WAVER / INDEX
          </a>
          <a
            href="/claw/new"
            className="inline-flex items-center gap-1.5 rounded-pixel border-2 border-ink bg-accent px-3 py-1.5 font-mono text-[12px] font-bold text-white shadow-pixel transition-transform hover:-translate-y-0.5"
          >
            <Plus size={13} strokeWidth={2.6} /> 派新活
          </a>
        </div>
      </header>

      <div className="mx-auto max-w-[1200px] px-6 py-6">
        <h1 className="font-pixel text-[0.9rem] tracking-wide text-ink">✦ CLAW 工作室</h1>
        <p className="mt-1 font-mono text-[12px] text-muted">一支常驻的 AI 团队 — 案卷在架上,账本在墙上。</p>

        {/* ── 案卷架 ── */}
        <h2 className="mt-6 mb-2 font-pixel text-[0.6rem] tracking-wide text-accent">§ 案卷架</h2>
        {runs === null ? (
          <p className="font-mono text-[12px] text-muted">翻档案中…</p>
        ) : runs.length === 0 ? (
          <div className="rounded-pixel border-2 border-dashed border-ink/40 p-8 text-center font-mono text-[12px] text-muted">
            案卷架还空着 — <a className="font-bold text-accent underline" href="/claw/new">派第一单活</a>
            <p className="mt-1 text-[10.5px]">(匿名/无持久化运行不会上架)</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {runs.map((r) => {
              const m = STATUS_MAP[r.status] ?? { s: "idle" as PixelStatus, label: r.status };
              return (
                <a
                  key={r.job_id}
                  href={`/claw/${r.job_id}?session=${r.session_id}`}
                  className="group rounded-pixel border-2 border-ink bg-surface p-3 shadow-pixel-sm transition-transform hover:-translate-y-0.5"
                >
                  <div className="flex items-start justify-between gap-2">
                    <p className="line-clamp-2 font-mono text-[12.5px] font-bold leading-snug text-ink">
                      {r.title || r.prompt}
                    </p>
                    <StatusChip status={m.s}>{m.label}</StatusChip>
                  </div>
                  <p className="mt-2 flex flex-wrap gap-x-2 font-mono text-[10.5px] text-ink-2">
                    {r.artifact_version > 0 && <span>报告 v{r.artifact_version}</span>}
                    {r.figures > 0 && <span>图 ×{r.figures}</span>}
                    {r.has_deck && <span>PPT</span>}
                    {r.videos > 0 && <span>视频 ×{r.videos}</span>}
                    {r.artifact_version === 0 && r.figures === 0 && !r.has_deck && r.videos === 0 && <span>—</span>}
                  </p>
                  <p className="mt-1 font-mono text-[10px] text-muted">{new Date(r.started_at).toLocaleString("zh-CN")}</p>
                </a>
              );
            })}
          </div>
        )}

        {/* ── 团队履历(账本,不是等级) ── */}
        <h2 className="mt-8 mb-2 font-pixel text-[0.6rem] tracking-wide text-accent">§ 团队履历</h2>
        <p className="mb-2 font-mono text-[10.5px] text-muted">每个数字都可展开成产生它的任务清单 — 账本事实,无经验值。</p>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {dossiers.map((d) => (
            <button
              key={d.key}
              type="button"
              onClick={() => setExpanded((e) => (e === d.key ? null : d.key))}
              className={clsx(
                "rounded-pixel border-2 border-ink bg-surface p-3 text-left shadow-pixel-sm transition-transform hover:-translate-y-0.5",
                expanded === d.key && "ring-2 ring-accent",
              )}
            >
              <p className="font-mono text-[12px] font-bold text-ink">{d.zh}</p>
              <dl className="mt-1.5 space-y-0.5 font-mono text-[10.5px] text-ink-2">
                <div className="flex justify-between"><dt className="text-muted">接单</dt><dd>×{d.assigned}</dd></div>
                <div className="flex justify-between"><dt className="text-muted">完成</dt><dd>×{d.done}</dd></div>
                {d.skipped > 0 && (
                  <div className="flex justify-between"><dt className="text-muted">被跳过</dt><dd style={{ color: "#c96f1f" }}>×{d.skipped}</dd></div>
                )}
                <div className="flex justify-between"><dt className="text-muted">参与案卷</dt><dd>×{d.runs}</dd></div>
              </dl>
            </button>
          ))}
        </div>
        {expanded && (
          <div className="mt-3 rounded-pixel border-2 border-ink bg-surface p-3 shadow-pixel-sm">
            <p className="mb-1.5 font-mono text-[11px] font-bold text-ink">
              {dossiers.find((d) => d.key === expanded)?.zh} · 经手任务
            </p>
            <ul className="max-h-[40vh] space-y-1 overflow-y-auto font-mono text-[11px] text-ink-2">
              {(dossiers.find((d) => d.key === expanded)?.tasks ?? []).map((t, i) => (
                <li key={i} className="flex items-baseline gap-2">
                  <span className={clsx("flex-none", t.status === "done" ? "text-grass" : t.status === "skipped" ? "text-line-2" : "text-muted")}>
                    {t.status === "done" ? "■✓" : t.status === "skipped" ? "⨯" : "□"}
                  </span>
                  <span>{t.title}</span>
                  <a className="ml-auto flex-none text-[10px] text-accent underline" href={`/claw/${t.jobId}`}>
                    {t.runTitle}
                  </a>
                </li>
              ))}
              {(dossiers.find((d) => d.key === expanded)?.tasks.length ?? 0) === 0 && <li className="text-muted">还没接过活。</li>}
            </ul>
          </div>
        )}
      </div>
    </main>
  );
}
