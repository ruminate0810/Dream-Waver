-- 0011_claw_videos — Claw v7 work package gains generated clips. The
-- videographer worker animates a figure into a short video (image-to-video);
-- the clips ride in their own jsonb column alongside figures + deck.
-- Idempotent: add-column-if-not-exists.

alter table claw_runs add column if not exists videos jsonb;
