// Browser-side Supabase client.
//
// Use this inside any "use client" component (auth widgets, hooks,
// realtime subscriptions, etc.). The publishable key is safe to
// expose — Supabase row-level security gates what an unauthenticated
// or authenticated client can actually read or write.
//
// Pair with the server-side helper at ./server.ts when you need to
// run queries from a Server Component, Route Handler, or middleware
// — those flow cookies through correctly so SSR sees the user.
"use client";

import { createBrowserClient } from "@supabase/ssr";

// Cached singleton. Calling createBrowserClient repeatedly creates
// new auth listeners and burns realtime channels; we want exactly
// one client per browser tab.
let _client: ReturnType<typeof createBrowserClient> | null = null;

export function getSupabaseClient() {
  if (_client) return _client;
  const url = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const key = process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY;
  if (!url || !key) {
    throw new Error(
      "Supabase not configured — set NEXT_PUBLIC_SUPABASE_URL and NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY in apps/web/.env.local",
    );
  }
  _client = createBrowserClient(url, key);
  return _client;
}
