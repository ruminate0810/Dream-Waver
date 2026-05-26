"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ImageOff, X } from "lucide-react";

import { forgetDeck, listRecentDecks, type RecentDeck } from "@/lib/recentDecks";

// RecentDecks — client-only list of the user's most recent decks,
// read from localStorage. Renders nothing on the server (Next.js
// hydration) and nothing when there are no decks to show, so the
// homepage stays clean for first-time visitors.
//
// Design: one row per deck — small 16:9 thumb of slide 1 (gracefully
// fails to a neutral block if the PNG isn't ready yet), title +
// theme + relative time, click anywhere on the card to re-open the
// /slides/[id] workspace. A small × button forgets the deck from
// the local list (server-side state is untouched).

export function RecentDecks() {
  const [decks, setDecks] = useState<RecentDeck[] | null>(null);

  useEffect(() => {
    const initial = listRecentDecks();
    setDecks(initial);
    // Also refresh when the page is shown after a back-nav (Safari
    // / Chrome bfcache) so a deck the user just generated appears.
    const onShow = () => setDecks(listRecentDecks());
    window.addEventListener("pageshow", onShow);

    // Background self-clean — probe each remembered deck with a
    // GET /slides/{id}. Anything 404 means the backend forgot it
    // (typically: orchestrator restart with in-memory store), so we
    // prune the localStorage entry. Done in parallel; failures
    // silently drop. We don't await — render the cached list
    // immediately; pruned entries vanish on the next state update.
    initial.forEach(async (d) => {
      try {
        const res = await fetch(`/api/v1/slides/${d.jobId}`, { method: "GET" });
        if (res.status === 404) {
          forgetDeck(d.jobId);
          setDecks((prev) => (prev ?? []).filter((x) => x.jobId !== d.jobId));
        }
      } catch {
        // Network error — leave the entry alone; the user can × it
        // manually if needed.
      }
    });

    return () => window.removeEventListener("pageshow", onShow);
  }, []);

  // SSR + zero-deck case → render nothing. The homepage's other
  // sections cover those flows.
  if (decks === null || decks.length === 0) return null;

  return (
    <section className="px-10 py-10">
      <div className="mb-5 flex items-baseline justify-between border-b border-zinc-200 pb-3">
        <h2 className="font-display text-[22px] tracking-tight text-zinc-900">
          最近的 deck <span className="ml-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-zinc-400">· Recent</span>
        </h2>
        <p className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-zinc-400">
          {decks.length} on this device
        </p>
      </div>

      <ul className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {decks.map((d) => (
          <DeckRow
            key={d.jobId}
            deck={d}
            onForget={() => setDecks(listRecentDecks().filter((x) => x.jobId !== d.jobId))}
          />
        ))}
      </ul>
    </section>
  );
}

function DeckRow({ deck, onForget }: { deck: RecentDeck; onForget: () => void }) {
  const [thumbBroken, setThumbBroken] = useState(false);
  const href = `/slides/${deck.jobId}?session=${deck.sessionId}`;
  const display = deck.title?.trim() || deck.topic || "Untitled deck";

  return (
    <li className="group relative">
      <Link
        href={href}
        className="flex items-stretch gap-3 border border-zinc-200 bg-white p-2.5 transition-all duration-200 hover:-translate-y-[1px] hover:border-zinc-400 hover:shadow-[0_18px_36px_-22px_rgba(0,0,0,0.15)]"
      >
        <div className="relative aspect-[16/9] w-[140px] shrink-0 overflow-hidden border border-zinc-100 bg-zinc-50">
          {thumbBroken ? (
            // Either the deck is still rendering (slide 1 PNG not
            // written yet) OR the backend forgot it (in-memory store
            // restart). The auto-prune effect upstairs handles the
            // forgotten case; this placeholder covers both with a
            // neutral hairline icon + no spinner (a spinner here was
            // misleading — looked like loading when really it's gone).
            <div className="flex h-full w-full flex-col items-center justify-center gap-1 text-zinc-300">
              <ImageOff size={14} strokeWidth={1.4} />
              <span className="font-mono-jb text-[8px] uppercase tracking-[0.22em]">
                no preview
              </span>
            </div>
          ) : (
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src={deck.thumbUrl}
              alt=""
              loading="lazy"
              onError={() => setThumbBroken(true)}
              className="h-full w-full object-cover"
            />
          )}
        </div>
        <div className="flex min-w-0 flex-1 flex-col justify-center gap-1.5">
          <p className="truncate font-display text-[15px] leading-tight text-zinc-900">
            {display}
          </p>
          <div className="flex items-baseline gap-2 font-mono-jb text-[9px] uppercase tracking-[0.22em] text-zinc-400">
            <span>{deck.theme}</span>
            <span>·</span>
            <span>{relativeTime(deck.createdAt)}</span>
          </div>
        </div>
      </Link>
      <button
        type="button"
        onClick={(e) => {
          e.preventDefault();
          forgetDeck(deck.jobId);
          onForget();
        }}
        aria-label="Forget this deck"
        className="absolute right-2 top-2 inline-flex h-5 w-5 items-center justify-center rounded-sm bg-white/0 text-zinc-300 opacity-0 transition-all hover:bg-zinc-100 hover:text-zinc-700 group-hover:opacity-100"
      >
        <X size={11} strokeWidth={1.8} />
      </button>
    </li>
  );
}

// Lightweight relative-time formatter — avoids pulling in date-fns
// just for this one surface. Sub-minute resolution isn't useful
// here; users care about "just now" / "5 min ago" / "yesterday".
function relativeTime(ts: number): string {
  const diff = Date.now() - ts;
  const s = Math.floor(diff / 1000);
  if (s < 60) return "just now";
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} hr ago`;
  const d = Math.floor(h / 24);
  if (d < 7) return `${d} day${d > 1 ? "s" : ""} ago`;
  return new Date(ts).toLocaleDateString();
}
