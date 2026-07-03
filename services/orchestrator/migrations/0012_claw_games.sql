-- V26 批二: claw work packages can include playable mini-games (references
-- into the games vertical: play_url + title + description; the HTML itself
-- lives with the games session/job).
ALTER TABLE claw_runs ADD COLUMN IF NOT EXISTS games jsonb;
