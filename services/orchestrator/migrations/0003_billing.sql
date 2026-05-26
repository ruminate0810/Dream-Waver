-- Sprint X3a — Phase 3: credit ledger + tool-call audit.
--
-- credit_ledger is append-only. Positive amount = grant (trial,
-- top-up, refund); negative amount = debit (tool call). Balance is
-- always SUM(amount_micro) WHERE workspace_id = $1 — never stored
-- as a column to avoid lost-update bugs across concurrent writers.
--
-- tool_calls is the per-invocation audit row. Written alongside
-- every Debit (and ALSO when a debit was skipped, e.g. tool ran
-- through a fallback). Disputed bills get reconstructed by joining
-- credit_ledger entries to their matching tool_calls row via the
-- ledger row's meta.tool_call_id (set by billing.Service).
--
-- Why no materialised balance: a personal-workspace ledger maxes
-- out around 1000 rows / month; SUM is O(N) but N stays tiny.
-- Team workspaces are higher volume but still within "milliseconds
-- per query" for the foreseeable future. When that stops being
-- true, add a `workspace_balances` materialised view refreshed
-- after each insert via a trigger.

-- ─── credit_ledger ──────────────────────────────────────────────────

create table if not exists credit_ledger (
  id            uuid primary key default uuid_generate_v4(),
  workspace_id  uuid not null references workspaces(id) on delete cascade,
  -- micro-units: 1 USD = 1_000_000. bigint chosen so a workspace
  -- can accumulate up to ~$9.2T of credit without overflow.
  amount_micro  bigint not null,
  -- Closed set of reasons; widening it requires a code change so
  -- audit consumers know every possible value.
  reason        text not null check (reason in (
                  'trial_grant',     -- $1 free credit on first login
                  'manual_topup',    -- admin-granted (future)
                  'tool_call',       -- standard debit
                  'refund',          -- explicit refund after support intervention
                  'adjustment'       -- catch-all for one-off corrections
                )),
  -- Free-form per-reason metadata (tool_name, args_hash, task_id,
  -- support_ticket_id, etc.). Indexed via partial GIN below.
  meta          jsonb,
  created_at    timestamptz not null default now()
);

create index if not exists credit_ledger_workspace_idx
  on credit_ledger (workspace_id, created_at desc);

-- Partial GIN over meta — only the rows we actually search by meta
-- (tool_call refunds need to find the original debit). Keeps the
-- index small.
create index if not exists credit_ledger_meta_tool_idx
  on credit_ledger using gin (meta jsonb_path_ops)
  where reason = 'tool_call' or reason = 'refund';

-- ─── tool_calls (audit) ─────────────────────────────────────────────

create table if not exists tool_calls (
  id                  uuid primary key default uuid_generate_v4(),
  workspace_id        uuid not null references workspaces(id) on delete cascade,
  -- user_id is nullable because some background flows (Opendream
  -- timeline refresh, scheduled re-runs) don't have a user.
  user_id             uuid references users(id) on delete set null,
  tool_name           text not null,
  -- Matches the args_hash format used by idempotency_keys so the
  -- two surfaces can be joined when debugging "why didn't this
  -- retry hit the cache".
  args_hash           bytea,
  result_summary      text,
  -- Debit amount in micro-units. 0 for unmetered tools (terminate,
  -- plan_outline, etc.) and for fallback paths that skipped billing.
  debit_amount_micro  bigint not null default 0,
  duration_ms         int not null default 0,
  attempt             int not null default 1,
  -- Name of the fallback tool that actually produced the result,
  -- when the primary failed and the Resilient wrapper fell through.
  -- Empty string when the primary succeeded.
  fallback_used       text not null default '',
  error               text not null default '',
  created_at          timestamptz not null default now()
);

create index if not exists tool_calls_workspace_idx
  on tool_calls (workspace_id, created_at desc);

-- Per-tool slice — used by "what's our DreamAPI spend this week"
-- analytics queries.
create index if not exists tool_calls_tool_idx
  on tool_calls (tool_name, created_at desc);

-- ─── RLS ────────────────────────────────────────────────────────────

alter table credit_ledger enable row level security;
alter table tool_calls    enable row level security;

drop policy if exists cl_members_read on credit_ledger;
create policy cl_members_read on credit_ledger
  for select
  using (public.is_workspace_member(workspace_id));

-- Only the service role writes the ledger — no DML policy means RLS
-- blocks all non-service writes by default. Documented here so a
-- future "let users top up their own balance via a Stripe webhook
-- → INSERT" doesn't accidentally try to use a member-scoped role.

drop policy if exists tc_members_read on tool_calls;
create policy tc_members_read on tool_calls
  for select
  using (public.is_workspace_member(workspace_id));
