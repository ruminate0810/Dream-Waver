"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ChevronDown, FileText, Plus } from "lucide-react";
import clsx from "clsx";

import { listRecentDecks, type RecentDeck } from "@/lib/recentDecks";

// Sprint AF.1-v3 — Session switcher as a pull-down popover anchored
// to a top-bar button. Replaces the AF.1 left-rail timeline (timeline
// squeezed chat + preview width too much). User clicks button →
// dropdown panel slides down with recent decks list. Click outside /
// Esc dismisses. Clicking a deck navigates via Next router.
//
// Recent decks come from lib/recentDecks (localStorage). Current
// deck is pinned at top with vermillion accent; others below.

export function SessionSwitcherPopover({
  currentJobId,
  currentTitle,
}: {
  currentJobId: string;
  currentTitle?: string;
}) {
  const [open, setOpen] = useState(false);
  const [decks, setDecks] = useState<RecentDeck[]>([]);
  const wrapperRef = useRef<HTMLDivElement | null>(null);

  // Hydrate localStorage client-side. Re-read whenever the popover
  // re-opens so a freshly-created deck in another tab shows up.
  useEffect(() => {
    if (!open) return;
    setDecks(listRecentDecks());
  }, [open]);

  // Click-outside + Esc dismiss.
  useEffect(() => {
    if (!open) return;
    const onClickOutside = (e: MouseEvent) => {
      if (!wrapperRef.current) return;
      if (!wrapperRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onClickOutside);
    document.addEventListener("keydown", onEsc);
    return () => {
      document.removeEventListener("mousedown", onClickOutside);
      document.removeEventListener("keydown", onEsc);
    };
  }, [open]);

  const currentLabel = (currentTitle || "当前卷宗").slice(0, 24);
  const otherCount = decks.filter((d) => d.jobId !== currentJobId).length;

  return (
    <div ref={wrapperRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={clsx(
          "group inline-flex items-center gap-2 rounded-pixel border-2 px-2.5 py-1 transition-all",
          open
            ? "border-ink bg-accent-soft shadow-pixel-sm"
            : "border-line-2 bg-surface text-ink-2 hover:border-ink",
        )}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <FileText
          size={11}
          strokeWidth={1.8}
          className="text-ink-2"
        />
        <span className="font-pixel text-[0.55rem] tracking-wide text-ink-2">
          Sessions
        </span>
        {otherCount > 0 ? (
          <span className="font-pixel text-[0.5rem] tracking-wide text-muted">
            · {decks.length}
          </span>
        ) : null}
        <ChevronDown
          size={11}
          strokeWidth={1.8}
          className={clsx(
            "text-muted transition-transform",
            open && "rotate-180",
          )}
        />
      </button>

      {open ? (
        <div
          role="listbox"
          className="animate-rise absolute right-0 top-[calc(100%+8px)] z-30 w-[340px] origin-top-right overflow-hidden rounded-pixel border-2 border-ink bg-surface shadow-pixel"
        >
          {/* Accent bleed strip — registration mark, same as gates */}
          <div className="h-[3px] w-[42px] bg-accent" />

          <div className="border-b border-line px-4 pt-3 pb-3">
            <p className="font-pixel text-[0.55rem] tracking-wide text-accent">
              Vol. 01 · <span className="font-mono text-[10px] font-semibold">当前</span>
            </p>
            <p className="mt-1 truncate font-mono text-[15px] font-bold tracking-tight text-ink">
              {currentLabel}
            </p>
          </div>

          <ul className="max-h-[360px] overflow-y-auto py-1 [scrollbar-width:thin]">
            {decks
              .filter((d) => d.jobId !== currentJobId)
              .slice(0, 12)
              .map((d) => (
                <SessionRow
                  key={d.jobId}
                  deck={d}
                  onPick={() => setOpen(false)}
                />
              ))}

            {otherCount === 0 ? (
              <li className="px-4 py-3 font-mono text-[13px] text-muted">
                还没有其他卷宗。
              </li>
            ) : null}
          </ul>

          <Link
            href="/slides/new"
            onClick={() => setOpen(false)}
            className="group flex items-center justify-between border-t border-line px-4 py-3 transition-colors hover:bg-accent-soft"
          >
            <span className="flex items-center gap-2">
              <Plus
                size={11}
                strokeWidth={1.8}
                className="text-accent"
              />
              <span className="font-mono text-[14px] font-semibold text-ink group-hover:text-accent">
                新建卷宗
              </span>
            </span>
            <span className="font-pixel text-[0.5rem] tracking-wide text-muted">
              /slides/new
            </span>
          </Link>
        </div>
      ) : null}
    </div>
  );
}

function SessionRow({
  deck,
  onPick,
}: {
  deck: RecentDeck;
  onPick: () => void;
}) {
  const ago = formatAgo(deck.createdAt);
  const title = (deck.title || deck.topic || "Untitled").slice(0, 42);
  const topic =
    deck.title && deck.topic ? deck.topic.slice(0, 50) : "";

  return (
    <li>
      <Link
        href={`/slides/${deck.jobId}?session=${deck.sessionId}`}
        onClick={onPick}
        className="group flex items-baseline gap-2 px-4 py-2 transition-colors hover:bg-accent-soft"
      >
        <div className="min-w-0 flex-1">
          <p className="truncate font-mono text-[14px] font-semibold leading-tight text-ink group-hover:text-accent">
            {title}
          </p>
          {topic ? (
            <p className="mt-0.5 truncate font-mono text-[11px] text-muted">
              {topic}
            </p>
          ) : null}
        </div>
        <span className="shrink-0 font-mono text-[10px] font-semibold tracking-wide text-muted">
          {ago}
        </span>
      </Link>
    </li>
  );
}

function formatAgo(ts: number): string {
  const diffSec = Math.floor((Date.now() - ts) / 1000);
  if (diffSec < 60) return "刚刚";
  const min = Math.floor(diffSec / 60);
  if (min < 60) return `${min} 分前`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr} 时前`;
  const day = Math.floor(hr / 24);
  if (day < 30) return `${day} 天前`;
  const mon = Math.floor(day / 30);
  return `${mon} 月前`;
}
