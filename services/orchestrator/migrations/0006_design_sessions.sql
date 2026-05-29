-- Sprint BA — design sessions (ChatGPT-style conversation threads).
--
-- /design was a single global canvas + a flat localStorage history.
-- A "session" makes it a named, resumable thread: each carries its own
-- chat history (the list of generations) and a thumbnail of the latest
-- result. Sessions are workspace-scoped — with the dev-user-id auth
-- path, that's the caller's auto-created personal workspace, so this
-- works per-device today and upgrades to real cross-device sync the
-- moment Supabase login lands (same workspace_id, no schema change).
--
-- Canvas pixels (TLDraw shapes) stay client-side in IndexedDB keyed by
-- the session id (persistenceKey) — only the conversation + metadata
-- live here. A server-side canvas snapshot is a Phase-2 follow-up if
-- cross-device canvas restore is wanted.
--
-- history is stored as a jsonb array of the frontend's HistoryEntry
-- shape ({id, prompt, thumbnailUrl, status, createdAt, skillId, …}).
-- It's small — hosted result URLs + prompt text, not base64 — so an
-- inline jsonb column beats a separate messages table for this
-- discrete-items log. Capped client-side at 60 entries.

create table if not exists design_sessions (
  id            uuid primary key,
  workspace_id  uuid not null references workspaces(id) on delete cascade,
  created_by    uuid references users(id) on delete set null,
  title         text not null default 'Untitled',
  thumbnail_url text,
  history       jsonb not null default '[]'::jsonb,
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now()
);

-- The switcher lists a workspace's sessions newest-updated first.
create index if not exists design_sessions_ws_updated_idx
  on design_sessions (workspace_id, updated_at desc);
