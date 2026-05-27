#!/usr/bin/env bash
# Sprint Y — Supabase setup helper.
#
# One-shot script you run AFTER filling in your .env with:
#   - DATABASE_URL          (pooler URL, port 6543, sslmode=require)
#   - SUPABASE_URL          (https://<project-ref>.supabase.co)
#   - SUPABASE_JWKS_URL     (auto-derived from SUPABASE_URL if unset)
#   - SUPABASE_ANON_KEY     (publishable / anon key, sb_publishable_*)
#   - SUPABASE_SERVICE_ROLE_KEY  (server-side only; never expose to FE)
#
# What it does:
#   1. Sources .env and validates the required vars exist
#   2. Runs psql connectivity check (catches sslmode / password typos)
#   3. Applies migrations/0001..0004 in order against your Supabase DB
#   4. Verifies the JWKS endpoint is reachable
#   5. Prints a final "ready" checklist
#
# It does NOT touch any secrets — everything stays local. .env is
# gitignored; this script just reads it.

set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

# ─── Tooling discovery ───────────────────────────────────────────────
# Postgres.app on macOS, fall back to system psql.
PSQL_BIN=""
for c in /Applications/Postgres.app/Contents/Versions/*/bin/psql /usr/local/bin/psql /opt/homebrew/bin/psql; do
  if [ -x "$c" ]; then PSQL_BIN="$c"; break; fi
done
if [ -z "$PSQL_BIN" ]; then
  if command -v psql >/dev/null 2>&1; then
    PSQL_BIN="$(command -v psql)"
  else
    echo "ERROR: psql not found. Install Postgres.app or 'brew install libpq'." >&2
    exit 1
  fi
fi
echo "✓ psql: $PSQL_BIN"

# ─── Env load ────────────────────────────────────────────────────────
if [ ! -f "$ROOT/.env" ]; then
  echo "ERROR: $ROOT/.env not found. Copy .env.example to .env first." >&2
  exit 1
fi

set -a
# shellcheck source=/dev/null
source "$ROOT/.env"
set +a

REQUIRED=(DATABASE_URL SUPABASE_URL)
for v in "${REQUIRED[@]}"; do
  if [ -z "${!v:-}" ]; then
    echo "ERROR: $v is not set in .env" >&2
    exit 1
  fi
done

# Auto-derive JWKS URL from SUPABASE_URL if not explicitly set.
if [ -z "${SUPABASE_JWKS_URL:-}" ]; then
  SUPABASE_JWKS_URL="${SUPABASE_URL%/}/auth/v1/.well-known/jwks.json"
  echo "ℹ  SUPABASE_JWKS_URL not set — derived: $SUPABASE_JWKS_URL"
fi

# Sanity: pooler URL on 6543 is what we recommend; direct conn on 5432
# works too but has lower concurrency headroom. Warn if neither.
case "$DATABASE_URL" in
  *":6543/"* ) echo "✓ DATABASE_URL uses Supavisor pooler (port 6543)";;
  *":5432/"* ) echo "ℹ  DATABASE_URL uses direct conn (port 5432). Pooler (6543) recommended for production.";;
  * )          echo "⚠  DATABASE_URL doesn't look like Supabase (no :5432/ or :6543/ in it). Continuing anyway.";;
esac
case "$DATABASE_URL" in
  *"sslmode=require"* | *"sslmode=verify-full"* )
    echo "✓ DATABASE_URL has SSL mode";;
  * )
    echo "⚠  DATABASE_URL missing sslmode=require — Supabase requires TLS. Add ?sslmode=require to the URL.";;
esac

# ─── Connectivity check ──────────────────────────────────────────────
echo ""
echo "→ Connectivity check..."
if ! "$PSQL_BIN" "$DATABASE_URL" -c "select version();" >/dev/null 2>&1; then
  echo "ERROR: cannot connect to DATABASE_URL. Check the password and that your IP is allowed in Supabase dashboard → Database → Network restrictions." >&2
  exit 1
fi
echo "✓ Connected"

# ─── JWKS reachability ──────────────────────────────────────────────
echo ""
echo "→ JWKS reachability check..."
if ! curl -fsSL --max-time 5 "$SUPABASE_JWKS_URL" -o /dev/null; then
  echo "ERROR: cannot fetch $SUPABASE_JWKS_URL. Make sure SUPABASE_URL is correct." >&2
  exit 1
fi
echo "✓ JWKS endpoint reachable"

# ─── Apply migrations ────────────────────────────────────────────────
echo ""
echo "→ Applying migrations..."
for m in "$ROOT/services/orchestrator/migrations"/*.sql; do
  name="$(basename "$m")"
  echo "  · $name"
  if ! "$PSQL_BIN" "$DATABASE_URL" \
       -v ON_ERROR_STOP=1 -X -q \
       -f "$m" >/tmp/dw-migrate-$$.log 2>&1; then
    echo "ERROR applying $name. Last 20 lines:" >&2
    tail -20 /tmp/dw-migrate-$$.log >&2
    rm -f /tmp/dw-migrate-$$.log
    exit 1
  fi
done
rm -f /tmp/dw-migrate-$$.log
echo "✓ All 4 migrations applied"

# ─── Schema sanity ───────────────────────────────────────────────────
echo ""
echo "→ Schema sanity check (table counts)..."
"$PSQL_BIN" "$DATABASE_URL" -At -c "
  select count(*) || ' tables in public:' from pg_tables where schemaname='public';
  select '  - ' || tablename from pg_tables where schemaname='public' order by tablename;
"

# ─── Ready ───────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════════════════"
echo "  Supabase is wired. Next steps:"
echo "    1. Restart orchestrator so it picks up SUPABASE_JWKS_URL"
echo "       pkill -f /tmp/dw-orchestrator"
echo "       (and re-launch with the usual nohup line)"
echo "    2. Visit http://localhost:3001/login and sign up / sign in"
echo "    3. Create a deck — should now run under your real Supabase user"
echo "════════════════════════════════════════════════════════════════"
