"use client";

import { useEffect, useState } from "react";
import { Plug, Save } from "lucide-react";
import clsx from "clsx";

import { getClawRoles, putClawRoles, type ClawRolesConfig } from "@/lib/api";
import { ACT, TOOL_ACTION, WORKERS } from "./workers";

// BindingsPanel is the 真·动态改绑 editor, rendered inside a PixelWindow from
// the dock: re-assign each rebindable execution tool to any execution-phase
// worker (radio chips), toggle optional workers on/off, save → PUT
// /claw/roles → effective on the NEXT run. The coordinator/writer spine is
// locked; tools whose capability isn't wired are flagged 未接入.

const TOOL_ZH: Record<string, string> = {
  web_search: "联网检索",
  code_execute: "代码执行",
  generate_image: "生成配图",
  write_document: "撰写报告",
  generate_deck: "生成幻灯",
  plan_tasks: "规划任务",
  update_task: "勾选进度",
};

export function BindingsPanel({ onApplied }: { onApplied: (cfg: ClawRolesConfig) => void }) {
  const [cfg, setCfg] = useState<ClawRolesConfig | null>(null);
  const [assign, setAssign] = useState<Record<string, string>>({});
  const [enabled, setEnabled] = useState<Record<string, boolean>>({});
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState<string>("");

  useEffect(() => {
    getClawRoles()
      .then((c) => {
        setCfg(c);
        setAssign({ ...c.assign });
        setEnabled(Object.fromEntries(c.roles.map((r) => [r.key, r.enabled])));
      })
      .catch(() => setMsg("加载绑定配置失败 — 后端是否在运行?"));
  }, []);

  if (!cfg) {
    return <div className="p-6 font-mono text-[12px] text-muted">{msg || "加载绑定配置…"}</div>;
  }

  const execRoles = cfg.roles.filter((r) => ["researcher", "engineer", "designer"].includes(r.key));
  const dirty =
    JSON.stringify(assign) !== JSON.stringify(cfg.assign) ||
    cfg.roles.some((r) => (enabled[r.key] ?? r.enabled) !== r.enabled);

  const save = async () => {
    setSaving(true);
    setMsg("");
    try {
      const next = await putClawRoles(assign, enabled);
      setCfg(next);
      setAssign({ ...next.assign });
      setEnabled(Object.fromEntries(next.roles.map((r) => [r.key, r.enabled])));
      onApplied(next);
      setMsg("✓ 已保存 — 下一单生效");
    } catch (e) {
      setMsg(`保存失败:${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex h-full flex-col overflow-y-auto bg-paper/60 p-4">
      <div className="mb-3 flex items-start gap-2">
        <span className="mt-0.5 grid h-6 w-6 flex-none place-items-center rounded-pixel border-2 border-ink bg-accent-soft text-accent">
          <Plug size={13} strokeWidth={2} />
        </span>
        <p className="font-mono text-[12px] leading-relaxed text-ink-2">
          把执行工具改派给任意执行角色、或启停可选角色 — 保存后<b className="text-ink">下一单</b>即按新绑定派活(计划提示词、子
          agent 工具箱、工位点亮全部跟着走)。
        </p>
      </div>

      {/* tool → worker assignment */}
      <p className="mb-1.5 font-mono text-[11px] font-bold tracking-wide text-muted">工具改派(每个工具归一名执行角色)</p>
      <div className="mb-4 space-y-2">
        {cfg.pool.map((tool) => (
          <div key={tool} className="flex items-center gap-2 rounded-pixel border-2 border-line bg-surface px-2.5 py-2">
            <span className="w-[118px] flex-none font-mono text-[12px] font-semibold text-ink">
              {TOOL_ZH[tool] ?? tool}
              {!cfg.tool_wired[tool] && <span className="ml-1 text-[9.5px] font-normal text-gold">未接入</span>}
              <span className="block text-[9px] font-normal text-muted">{tool}{TOOL_ACTION[tool] && ACT[TOOL_ACTION[tool]] ? ` · ${ACT[TOOL_ACTION[tool]].zh}` : ""}</span>
            </span>
            <div className="flex flex-1 flex-wrap gap-1">
              {execRoles.map((role) => {
                const zh = WORKERS.find((w) => w.key === role.key)?.zh ?? role.name;
                const active = assign[tool] === role.key;
                return (
                  <button
                    key={role.key}
                    type="button"
                    onClick={() => setAssign((a) => ({ ...a, [tool]: role.key }))}
                    className={clsx(
                      "rounded-pixel border-2 px-2 py-1 font-mono text-[11px] font-semibold transition-colors",
                      active ? "border-ink bg-accent text-white shadow-pixel-sm" : "border-line-2 bg-surface text-ink-2 hover:border-ink hover:text-ink",
                    )}
                  >
                    {zh}
                  </button>
                );
              })}
            </div>
          </div>
        ))}
      </div>

      {/* role enablement */}
      <p className="mb-1.5 font-mono text-[11px] font-bold tracking-wide text-muted">角色启停</p>
      <div className="mb-4 flex flex-wrap gap-1.5">
        {cfg.roles.map((role) => {
          const zh = WORKERS.find((w) => w.key === role.key)?.zh ?? role.name;
          const on = enabled[role.key] ?? role.enabled;
          return (
            <button
              key={role.key}
              type="button"
              disabled={role.locked}
              onClick={() => setEnabled((e) => ({ ...e, [role.key]: !on }))}
              className={clsx(
                "rounded-pixel border-2 px-2.5 py-1 font-mono text-[11px] font-semibold transition-colors",
                role.locked
                  ? "cursor-not-allowed border-line-2 bg-surface-2 text-muted"
                  : on
                    ? "border-ink bg-grass/15 text-grass"
                    : "border-line-2 bg-surface text-line-2 line-through",
              )}
              title={role.locked ? "每单必需,不可停用" : on ? "点击停用" : "点击启用"}
            >
              {zh}
              {role.locked ? " 🔒" : on ? " ✓" : ""}
            </button>
          );
        })}
      </div>

      <div className="mt-auto flex items-center justify-between gap-2 border-t-2 border-line pt-3">
        <span className={clsx("font-mono text-[11px]", msg.startsWith("✓") ? "text-grass" : "text-gold")}>{msg}</span>
        <button
          type="button"
          onClick={save}
          disabled={!dirty || saving}
          className="inline-flex items-center gap-1.5 rounded-pixel border-2 border-ink bg-accent px-3.5 py-1.5 font-mono text-[12px] font-semibold text-white shadow-pixel-sm transition-transform hover:translate-x-[1px] hover:translate-y-[1px] active:translate-x-[3px] active:translate-y-[3px] active:!shadow-none disabled:opacity-50"
        >
          <Save size={13} strokeWidth={2.2} />
          {saving ? "保存中…" : "保存绑定"}
        </button>
      </div>
    </div>
  );
}
