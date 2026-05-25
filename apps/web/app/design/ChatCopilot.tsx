"use client";

import { useState, type FormEvent, type KeyboardEvent } from "react";
import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Loader2,
  MessageSquarePlus,
  Sparkles,
} from "lucide-react";
import clsx from "clsx";

import type { HistoryEntry } from "./page";

// ChatCopilot is the left drawer: prompt input on top + per-prompt
// history below. Each history entry shows the prompt text and (once
// generation completes) a small thumbnail; clicking the entry calls
// onFocus to pan/zoom the canvas to that image's shape.
//
// This is the "type and forget" entry to the design surface — the
// counterpart to the AI panel (top-right) which is more controllable
// (aspect ratio, variant mode). Both write into the same history
// state at the page level, so the drawer reflects every generation
// regardless of which entry point triggered it.
//
// Architecture decision: the drawer doesn't OWN the in-flight task
// (no SSE plumbing here) — it just emits onSubmit(prompt) and the
// parent's useGenerateWithProgress hook handles the rest. That way
// retry / cancel logic lives in one place and the chat surface
// stays display-only.
//
// MVP scope cuts:
//   - No real LLM agent for "intent classification". Every submit is
//     interpreted as text-to-image. Smart routing ("remove the
//     background of the selected image") is a sprint-W3 task.
//   - No conversation threads — single flat history.
//   - History is in-memory; refresh loses it. Matches TLDraw's
//     localStorage canvas — both get a unified persistence story
//     when /api/v1/design/canvases lands.

export type ChatCopilotProps = {
  history: HistoryEntry[];
  /** Called when the user submits a prompt — parent kicks off SSE
   *  generation, places placeholder + image on canvas. */
  onSubmit: (prompt: string) => void;
  /** Called when the user clicks a history entry — parent should
   *  focus that shape on the canvas (select + zoom). No-op if the
   *  shape was deleted from the canvas. */
  onFocus: (shapeId: string) => void;
};

// Persisted across drawer open/close so the user's typed-but-not-
// submitted prompt isn't lost when they hide the drawer.
//
// Lives in component state (not a ref) so the textarea reflects it.
// localStorage-backed persistence is a follow-up if users ask for it.

export function ChatCopilot({ history, onSubmit, onFocus }: ChatCopilotProps) {
  const [open, setOpen] = useState(true);
  const [prompt, setPrompt] = useState("");

  function submit(e?: FormEvent) {
    e?.preventDefault();
    const trimmed = prompt.trim();
    if (!trimmed) return;
    onSubmit(trimmed);
    setPrompt("");
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    // Cmd/Ctrl-Enter or plain Enter submits; Shift-Enter for newline.
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex w-10 shrink-0 items-center justify-center border-r border-zinc-200 bg-white text-zinc-500 transition hover:bg-zinc-50 hover:text-zinc-800"
        aria-label="Open chat copilot"
      >
        <ChevronRight size={14} />
      </button>
    );
  }

  return (
    <aside className="flex w-80 shrink-0 flex-col border-r border-zinc-200 bg-white">
      <header className="flex items-baseline justify-between border-b border-zinc-100 px-4 py-3">
        <h2 className="inline-flex items-baseline gap-1.5 text-[13px] font-medium text-zinc-900">
          <Sparkles size={12} className="self-center text-fuchsia-500" />
          Copilot
        </h2>
        <button
          type="button"
          onClick={() => setOpen(false)}
          className="text-zinc-400 hover:text-zinc-700"
          aria-label="Close chat copilot"
        >
          <ChevronLeft size={14} />
        </button>
      </header>

      {/* Prompt input — pinned at top so it's reachable without
          scrolling through history first. */}
      <form onSubmit={submit} className="border-b border-zinc-100 p-3">
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder="Describe an image…&#10;e.g. minimalist mountain landscape, golden hour"
          rows={3}
          spellCheck={false}
          className="w-full resize-none rounded-md border border-zinc-200 px-2.5 py-2 text-[13px] focus:border-zinc-400 focus:outline-none"
        />
        <div className="mt-2 flex items-center justify-between">
          <span className="text-[10px] text-zinc-400">
            Enter = send · Shift+Enter = newline
          </span>
          <button
            type="submit"
            disabled={!prompt.trim()}
            className="inline-flex items-center gap-1.5 rounded-md bg-zinc-900 px-2.5 py-1.5 text-[12px] font-medium text-white disabled:opacity-50"
          >
            <MessageSquarePlus size={12} />
            Send
          </button>
        </div>
      </form>

      {/* History — flat list of every prompt the canvas has fired,
          newest first. Entries paint as "running" until SSE done. */}
      <div className="flex-1 overflow-y-auto px-2 py-2">
        {history.length === 0 ? (
          <p className="px-2 py-6 text-center text-[12px] text-zinc-400">
            Your generations land here as you type.
          </p>
        ) : (
          <ul className="space-y-1">
            {history.map((h) => (
              <HistoryRow key={h.id} entry={h} onFocus={onFocus} />
            ))}
          </ul>
        )}
      </div>
    </aside>
  );
}

function HistoryRow({
  entry,
  onFocus,
}: {
  entry: HistoryEntry;
  onFocus: (shapeId: string) => void;
}) {
  const canFocus = entry.status === "done" && entry.shapeId !== undefined;

  return (
    <li>
      <button
        type="button"
        onClick={() => {
          if (canFocus && entry.shapeId) onFocus(entry.shapeId);
        }}
        disabled={!canFocus}
        className={clsx(
          "flex w-full items-start gap-2 rounded-md px-2 py-2 text-left transition",
          canFocus
            ? "cursor-pointer hover:bg-zinc-50"
            : "cursor-default opacity-90",
        )}
      >
        {/* Thumbnail / status indicator (always 40×40 so the layout
            stays calm regardless of progress state). */}
        <div className="relative h-10 w-10 shrink-0 overflow-hidden rounded border border-zinc-200 bg-zinc-50">
          {entry.status === "done" && entry.thumbnailUrl ? (
            <img
              src={entry.thumbnailUrl}
              alt=""
              className="h-full w-full object-cover"
            />
          ) : entry.status === "error" ? (
            <div className="flex h-full w-full items-center justify-center text-red-500">
              <AlertTriangle size={14} />
            </div>
          ) : (
            <div className="flex h-full w-full items-center justify-center text-zinc-400">
              <Loader2 size={14} className="animate-spin" />
            </div>
          )}
        </div>

        <div className="min-w-0 flex-1">
          <p
            className={clsx(
              "line-clamp-2 text-[12px] leading-snug",
              entry.status === "error" ? "text-red-700" : "text-zinc-800",
            )}
          >
            {entry.prompt}
          </p>
          <p className="mt-0.5 font-mono text-[10px] uppercase tracking-[0.14em] text-zinc-400">
            {labelForStatus(entry)}
          </p>
        </div>
      </button>
    </li>
  );
}

function labelForStatus(entry: HistoryEntry): string {
  if (entry.status === "running") return "generating…";
  if (entry.status === "error") return entry.errorMessage ?? "error";
  return new Date(entry.createdAt).toLocaleTimeString();
}
