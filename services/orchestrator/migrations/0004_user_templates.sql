-- Sprint T2 — user-saved templates. Lets users save a preset
-- (theme + brand + font) and re-apply it from /slides/new's
-- "我的模板" tab.
--
-- Scoped to workspace_id (not user_id) so all members of a team
-- workspace can pick from the same template pool. RLS gates access to
-- the workspace_members set; the orchestrator's service-role
-- connection bypasses RLS and the store layer enforces in app code.

create table if not exists user_templates (
  id              uuid primary key default uuid_generate_v4(),
  workspace_id    uuid not null references workspaces(id) on delete cascade,
  created_by      uuid references users(id) on delete set null,
  -- User-facing label. Free text, ≤120 chars (the store layer
  -- trims + enforces). Not unique — a user can have two "公司主调"
  -- presets if they want.
  name            text not null,
  -- Base theme key, must match a schema.Theme enum value
  -- (minimalist / corporate / pitch-deck / academic / playful /
  --  editorial / retro / tech / zen / warm / noir). Validated by
  -- the store layer + routes handler.
  theme           text not null,
  -- Brand overlay, all optional. Hex strings; the orchestrator's
  -- apply_brand tool consumes the same shape.
  brand_primary   text,
  brand_accent    text,
  font_family     text,
  created_at      timestamptz not null default now()
);

create index if not exists user_templates_workspace_idx
  on user_templates (workspace_id, created_at desc);

-- ─── RLS — workspace members can read/write their workspace's
-- ─── templates. Same shape as slide_jobs / game_jobs policies in
-- ─── 0001_init.sql.

alter table user_templates enable row level security;

drop policy if exists ut_members_all on user_templates;
create policy ut_members_all on user_templates
  for all
  using (public.is_workspace_member(workspace_id))
  with check (public.is_workspace_member(workspace_id));
