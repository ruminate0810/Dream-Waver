import { NextResponse, type NextRequest } from "next/server";
import { createServerClient } from "@supabase/ssr";

// /auth/callback is Supabase's redirect target after a magic-link
// click. The link carries a `?code=...` we exchange for a session;
// once that's done we drop the session cookies and bounce to `next`.
//
// On any failure we redirect to /login?error=... so the form can
// surface the message instead of leaving the user stranded on a
// blank page.

export async function GET(req: NextRequest) {
  const { searchParams, origin } = req.nextUrl;
  const code = searchParams.get("code");
  const next = searchParams.get("next") ?? "/slides/new";

  if (!code) {
    return NextResponse.redirect(
      new URL("/login?error=missing_code", origin),
    );
  }

  const url = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const key = process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY;
  if (!url || !key) {
    return NextResponse.redirect(
      new URL("/login?error=supabase_not_configured", origin),
    );
  }

  // Build the response first so the Supabase cookie-write hooks can
  // mutate its headers. This is the canonical @supabase/ssr pattern
  // for route handlers — the response we return at the end carries
  // every cookie the auth exchange wrote.
  const response = NextResponse.redirect(new URL(next, origin));

  const supabase = createServerClient(url, key, {
    cookies: {
      getAll: () => req.cookies.getAll(),
      setAll: (toSet) => {
        toSet.forEach(({ name, value, options }) => {
          response.cookies.set(name, value, options);
        });
      },
    },
  });

  const { error } = await supabase.auth.exchangeCodeForSession(code);
  if (error) {
    return NextResponse.redirect(
      new URL(
        `/login?error=${encodeURIComponent(error.message)}`,
        origin,
      ),
    );
  }
  return response;
}
