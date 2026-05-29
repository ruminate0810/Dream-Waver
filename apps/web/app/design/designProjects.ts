// Design projects/sessions — adapter over the server-backed design
// sessions API (Sprint BA). A "project" in the UI === a design session
// on the server (a ChatGPT-style thread: named, resumable, with its own
// chat history + thumbnail).
//
// Why an adapter (vs calling lib/api directly from the page): it keeps
// the `DesignProject` shape the ProjectSwitcher already renders, maps
// the server DTO's `title`/`thumbnail_url`/`updated_at` → `name`/
// `thumbnailUrl`/`updatedAt`, and isolates the "which session to reopen
// on reload" pointer (the one thing that stays in localStorage).
//
// Persistence split:
//   - Session metadata + chat history  → SERVER (workspace-scoped via
//     the X-Dev-User-Id personal workspace; survives reload + device).
//   - Active session id                → localStorage (a per-device
//     "last opened" pointer; the session list itself is server truth).
//   - TLDraw canvas pixels             → IndexedDB, keyed by session id
//     (persistenceKey). Local-only in Phase 1.

import {
  createDesignSession,
  deleteDesignSession,
  getDesignSession,
  listDesignSessions,
  updateDesignSession,
  type DesignSessionMeta,
} from "@/lib/api";

const ACTIVE_KEY = "dw.designActiveSession";

/** UI-facing project shape (what ProjectSwitcher renders). */
export type DesignProject = {
  id: string;
  name: string;
  createdAt: number;
  updatedAt: number;
  thumbnailUrl?: string;
};

function fromMeta(m: DesignSessionMeta): DesignProject {
  return {
    id: m.id,
    name: m.title,
    createdAt: Date.parse(m.created_at) || Date.now(),
    updatedAt: Date.parse(m.updated_at) || Date.now(),
    thumbnailUrl: m.thumbnail_url || undefined,
  };
}

// ─── Active-id pointer (localStorage; sync) ───────────────────────────

export function getActiveProjectId(): string | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage.getItem(ACTIVE_KEY);
  } catch {
    return null;
  }
}

export function setActiveProjectId(id: string): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(ACTIVE_KEY, id);
  } catch {
    /* private mode — fine, the server list is the source of truth */
  }
}

// ─── Project CRUD (server-backed; async) ──────────────────────────────

export async function listProjects(): Promise<DesignProject[]> {
  const metas = await listDesignSessions(100);
  return metas.map(fromMeta); // server already sorts updated_at desc
}

export async function createProject(name?: string): Promise<DesignProject> {
  const full = await createDesignSession(name);
  return fromMeta(full);
}

export async function renameProject(id: string, name: string): Promise<void> {
  const trimmed = name.trim();
  if (!trimmed) return;
  await updateDesignSession(id, { title: trimmed });
}

export async function deleteProject(id: string): Promise<void> {
  await deleteDesignSession(id);
}

/** Bump the session's thumbnail (and updated_at server-side) after a
 *  generation completes. Fire-and-forget at the call site. */
export async function touchThumbnail(
  id: string,
  thumbnailUrl: string,
): Promise<void> {
  if (!thumbnailUrl) return;
  await updateDesignSession(id, { thumbnail_url: thumbnailUrl });
}

// ─── Active-project resolution ────────────────────────────────────────

/** Resolve which project to open on mount: the stored active id if it
 *  still exists server-side, else the most-recently-updated project,
 *  else a freshly-created one. Always returns a project so the canvas
 *  has a persistenceKey. */
export async function ensureActiveProject(): Promise<{
  active: DesignProject;
  all: DesignProject[];
}> {
  let all = await listProjects();
  const activeId = getActiveProjectId();
  if (activeId) {
    const found = all.find((p) => p.id === activeId);
    if (found) return { active: found, all };
  }
  if (all.length > 0) {
    setActiveProjectId(all[0].id);
    return { active: all[0], all };
  }
  const fresh = await createProject();
  setActiveProjectId(fresh.id);
  all = [fresh];
  return { active: fresh, all };
}

// ─── Per-project chat history (server) ────────────────────────────────

export async function loadHistory<T = unknown>(
  projectId: string,
): Promise<T[]> {
  try {
    const full = await getDesignSession<T>(projectId);
    return full.history ?? [];
  } catch {
    // Session vanished (deleted elsewhere) — empty history is the safe
    // fallback; the caller will fall back to a fresh project anyway.
    return [];
  }
}

export async function saveHistory(
  projectId: string,
  entries: unknown[],
): Promise<void> {
  // Cap to the most recent 60 — keeps the jsonb column small and the
  // PATCH payload light. Older generations remain on the canvas.
  const capped = entries.slice(0, 60);
  await updateDesignSession(projectId, { history: capped });
}
