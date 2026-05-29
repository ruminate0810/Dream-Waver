-- Sprint AA.1 — chat event log.
--
-- Currently the WS Hub fan-outs every event to subscribed clients but
-- keeps no history. Reload /slides/[id] mid-deck = chat thread is
-- empty (Turn[] is React in-memory state). WS reconnects after a drop
-- = miss every event during the gap. Both are user-facing pain.
--
-- Solution: append-only events table, per-session monotonic seq. The
-- WS Hub mirrors each emit to PG (best-effort; failure does NOT block
-- live broadcast). The FE pulls the log on /slides/[id] mount before
-- subscribing to live WS, and tracks a cursor so reconnects can
-- back-fill the gap.
--
-- Sized for: ~200 events per deck × ~10 decks/user/day → ~2k events
-- per user per day. Even 100k users = ~200M rows over 90 days, well
-- within Postgres comfort zone with the (session_id, seq) BTREE.
-- Post-MVP we'll add a TTL trigger to drop rows older than 30 days.

create table if not exists chat_events (
  session_id  uuid       not null,
  seq         bigint     not null,
  kind        text       not null,
  data        jsonb      not null,
  at          timestamptz not null default now(),
  primary key (session_id, seq)
);

-- The lookups we make:
--   1. ListSince(session_id, since seq) → ORDER BY seq → covered by PK
--   2. (Eventually) cleanup by `at` < cutoff → needs `at` index
create index if not exists chat_events_at_idx on chat_events (at);
