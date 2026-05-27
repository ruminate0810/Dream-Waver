-- Sprint X2a — Phase 2: idempotency keys for tool calls.
--
-- Purpose: when the agent loop retries a failed tool call (or when a
-- client double-submits a request after a network hiccup), we want the
-- second invocation to return the cached result instead of re-running
-- the underlying side effect (DreamAPI generation, Opendream run,
-- sandbox execution — all of which cost money).
--
-- Key shape: (workspace_id, tool_name, args_hash). args_hash is a
-- bytea (SHA-256 over canonicalised JSON args — see internal/tool/
-- idempotency.go for the canonicalisation rules). 24h TTL — long
-- enough for a retry loop to find it, short enough that storage
-- doesn't grow unbounded.
--
-- We do NOT delete expired rows on a schedule; the application skips
-- rows past their ttl. A periodic GC sweep is a Phase 5 cleanup item.

create table if not exists idempotency_keys (
  workspace_id  uuid not null references workspaces(id) on delete cascade,
  tool_name     text not null,
  args_hash     bytea not null,
  -- Stored as jsonb so the caller can stream the cached value
  -- straight back without re-serialising.
  result_json   jsonb not null,
  -- ttl is the absolute expiry timestamp, not a duration — easier to
  -- query (`where ttl > now()`) and less ambiguous on rehydration.
  ttl           timestamptz not null,
  created_at    timestamptz not null default now(),

  primary key (workspace_id, tool_name, args_hash)
);

-- Sweep helper index — lets the future GC job find expired rows
-- without a full table scan. Originally a partial index
-- (`where ttl < now()`) for storage efficiency, but Postgres 18
-- tightened the rule on index predicates: functions used in them
-- must be IMMUTABLE, and `now()` is STABLE. A full-column btree
-- still serves the same `where ttl < now()` GC query via range
-- scan; we just pay slightly more disk for the universe of rows
-- instead of the expired-only subset.
create index if not exists idempotency_keys_ttl_idx
  on idempotency_keys (ttl);

alter table idempotency_keys enable row level security;

-- RLS: the same workspace-membership policy other job tables use.
drop policy if exists ik_members_all on idempotency_keys;
create policy ik_members_all on idempotency_keys
  for all
  using (public.is_workspace_member(workspace_id))
  with check (public.is_workspace_member(workspace_id));
