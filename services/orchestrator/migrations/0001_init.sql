-- Sprint X1 — Phase 1 foundation: users, workspaces, workspace_members,
-- and one row per vertical (slide / game / video / design). Everything
-- below is multi-tenant from day one — every job row carries a
-- workspace_id, and Row-Level Security (RLS) is the backstop in case a
-- direct DB client (psql, PostgREST) ever sees the data.
--
-- Designed to run on BOTH:
--   * Supabase prod      — uses the project's built-in `auth.uid()`
--                          to gate RLS.
--   * Local Postgres dev — the `auth.uid()` shim below reads from a
--                          per-transaction `app.user_id` GUC that the
--                          orchestrator sets via `SET LOCAL` when it
--                          connects as a non-service role.
--
-- The Go orchestrator typically connects with the service role (which
-- bypasses RLS) and enforces workspace ownership in application code
-- via the store layer. RLS exists as defense in depth.

-- ─── Extensions ─────────────────────────────────────────────────────

create extension if not exists "uuid-ossp";

-- ─── auth.uid() shim for local Postgres ─────────────────────────────
-- Supabase already provides `auth.uid()`; this block is a no-op there
-- because the function already exists. On local Postgres without
-- Supabase Auth we create our own version that reads a GUC the
-- application can set per transaction.

create schema if not exists auth;

do $$
begin
  if not exists (
    select 1 from pg_proc p
    join pg_namespace n on n.oid = p.pronamespace
    where n.nspname = 'auth' and p.proname = 'uid'
  ) then
    create function auth.uid() returns uuid
      language sql stable as $f$
        select nullif(current_setting('app.user_id', true), '')::uuid
      $f$;
  end if;
end$$;

-- ─── Core identity tables ───────────────────────────────────────────

-- users mirrors the essentials of Supabase auth.users. Synced on the
-- first request from a given Supabase JWT sub (the orchestrator's
-- store.Users.UpsertFromJWT does this).
create table if not exists users (
  id          uuid primary key,
  email       text,
  created_at  timestamptz not null default now()
);

create table if not exists workspaces (
  id              uuid primary key default uuid_generate_v4(),
  name            text not null,
  owner_user_id   uuid not null references users(id) on delete cascade,
  -- 'personal' is auto-created on first login; 'team' is user-created
  -- via POST /api/v1/workspaces.
  kind            text not null check (kind in ('personal', 'team')),
  created_at      timestamptz not null default now()
);

create index if not exists workspaces_owner_idx on workspaces (owner_user_id);

create table if not exists workspace_members (
  workspace_id uuid not null references workspaces(id) on delete cascade,
  user_id      uuid not null references users(id) on delete cascade,
  -- Three roles for MVP. Enterprise RBAC (per-resource permissions)
  -- is deferred — Phase 6+ if anyone asks.
  role         text not null check (role in ('owner', 'admin', 'member')),
  created_at   timestamptz not null default now(),
  primary key (workspace_id, user_id)
);

create index if not exists workspace_members_user_idx on workspace_members (user_id);

-- ─── Per-vertical job tables ────────────────────────────────────────
-- Every job table carries workspace_id (required) and created_by
-- (the user who initiated). The Go store package CRUD methods take
-- workspace_id as a mandatory parameter — no convenience overloads
-- that omit it, to keep tenant isolation a compile-time concern.

create table if not exists slide_jobs (
  id            uuid primary key,
  workspace_id  uuid not null references workspaces(id) on delete cascade,
  session_id    uuid not null,
  created_by    uuid references users(id) on delete set null,
  status        text not null,                      -- running | finished | error | awaiting_clarification | awaiting_outline_approval
  mode          text,                               -- agent | pipeline
  input         jsonb,                              -- original CreateSlidesRequest
  title         text,
  slide_count   int,
  pptx_path     text,                               -- on-disk path (will swap for S3 key in a later sprint)
  error         text,
  -- HILT envelope — survives a process restart so resume keeps working.
  pending       jsonb,
  -- Agent memory snapshot, persisted at turn boundaries (NOT every
  -- SetMemory) per Phase 2 plan. Phase 1 just reserves the column.
  memory        jsonb,
  -- Deck + content + outline materialised so live preview can rebuild
  -- without re-running the agent. Phase 2 wires this up.
  deck          jsonb,
  started_at    timestamptz not null default now(),
  finished_at   timestamptz
);

create index if not exists slide_jobs_workspace_idx on slide_jobs (workspace_id, started_at desc);

create table if not exists game_jobs (
  id            uuid primary key,
  workspace_id  uuid not null references workspaces(id) on delete cascade,
  session_id    uuid not null,
  created_by    uuid references users(id) on delete set null,
  status        text not null,
  input         jsonb,
  title         text,
  bytes         int,
  play_url      text,
  -- Multi-file workspaces — index.html + assets. Stored as map[path]content.
  files         jsonb,
  -- Immutable revision history; restore forks the next edit from a
  -- chosen revision.
  revisions     jsonb,
  error         text,
  pending       jsonb,    -- HILT, ride-along in Phase 5d
  memory        jsonb,
  started_at    timestamptz not null default now(),
  finished_at   timestamptz
);

create index if not exists game_jobs_workspace_idx on game_jobs (workspace_id, started_at desc);

create table if not exists video_runs (
  id                 uuid primary key,
  workspace_id       uuid not null references workspaces(id) on delete cascade,
  created_by         uuid references users(id) on delete set null,
  -- The Opendream side allocates its own run_id; we surface that here
  -- so the bridge knows which upstream run to poll.
  opendream_run_id   text not null,
  title              text,
  status             text not null,
  -- Echoed back to UI so the timeline rebuilds without re-fetching
  -- from Opendream on every page load. Lazy refresh on SSE event.
  spec               jsonb,
  started_at         timestamptz not null default now(),
  finished_at        timestamptz
);

create index if not exists video_runs_workspace_idx on video_runs (workspace_id, started_at desc);

create table if not exists design_assets (
  id            uuid primary key,
  workspace_id  uuid not null references workspaces(id) on delete cascade,
  created_by    uuid references users(id) on delete set null,
  -- 'generate' | 'variants' | 'edit' so the canvas history pane can
  -- distinguish initial vs. derived assets.
  kind          text not null check (kind in ('generate', 'variants', 'edit')),
  image_url     text not null,
  width         int,
  height        int,
  task_id       text,                               -- DreamAPI task id for traceability
  metadata      jsonb,                              -- prompt / source_image_url / op (remove_bg/enhance/...)
  created_at    timestamptz not null default now()
);

create index if not exists design_assets_workspace_idx on design_assets (workspace_id, created_at desc);

-- ─── Row-Level Security policies ────────────────────────────────────
-- Backstop only — the Go service connects with the service role and
-- bypasses RLS. Policies protect us from a future PostgREST exposure
-- or a misconfigured direct-DB client.

alter table workspaces        enable row level security;
alter table workspace_members enable row level security;
alter table slide_jobs        enable row level security;
alter table game_jobs         enable row level security;
alter table video_runs        enable row level security;
alter table design_assets     enable row level security;

-- Helper: is the calling user a member of <ws>? Marked SECURITY
-- DEFINER so it can look across workspace_members even when the
-- calling role can't directly read that table.
create or replace function public.is_workspace_member(ws uuid)
  returns boolean
  language sql
  security definer
  stable
  as $$
    select exists (
      select 1
      from workspace_members
      where workspace_id = ws and user_id = auth.uid()
    );
  $$;

revoke all on function public.is_workspace_member(uuid) from public;
grant execute on function public.is_workspace_member(uuid) to public;

-- workspaces: members see their own; owner can write.
drop policy if exists ws_members_read on workspaces;
create policy ws_members_read on workspaces
  for select
  using (public.is_workspace_member(id));

drop policy if exists ws_owner_write on workspaces;
create policy ws_owner_write on workspaces
  for all
  using (owner_user_id = auth.uid())
  with check (owner_user_id = auth.uid());

-- workspace_members: members can see other members of their workspaces.
drop policy if exists wm_members_read on workspace_members;
create policy wm_members_read on workspace_members
  for select
  using (public.is_workspace_member(workspace_id));

-- Owner/admin can add/remove members. For MVP we don't distinguish
-- admin from owner — both have the same DML rights.
drop policy if exists wm_owner_write on workspace_members;
create policy wm_owner_write on workspace_members
  for all
  using (
    workspace_id in (
      select id from workspaces where owner_user_id = auth.uid()
    )
    or
    workspace_id in (
      select workspace_id from workspace_members
      where user_id = auth.uid() and role in ('owner','admin')
    )
  )
  with check (
    workspace_id in (
      select id from workspaces where owner_user_id = auth.uid()
    )
    or
    workspace_id in (
      select workspace_id from workspace_members
      where user_id = auth.uid() and role in ('owner','admin')
    )
  );

-- Each job table follows the same "any member can read+write" rule.
-- More granular policies (e.g. owner-only delete) can layer on later
-- without rewriting the column shape.
drop policy if exists sj_members_all on slide_jobs;
create policy sj_members_all on slide_jobs
  for all
  using (public.is_workspace_member(workspace_id))
  with check (public.is_workspace_member(workspace_id));

drop policy if exists gj_members_all on game_jobs;
create policy gj_members_all on game_jobs
  for all
  using (public.is_workspace_member(workspace_id))
  with check (public.is_workspace_member(workspace_id));

drop policy if exists vr_members_all on video_runs;
create policy vr_members_all on video_runs
  for all
  using (public.is_workspace_member(workspace_id))
  with check (public.is_workspace_member(workspace_id));

drop policy if exists da_members_all on design_assets;
create policy da_members_all on design_assets
  for all
  using (public.is_workspace_member(workspace_id))
  with check (public.is_workspace_member(workspace_id));
