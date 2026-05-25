"use client";

import { useEffect, useState } from "react";
import { Check, ChevronDown, Loader2, Plus, Users } from "lucide-react";
import clsx from "clsx";

import {
  createWorkspace,
  listWorkspaces,
  type Workspace,
} from "@/lib/api";
import {
  getActiveWorkspaceID,
  setActiveWorkspaceID,
} from "@/lib/workspace";

// Switcher renders the active workspace name + dropdown of every
// workspace the user belongs to. Selecting one writes the cookie and
// reloads the page (cheapest way to ensure every component re-fetches
// data scoped to the new workspace; an in-memory state-bus would
// require touching every data hook).
//
// Drop it in the page header alongside the user avatar. The dropdown
// also has a "Create team workspace" button at the bottom that opens
// a tiny inline form — no separate /workspaces/new page.

export function WorkspaceSwitcher() {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [activeID, setActiveIDState] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [err, setErr] = useState<string | null>(null);

  // Initial load.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listWorkspaces()
      .then((res) => {
        if (cancelled) return;
        setWorkspaces(res.workspaces);
        const cookie = getActiveWorkspaceID();
        if (cookie && res.workspaces.find((w) => w.id === cookie)) {
          setActiveIDState(cookie);
        } else if (res.workspaces[0]) {
          // No valid cookie → default to first workspace.
          setActiveWorkspaceID(res.workspaces[0].id);
          setActiveIDState(res.workspaces[0].id);
        }
      })
      .catch((e) => setErr(e instanceof Error ? e.message : String(e)))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  function pick(id: string) {
    setActiveWorkspaceID(id);
    setActiveIDState(id);
    setOpen(false);
    // Reload so every data fetch on the page re-runs with the new
    // X-Workspace-ID header. Fine for MVP; a real state bus is a
    // Phase 4 polish.
    window.location.reload();
  }

  async function onCreate(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    if (!newName.trim()) return;
    try {
      const ws = await createWorkspace({ name: newName.trim() });
      setWorkspaces((prev) => [ws, ...prev]);
      pick(ws.id);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }

  const active = workspaces.find((w) => w.id === activeID);

  if (loading) {
    return (
      <div className="inline-flex items-center gap-1.5 px-2 py-1 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-400">
        <Loader2 size={11} className="animate-spin" />
        Loading
      </div>
    );
  }

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="inline-flex items-center gap-1.5 rounded-md border border-zinc-200 bg-white px-2.5 py-1 text-[12px] text-zinc-700 transition hover:bg-zinc-50"
      >
        {active?.kind === "team" ? (
          <Users size={11} className="text-zinc-500" />
        ) : (
          <span className="h-2 w-2 rounded-full bg-emerald-500" aria-hidden />
        )}
        <span className="max-w-[140px] truncate">{active?.name ?? "Workspace"}</span>
        <ChevronDown size={11} className="text-zinc-400" />
      </button>

      {open && (
        <div className="absolute right-0 top-full z-40 mt-1 w-72 rounded-lg border border-zinc-200 bg-white p-1 shadow-lg">
          <div className="px-2 py-1.5 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-400">
            Switch workspace
          </div>
          {workspaces.map((w) => (
            <button
              key={w.id}
              type="button"
              onClick={() => pick(w.id)}
              className={clsx(
                "flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[13px] transition",
                "hover:bg-zinc-50",
              )}
            >
              {w.kind === "team" ? (
                <Users size={12} className="text-zinc-400" />
              ) : (
                <span className="h-2 w-2 rounded-full bg-emerald-500" aria-hidden />
              )}
              <span className="flex-1 truncate">{w.name}</span>
              {w.id === activeID && (
                <Check size={12} className="text-emerald-600" strokeWidth={2.5} />
              )}
            </button>
          ))}

          <div className="my-1 h-px bg-zinc-100" />

          {!creating ? (
            <button
              type="button"
              onClick={() => setCreating(true)}
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[12px] text-zinc-600 transition hover:bg-zinc-50"
            >
              <Plus size={12} />
              Create team workspace
            </button>
          ) : (
            <form onSubmit={onCreate} className="space-y-1.5 p-2">
              <input
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="Team name…"
                autoFocus
                className="w-full rounded border border-zinc-200 px-2 py-1 text-[12px] focus:border-zinc-400 focus:outline-none"
              />
              <div className="flex gap-1">
                <button
                  type="submit"
                  disabled={!newName.trim()}
                  className="flex-1 rounded bg-zinc-900 px-2 py-1 text-[11px] font-medium text-white disabled:opacity-50"
                >
                  Create
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setCreating(false);
                    setNewName("");
                  }}
                  className="rounded border border-zinc-200 px-2 py-1 text-[11px] text-zinc-600"
                >
                  Cancel
                </button>
              </div>
            </form>
          )}

          {err && (
            <div className="m-2 rounded border border-red-200 bg-red-50 px-2 py-1 text-[11px] text-red-700">
              {err}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
