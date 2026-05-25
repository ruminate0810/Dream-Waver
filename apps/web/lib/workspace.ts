// Client-side workspace helpers — cookie read/write + active-workspace
// state available to React components.
//
// Why both cookie AND header?
//   - Cookie: middleware.ts and SSR can see the active workspace
//     without an extra round-trip; survives a hard refresh.
//   - Header (X-Workspace-ID): every API call to the Go orchestrator
//     explicitly carries the active workspace so the backend doesn't
//     have to read cookies (Go side stays cookieless and stateless).
//
// The cookie name `dw_workspace_id` matches what the Go auth
// middleware looks for as a fallback when X-Workspace-ID isn't set.

"use client";

const COOKIE_NAME = "dw_workspace_id";
const COOKIE_MAX_AGE_S = 60 * 60 * 24 * 365; // 1 year — workspace choice persists across sessions

/** Read the active workspace id from the cookie. Returns null when
 *  unset or in an SSR context where document.cookie isn't available. */
export function getActiveWorkspaceID(): string | null {
  if (typeof document === "undefined") return null;
  const match = document.cookie
    .split(";")
    .map((c) => c.trim())
    .find((c) => c.startsWith(COOKIE_NAME + "="));
  if (!match) return null;
  const value = decodeURIComponent(match.slice(COOKIE_NAME.length + 1));
  return value || null;
}

/** Persist the active workspace id. Called when the user picks a
 *  workspace from the switcher, OR implicitly when /me returns a
 *  workspace and no cookie is set yet. */
export function setActiveWorkspaceID(id: string): void {
  if (typeof document === "undefined") return;
  // SameSite=Lax so the cookie rides on top-level navigations (e.g.
  // bookmarking a deck URL still carries the workspace context).
  document.cookie = `${COOKIE_NAME}=${encodeURIComponent(id)}; path=/; max-age=${COOKIE_MAX_AGE_S}; samesite=lax`;
}

/** Clear the cookie on logout. */
export function clearActiveWorkspaceID(): void {
  if (typeof document === "undefined") return;
  document.cookie = `${COOKIE_NAME}=; path=/; max-age=0; samesite=lax`;
}
