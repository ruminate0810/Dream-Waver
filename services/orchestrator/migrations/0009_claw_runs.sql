-- 0009_claw_runs — Claw vertical (general AI worker). One row per worker
-- session: a plan (sub-task checklist) + a markdown report artifact that
-- versions on every write_document. Mirrors game_jobs' shape and RLS so
-- the route layer's restart-recovery hydration (copied from games) has a
-- persistence point. Idempotent: create-if-not-exists + drop/create
-- policy, same convention as 0001_init.sql.

create table if not exists claw_runs (
  id               uuid primary key,
  workspace_id     uuid not null references workspaces(id) on delete cascade,
  session_id       uuid not null,
  created_by       uuid references users(id) on delete set null,
  status           text not null,
  prompt           text,
  title            text,
  -- []claw.Task snapshot the frontend checklist renders. Authoritative
  -- plan state so a cold page load can draw the checklist without replay.
  plan             jsonb,
  -- Latest markdown report (full text). Served over GET — never the WS.
  artifact         text,
  artifact_version int not null default 0,
  -- []schema.Message conversation so a follow-up survives a restart.
  memory           jsonb,
  error            text,
  started_at       timestamptz not null default now(),
  finished_at      timestamptz
);

create index if not exists claw_runs_workspace_idx on claw_runs (workspace_id, started_at desc);

alter table claw_runs enable row level security;

drop policy if exists cr_members_all on claw_runs;
create policy cr_members_all on claw_runs
  for all
  using (public.is_workspace_member(workspace_id))
  with check (public.is_workspace_member(workspace_id));
