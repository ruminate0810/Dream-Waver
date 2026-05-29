-- Sprint BB — design memory (ChatGPT-style cross-session memory).
--
-- Where design_sessions (0006) is per-thread conversation history,
-- design_memory is the WORKSPACE-level persistent facts the assistant
-- remembers ACROSS all sessions: style preferences, brand constraints,
-- recurring subjects ("the mascot is a fox named Pip", "brand color is
-- #FF6B6B", "always 16:9 for social"). Injected into every generation
-- + the intent router so the assistant stays consistent without the
-- user re-stating preferences each session.
--
-- Model: a mem0-style extract → consolidate pipeline writes these
-- (source='auto'), and the user can pin facts manually (source='manual',
-- never auto-deleted). At our scale (dozens of facts per workspace) we
-- inject ALL of them rather than doing vector top-k retrieval — so no
-- embedding column. content is a single short fact per row.

create table if not exists design_memory (
  id            uuid primary key,
  workspace_id  uuid not null references workspaces(id) on delete cascade,
  content       text not null,
  -- 'auto'   → written by the extract/consolidate LLM pass; the pass
  --            may UPDATE or DELETE these as the conversation evolves.
  -- 'manual' → the user explicitly asked to remember this; the auto
  --            pass leaves manual rows untouched (never auto-deletes).
  source        text not null default 'auto' check (source in ('auto', 'manual')),
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now()
);

-- We always list a workspace's memory newest-first for the panel +
-- pull the full set for prompt injection.
create index if not exists design_memory_ws_idx
  on design_memory (workspace_id, updated_at desc);
