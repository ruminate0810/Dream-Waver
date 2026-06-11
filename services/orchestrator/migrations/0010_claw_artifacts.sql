-- 0010_claw_artifacts — Claw v2 work package. A run now delivers not just a
-- markdown report (the existing `artifact` column) but also generated
-- figures (designer worker) and, later, a deck. Figures ride in their own
-- jsonb column. Idempotent: add-column-if-not-exists.

alter table claw_runs add column if not exists figures jsonb;
alter table claw_runs add column if not exists deck jsonb;
