import { NextResponse, type NextRequest } from "next/server";
import { createServerClient } from "@supabase/ssr";

// Middleware does two jobs:
//
//  1. **Cookie refresh** — Supabase access tokens live ~1 hour. The
//     SSR helper re-issues a refresh on every request when one is
//     about to expire. We mirror the cookie writes onto the outgoing
//     response so the browser sees the new state.
//
//  2. **Auth-required redirect** — anonymous visitors to protected
//     paths (everything that needs `/slides/{id}`, `/games/{id}`,
//     `/video/{id}`, `/design`, or `/me`) get bounced to /login with
//     a `?next=` query so post-login they land where they were going.
//
// The matcher below excludes `/login`, `/auth/*`, `/_next/*`,
// `/public/*`, and `/api/*` (the Go orchestrator handles its own auth
// for API calls — the browser's cookie just rides along).

const PROTECTED_PREFIXES = [
  "/slides",
  "/games",
  "/video",
  "/design",
  "/code",
  "/me",
  "/workspaces",
];

const PUBLIC_PREFIXES = [
  "/login",
  "/auth",
  "/_next",
  "/favicon",
  "/api", // proxied to Go, has its own auth
];

export async function middleware(req: NextRequest) {
  // Pass-through fast path for public paths so SSR cost on those
  // routes is unchanged.
  if (PUBLIC_PREFIXES.some((p) => req.nextUrl.pathname.startsWith(p))) {
    return NextResponse.next();
  }

  let response = NextResponse.next({ request: req });

  // Supabase client mirrored from lib/supabase/server.ts but bound to
  // middleware's request/response cookie API. Each request gets a
  // fresh client because the cookie reader/writer is per-request.
  const url = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const key = process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY;
  if (!url || !key) {
    // Supabase not configured — let the request through unmodified.
    // Local dev without Supabase still works via the orchestrator's
    // dev-mode auth fallback (X-Dev-User-Id header).
    return response;
  }

  const supabase = createServerClient(url, key, {
    cookies: {
      getAll: () => req.cookies.getAll(),
      setAll: (toSet) => {
        toSet.forEach(({ name, value, options }) => {
          // setAll fires twice — once on the request (so the next
          // call to getAll sees the new value), once on the response
          // (so the browser receives the new cookie). Mirror both.
          req.cookies.set(name, value);
          response.cookies.set(name, value, options);
        });
      },
    },
  });

  // getUser() forces a real round-trip to Supabase (server-validated);
  // getSession() trusts the JWT in the cookie which is fine for the
  // hot path but doesn't catch revoked tokens. Middleware is the
  // right place to pay the validation cost once per request.
  const { data: { user } } = await supabase.auth.getUser();

  const path = req.nextUrl.pathname;
  const requiresAuth = PROTECTED_PREFIXES.some((p) => path.startsWith(p));

  if (requiresAuth && !user) {
    const loginURL = new URL("/login", req.url);
    loginURL.searchParams.set("next", path + req.nextUrl.search);
    return NextResponse.redirect(loginURL);
  }

  // Signed-in users hitting /login or / get bounced to their workspace.
  // (Soft default: /slides/new — Dream-Waver's most-used entry today.)
  if (user && (path === "/login" || path === "/")) {
    const home = req.nextUrl.searchParams.get("next") ?? "/slides/new";
    return NextResponse.redirect(new URL(home, req.url));
  }

  return response;
}

// Skip middleware on static assets so we don't pay per-image SSR.
export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico|theme-previews|.*\\.svg).*)"],
};
