"use client";

import { useEffect, useRef, useState } from "react";
import {
  Check,
  ChevronDown,
  FolderPlus,
  Loader2,
  Pencil,
  Trash2,
} from "lucide-react";
import clsx from "clsx";

import type { DesignProject } from "./designProjects";

// ProjectSwitcher is the design-canvas equivalent of the workspace
// Switcher — a header dropdown listing every design project (a named
// canvas + its chat history), with create / rename / delete. Switching
// a project remounts the TLDraw canvas under a new persistenceKey and
// reloads that project's chat history.
//
// Pure presentation: all persistence lives in designProjects.ts; the
// parent (page.tsx) owns the project list state and passes callbacks.

export type ProjectSwitcherProps = {
  projects: DesignProject[];
  activeId: string | null;
  onSwitch: (id: string) => void;
  onCreate: () => void;
  onRename: (id: string, name: string) => void;
  onDelete: (id: string) => void;
};

export function ProjectSwitcher({
  projects,
  activeId,
  onSwitch,
  onCreate,
  onRename,
  onDelete,
}: ProjectSwitcherProps) {
  const [open, setOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftName, setDraftName] = useState("");
  const containerRef = useRef<HTMLDivElement | null>(null);

  const active = projects.find((p) => p.id === activeId) ?? null;

  useEffect(() => {
    if (!open) return;
    function onDocClick(ev: MouseEvent) {
      if (!containerRef.current) return;
      if (containerRef.current.contains(ev.target as Node)) return;
      setOpen(false);
      setEditingId(null);
    }
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [open]);

  function startEditing(p: DesignProject) {
    setEditingId(p.id);
    setDraftName(p.name);
  }

  function commitEditing() {
    if (editingId && draftName.trim()) {
      onRename(editingId, draftName.trim());
    }
    setEditingId(null);
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        title="Switch session"
        className={clsx(
          "inline-flex max-w-[200px] items-center gap-1.5 rounded-md px-2 py-1 text-[13px] transition",
          open ? "bg-zinc-100 text-zinc-900" : "text-zinc-700 hover:bg-zinc-50",
        )}
      >
        <span className="truncate font-medium">
          {active ? active.name : "Design"}
        </span>
        <ChevronDown size={12} className="shrink-0 text-zinc-400" />
      </button>

      {open && (
        <div className="absolute left-0 top-full z-40 mt-1 w-72 rounded-lg border border-zinc-200 bg-white p-1 shadow-lg">
          <p className="px-2 py-1 font-mono text-[9px] uppercase tracking-[0.16em] text-zinc-400">
            Sessions · {projects.length}
          </p>

          <div className="max-h-[50vh] overflow-y-auto">
            {projects.length === 0 ? (
              <div className="flex items-center gap-1.5 px-2 py-3 text-[12px] text-zinc-400">
                <Loader2 size={12} className="animate-spin" /> Loading…
              </div>
            ) : (
              projects.map((p) => {
                const isActive = p.id === activeId;
                const isEditing = p.id === editingId;
                return (
                  <div
                    key={p.id}
                    className={clsx(
                      "group flex items-center gap-2 rounded-md px-2 py-1.5",
                      isActive ? "bg-fuchsia-50" : "hover:bg-zinc-50",
                    )}
                  >
                    {/* Thumbnail / placeholder */}
                    <div className="h-8 w-8 shrink-0 overflow-hidden rounded border border-zinc-200 bg-zinc-50">
                      {p.thumbnailUrl ? (
                        // eslint-disable-next-line @next/next/no-img-element
                        <img
                          src={p.thumbnailUrl}
                          alt=""
                          className="h-full w-full object-cover"
                        />
                      ) : null}
                    </div>

                    {isEditing ? (
                      <input
                        autoFocus
                        value={draftName}
                        onChange={(e) => setDraftName(e.target.value)}
                        onBlur={commitEditing}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") commitEditing();
                          if (e.key === "Escape") setEditingId(null);
                        }}
                        className="min-w-0 flex-1 rounded border border-zinc-300 px-1.5 py-0.5 text-[12px] focus:border-zinc-500 focus:outline-none"
                      />
                    ) : (
                      <button
                        type="button"
                        onClick={() => {
                          onSwitch(p.id);
                          setOpen(false);
                        }}
                        className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
                      >
                        <span
                          className={clsx(
                            "min-w-0 flex-1 truncate text-[12px]",
                            isActive
                              ? "font-medium text-fuchsia-900"
                              : "text-zinc-800",
                          )}
                        >
                          {p.name}
                        </span>
                        {isActive && (
                          <Check size={12} className="shrink-0 text-fuchsia-600" />
                        )}
                      </button>
                    )}

                    {/* Row actions — appear on hover (or always for active). */}
                    {!isEditing && (
                      <div
                        className={clsx(
                          "flex shrink-0 items-center gap-0.5 transition",
                          "opacity-0 group-hover:opacity-100",
                        )}
                      >
                        <button
                          type="button"
                          onClick={() => startEditing(p)}
                          title="Rename"
                          className="rounded p-0.5 text-zinc-400 hover:bg-zinc-200 hover:text-zinc-700"
                        >
                          <Pencil size={11} />
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            if (
                              window.confirm(
                                `Delete session "${p.name}"? Its canvas + chat history are removed.`,
                              )
                            ) {
                              onDelete(p.id);
                            }
                          }}
                          title="Delete"
                          className="rounded p-0.5 text-zinc-400 hover:bg-red-100 hover:text-red-600"
                        >
                          <Trash2 size={11} />
                        </button>
                      </div>
                    )}
                  </div>
                );
              })
            )}
          </div>

          <div className="mt-1 border-t border-zinc-100 pt-1">
            <button
              type="button"
              onClick={() => {
                onCreate();
                setOpen(false);
              }}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-[12px] text-zinc-700 transition hover:bg-zinc-50"
            >
              <FolderPlus size={13} className="text-zinc-500" />
              New session
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
