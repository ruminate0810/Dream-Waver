// Server-side Supabase client — Server Components, Route Handlers,
// and Server Actions all consume this. The cookies() bridge is what
// lets the server see the user's auth state (Supabase stores the
// session in httpOnly cookies; @supabase/ssr handles refresh).
//
// IMPORTANT: each request must call createServerSupabaseClient()
// fresh because Next.js cookies() returns a per-request store. Do
// NOT cache the returned client across requests.

import { createServerClient } from "@supabase/ssr";
import { cookies } from "next/headers";

export async function createServerSupabaseClient() {
  const url = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const key = process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY;
  if (!url || !key) {
    throw new Error(
      "Supabase not configured — set NEXT_PUBLIC_SUPABASE_URL and NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY in apps/web/.env.local",
    );
  }
  // Next.js 15 made cookies() async — must await.
  const cookieStore = await cookies();
  return createServerClient(url, key, {
    cookies: {
      getAll: () => cookieStore.getAll(),
      setAll: (toSet) => {
        // In Server Components cookies are read-only; the setAll
        // attempt throws but we swallow it because middleware
        // (middleware.ts, when added) handles the refresh-token
        // write path. This is the canonical @supabase/ssr pattern.
        try {
          toSet.forEach(({ name, value, options }) => {
            cookieStore.set(name, value, options);
          });
        } catch {
          /* Server Component context — middleware writes cookies */
        }
      },
    },
  });
}
