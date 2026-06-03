-- Sprint BR.3 — reference deck corpus for RAG-style outline planning.
--
-- High-quality "exemplar" decks we feed into the outline planner as
-- inspiration. At plan_outline time we run a tag+keyword overlap
-- query to fetch the top-K matches, then ask the LLM to use their
-- structure & density as a soft reference (NOT to copy headlines).
--
-- This is a GLOBAL corpus — not scoped to any workspace. Every user
-- benefits from the same curated examples. No RLS, no FK to
-- workspaces/users. Seeded via scripts/seed-reference-decks.ts (BR.4).

create table if not exists reference_decks (
  id             uuid primary key default uuid_generate_v4(),
  -- Stable url-safe identifier — used for attribution + dedup. Often
  -- mirrors the topic, e.g. "saas-pitch-codepilot-v1".
  slug           text unique not null,
  -- Which kind of deck this is — should match a blueprints.scenario_tag
  -- value so retrieve can join blueprint→reference quickly. Free text
  -- (no FK to blueprints since blueprints live in code, not DB).
  scenario       text not null,
  -- Which blueprint (if any) was used when generating this exemplar.
  -- Nullable for free-form examples; very useful for retrieval boost
  -- so a series-a-pitch outline planning run prefers references that
  -- ALSO used series-a-pitch.
  blueprint_id   text,
  -- Theme used for the deck (one of the 12 schema.Theme values).
  -- Stored as text — schema lives in Go, not DB.
  theme          text not null,
  -- Topic tags — short keywords useful for retrieval scoring.
  -- e.g. ['SaaS', 'B2B', 'developer-tools', 'AI'].
  topic_tags     text[] not null default '{}',
  -- Human-facing display title — surfaced in the "灵感来自:" line
  -- on the outline review card.
  title          text not null,
  -- Full OutlineResult JSON — what the planner LLM produced when the
  -- exemplar was generated. This is what we inject as inspiration.
  outline_json   jsonb not null,
  -- Optional full ContentResult JSON — kept so we can later upgrade
  -- to content-level reference injection (BR.next). Not used in BR.3.
  content_json   jsonb,
  -- Trace back to the originating job (for self-bootstrapped examples).
  -- Nullable for hand-curated entries.
  source_job_id  uuid,
  -- Human-rated quality 0–5. Retrieval boosts higher-scored rows.
  -- Default 0 means "unscored" → still considered, just no boost.
  quality_score  smallint not null default 0,
  created_at     timestamptz not null default now()
);

-- Per-scenario lookup (most common retrieval shape).
create index if not exists reference_decks_scenario_idx
  on reference_decks (scenario, quality_score desc);

-- Per-blueprint lookup (boosts blueprint-matching references).
create index if not exists reference_decks_blueprint_idx
  on reference_decks (blueprint_id, quality_score desc)
  where blueprint_id is not null;

-- GIN on topic_tags for keyword-overlap queries (intersection /
-- containment). Lets Retrieve filter by tag intersection cheaply.
create index if not exists reference_decks_tags_idx
  on reference_decks using gin (topic_tags);

-- No RLS — global corpus. The orchestrator's service-role connection
-- has full access; this table is intentionally NOT exposed via
-- frontend user fetchers.
