// recentDecks — client-side persistence of the user's most recent
// deck sessions so closing the tab doesn't lose the URL.
//
// Why localStorage: there's no auth + DB layer yet. This is a pure
// client list; if the user clears their browser data it goes with it.
// When SaaS lands and we have real session persistence, this hook
// stays — it'll just stop being the *only* source of truth and
// becomes a write-through cache of the server list.
//
// SSR safety: every function early-returns the noop when `window` is
// undefined. Next.js renders pages on the server; we hydrate the
// real list client-side via useEffect.

export type RecentDeck = {
  jobId: string;
  sessionId: string;
  title: string;        // deck title once finished; falls back to topic snippet
  topic: string;        // user's original prompt (truncated to ~80 chars)
  theme: string;        // force_theme used at submit time
  createdAt: number;    // Date.now() at first push
  thumbUrl: string;     // /api/v1/slides/{id}/page/1.png — may 404 if job errored
};

const KEY = "dw_recent_decks_v1";
const MAX = 10; // most recent N — keeps the list scannable on /

function readAll(): RecentDeck[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    // Defensive — old shapes get dropped silently instead of crashing.
    return parsed.filter(
      (r): r is RecentDeck =>
        r &&
        typeof r.jobId === "string" &&
        typeof r.sessionId === "string" &&
        typeof r.createdAt === "number",
    );
  } catch {
    return [];
  }
}

function writeAll(decks: RecentDeck[]): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(KEY, JSON.stringify(decks.slice(0, MAX)));
  } catch {
    // Quota / private mode — drop silently. The user just won't see
    // the deck in the list; the deck itself still exists server-side.
  }
}

/** Insert (or move to top) one deck. Caller is `/slides/new` immediately
 *  after `createSlides()` returns — at that moment we only know the
 *  topic + theme + ids; title comes later via `update()`. */
export function rememberDeck(initial: Omit<RecentDeck, "createdAt" | "title" | "thumbUrl"> & { title?: string }): void {
  const all = readAll();
  const without = all.filter((r) => r.jobId !== initial.jobId);
  const next: RecentDeck = {
    ...initial,
    title: initial.title ?? "",
    createdAt: Date.now(),
    thumbUrl: `/api/v1/slides/${initial.jobId}/page/1.png`,
  };
  writeAll([next, ...without]);
}

/** Patch an existing entry's title (called from /slides/{id} when the
 *  job finishes and we learn what the planner actually titled it). */
export function updateDeckTitle(jobId: string, title: string): void {
  if (!title) return;
  const all = readAll();
  const idx = all.findIndex((r) => r.jobId === jobId);
  if (idx < 0) return;
  if (all[idx].title === title) return;
  all[idx] = { ...all[idx], title };
  writeAll(all);
}

/** Drop one deck from the list (e.g. user clicked "forget"). */
export function forgetDeck(jobId: string): void {
  writeAll(readAll().filter((r) => r.jobId !== jobId));
}

/** Read the full recent list. SSR returns []; hydrate in a useEffect. */
export function listRecentDecks(): RecentDeck[] {
  return readAll();
}
