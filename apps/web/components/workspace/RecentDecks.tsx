"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ImageOff, X } from "lucide-react";

import { forgetDeck, listRecentDecks, type RecentDeck } from "@/lib/recentDecks";

// RecentDecks — client-only list of the user's most recent decks from
// localStorage, pixel re-skin. Renders nothing on the server / when empty.
// The 404 self-prune + ScrollTrigger reveal are unchanged.

export function RecentDecks() {
  const [decks, setDecks] = useState<RecentDeck[] | null>(null);

  // Entrance is CSS-driven (.dw-recent-card in globals.css) so cards can't
  // strand at opacity:0 under React Strict Mode's dev double-mount.
  useEffect(() => {
    const initial = listRecentDecks();
    setDecks(initial);
    const onShow = () => setDecks(listRecentDecks());
    window.addEventListener("pageshow", onShow);

    initial.forEach(async (d) => {
      try {
        const res = await fetch(`/api/v1/slides/${d.jobId}`, { method: "GET" });
        if (res.status === 404) {
          forgetDeck(d.jobId);
          setDecks((prev) => (prev ?? []).filter((x) => x.jobId !== d.jobId));
        }
      } catch {
        /* network error — leave it; user can × manually */
      }
    });

    return () => window.removeEventListener("pageshow", onShow);
  }, []);

  if (decks === null || decks.length === 0) return null;

  return (
    <section className="px-10 py-10">
      <div className="mb-5 flex items-baseline justify-between border-b-2 border-ink pb-3">
        <h2 className="font-mono text-[20px] font-extrabold tracking-tight text-ink">
          最近的 deck
          <span className="ml-2 font-pixel text-[0.55rem] tracking-wide text-muted">· RECENT</span>
        </h2>
        <p className="font-pixel text-[0.55rem] tracking-wide text-muted">
          {decks.length} ON THIS DEVICE
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
    <li className="dw-recent-card group relative">
      <Link
        href={href}
        className="flex items-stretch gap-3 rounded-pixel border-2 border-ink bg-surface p-2.5 shadow-pixel-sm transition-transform duration-150 hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-pixel"
      >
        <div className="relative aspect-[16/9] w-[140px] shrink-0 overflow-hidden rounded-[3px] border-2 border-ink bg-surface-2">
          {thumbBroken ? (
            <div className="flex h-full w-full flex-col items-center justify-center gap-1 text-line-2">
              <ImageOff size={14} strokeWidth={1.6} />
              <span className="font-pixel text-[0.45rem] tracking-wide">no preview</span>
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
          <p className="truncate font-mono text-[15px] font-semibold leading-tight text-ink">
            {display}
          </p>
          <div className="flex items-baseline gap-2 font-pixel text-[0.5rem] tracking-wide text-muted">
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
        className="absolute right-2 top-2 inline-flex h-5 w-5 items-center justify-center rounded-[3px] border-2 border-transparent text-line-2 opacity-0 transition-all hover:border-ink hover:bg-surface hover:text-ink group-hover:opacity-100"
      >
        <X size={11} strokeWidth={2.2} />
      </button>
    </li>
  );
}

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
