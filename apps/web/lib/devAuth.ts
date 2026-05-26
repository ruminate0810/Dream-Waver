// Sprint T3 — dev-mode user identity for "我的模板" and other
// auth-required surfaces, until real login lands (Sprint J2).
//
// Strategy: every browser gets a stable per-device UUID stored in a
// cookie + localStorage. apiFetch (lib/api.ts) reads it and attaches
// the X-Dev-User-Id header on every request. The orchestrator's
// auth/middleware.go accepts this header WHEN running in dev mode
// (ENV=development AND SUPABASE_JWKS_URL empty) and synthesises a
// user/workspace from it — see services/orchestrator/internal/auth.
//
// Two storage spots so a cookie clear doesn't lose the id (and
// vice-versa). On any read mismatch, localStorage wins and we
// re-sync the cookie.
//
// Production safety: in real auth the orchestrator IGNORES this
// header. The frontend can keep sending it; it just becomes a no-op.

"use client";

const STORAGE_KEY = "dw-dev-user-id";
const COOKIE_NAME = "dw_dev_user_id";
const COOKIE_MAX_AGE_S = 60 * 60 * 24 * 365; // 1 year

// uuid v4 — RFC 4122 compliant. We bundle a tiny generator rather than
// pull in crypto-randomUUID for older browsers; crypto.randomUUID
// isn't available in some embedded webviews we care about.
function newUUID(): string {
  // Per RFC 4122 §4.4: set bits 6-7 of clock_seq_hi_and_reserved to 0/1
  // and bits 12-15 of time_hi_and_version to 0/1/0/0.
  const bytes = new Uint8Array(16);
  if (typeof crypto !== "undefined" && crypto.getRandomValues) {
    crypto.getRandomValues(bytes);
  } else {
    // Last-ditch fallback — Math.random is NOT crypto, but a UUID
    // collision in dev-mode auth is harmless.
    for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256);
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0"));
  return (
    hex.slice(0, 4).join("") +
    "-" +
    hex.slice(4, 6).join("") +
    "-" +
    hex.slice(6, 8).join("") +
    "-" +
    hex.slice(8, 10).join("") +
    "-" +
    hex.slice(10, 16).join("")
  );
}

function readCookie(): string | null {
  if (typeof document === "undefined") return null;
  const match = document.cookie
    .split(";")
    .map((c) => c.trim())
    .find((c) => c.startsWith(COOKIE_NAME + "="));
  if (!match) return null;
  const value = decodeURIComponent(match.slice(COOKIE_NAME.length + 1));
  return value || null;
}

function writeCookie(id: string): void {
  if (typeof document === "undefined") return;
  document.cookie = `${COOKIE_NAME}=${encodeURIComponent(
    id,
  )}; path=/; max-age=${COOKIE_MAX_AGE_S}; samesite=lax`;
}

/** Read the dev user id, generating + persisting one if absent. SSR-safe:
 *  returns null when run on the server. */
export function getDevUserID(): string | null {
  if (typeof window === "undefined") return null;

  let id: string | null = null;
  try {
    id = window.localStorage.getItem(STORAGE_KEY);
  } catch {
    // Storage may be disabled (privacy modes); fall through to cookie.
  }
  if (!id) {
    id = readCookie();
  }
  if (!id) {
    id = newUUID();
  }

  // Always re-sync both backends — cheap, keeps them coherent if one
  // got cleared.
  try {
    window.localStorage.setItem(STORAGE_KEY, id);
  } catch {
    /* ignore */
  }
  writeCookie(id);
  return id;
}
