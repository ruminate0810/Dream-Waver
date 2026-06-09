"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowDown, ArrowUp, Copy, Plus, Trash2 } from "lucide-react";
import clsx from "clsx";

// SlideToolbar is the per-slide hover strip that floats over a frame's
// top-right corner. It exposes the four structural slide-management actions
// (insert after / duplicate / move up / move down / delete) that previously
// could only be reached by asking the agent in chat. Each button calls back
// into LivePreviewStack, which posts a deterministic instruction naming the
// exact tool — no LLM reasoning, same channel the edit popover uses.
//
// Delete is a two-step confirm (a structural action is destructive and a
// hover strip is easy to fat-finger): first click arms it, second click
// within 3s commits; it auto-disarms otherwise.

export function SlideToolbar({
  oneBased,
  total,
  busy,
  onAdd,
  onDuplicate,
  onMoveUp,
  onMoveDown,
  onDelete,
}: {
  oneBased: number;
  total: number;
  busy: boolean;
  onAdd: () => void;
  onDuplicate: () => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
  onDelete: () => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const disarmRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (disarmRef.current) clearTimeout(disarmRef.current);
    };
  }, []);

  const armOrDelete = () => {
    if (confirmDelete) {
      if (disarmRef.current) clearTimeout(disarmRef.current);
      setConfirmDelete(false);
      onDelete();
      return;
    }
    setConfirmDelete(true);
    disarmRef.current = setTimeout(() => setConfirmDelete(false), 3000);
  };

  return (
    <div
      className={clsx(
        "absolute right-2 top-2 z-20 flex items-center gap-0.5 rounded-[2px] border border-[color:var(--rule)]",
        "bg-[color:var(--paper)]/95 px-1 py-0.5 shadow-[0_2px_10px_-4px_rgba(26,22,20,0.35)] backdrop-blur-sm",
        "opacity-0 transition-opacity duration-200 group-hover/frame:opacity-100 focus-within:opacity-100",
        busy && "pointer-events-none opacity-40",
      )}
      // Don't let toolbar clicks bubble into the frame's edit machinery.
      onClick={(e) => e.stopPropagation()}
    >
      <TBtn label="在此页后新增一页" onClick={onAdd} disabled={busy}>
        <Plus size={13} strokeWidth={1.8} />
      </TBtn>
      <TBtn label="复制本页" onClick={onDuplicate} disabled={busy}>
        <Copy size={12} strokeWidth={1.8} />
      </TBtn>
      <TBtn label="上移一位" onClick={onMoveUp} disabled={busy || oneBased <= 1}>
        <ArrowUp size={13} strokeWidth={1.8} />
      </TBtn>
      <TBtn label="下移一位" onClick={onMoveDown} disabled={busy || oneBased >= total}>
        <ArrowDown size={13} strokeWidth={1.8} />
      </TBtn>
      <span className="mx-0.5 h-3.5 w-px bg-[color:var(--rule)]" aria-hidden />
      {confirmDelete ? (
        <button
          type="button"
          onClick={armOrDelete}
          disabled={busy}
          className="inline-flex items-center gap-1 rounded-[2px] bg-[color:var(--vermillion)] px-1.5 py-1 font-mono-jb text-[9px] uppercase tracking-[0.18em] text-[color:var(--paper)]"
        >
          <Trash2 size={11} strokeWidth={1.8} />
          确认删
        </button>
      ) : (
        <TBtn label="删除本页" onClick={armOrDelete} disabled={busy} danger>
          <Trash2 size={12} strokeWidth={1.8} />
        </TBtn>
      )}
    </div>
  );
}

function TBtn({
  label,
  onClick,
  disabled,
  danger,
  children,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  danger?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      disabled={disabled}
      className={clsx(
        "inline-flex h-6 w-6 items-center justify-center rounded-[2px] transition-colors",
        "text-[color:var(--ink-soft)] hover:bg-[color:var(--ink)]/[0.06]",
        danger ? "hover:text-[color:var(--vermillion)]" : "hover:text-[color:var(--ink)]",
        disabled && "cursor-not-allowed opacity-30 hover:bg-transparent",
      )}
    >
      {children}
    </button>
  );
}
