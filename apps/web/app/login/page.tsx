"use client";

import { Suspense, useState, type FormEvent } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Loader2, Mail } from "lucide-react";

import { getSupabaseClient } from "@/lib/supabase/client";
import { PixelButton, WindowCard } from "@/components/ui/pixel";

// /login is the entry to the magic-link flow. Type email → Supabase
// sends a link → user clicks → /auth/callback exchanges the code for
// a session → middleware sees the cookie on the next request.
//
// Phase 1 ships ONLY magic link (no password, no OAuth provider).
// OAuth + password live behind feature flags we'll add when there's
// demand — keeping the surface small keeps the auth-failure modes
// small too. Magic link is end-user-friendly and skips a whole pile
// of password-management code.

export default function LoginPage() {
  return (
    <Suspense fallback={null}>
      <LoginForm />
    </Suspense>
  );
}

function LoginForm() {
  const router = useRouter();
  const search = useSearchParams();
  const nextPath = search?.get("next") ?? "/slides/new";

  const [email, setEmail] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [sent, setSent] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Entrance is CSS-driven (`animate-rise` + staggered delays) so it can
  // never strand content invisible the way a reverted gsap.from can under
  // React Strict Mode. The success card animates on appearance for free —
  // it only mounts when `sent` flips true.

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr(null);
    if (!email.trim()) {
      setErr("Enter your email.");
      return;
    }
    setSubmitting(true);
    try {
      const supabase = getSupabaseClient();
      // emailRedirectTo lands at our callback with a `code` query that
      // we exchange for a session cookie. The `next` query lets the
      // callback bounce the user back to where they started.
      const redirectTo = `${window.location.origin}/auth/callback?next=${encodeURIComponent(nextPath)}`;
      const { error } = await supabase.auth.signInWithOtp({
        email: email.trim(),
        options: { emailRedirectTo: redirectTo },
      });
      if (error) {
        setErr(error.message);
        return;
      }
      setSent(true);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="dot-grid flex min-h-screen items-center justify-center bg-paper px-6 text-ink antialiased">
      <div className="w-full max-w-sm">
        <header className="animate-rise mb-8 text-center">
          <h1 className="font-mono text-2xl font-extrabold tracking-tight text-ink">
            Dream-Waver
          </h1>
          <p className="mt-2 font-pixel text-[0.55rem] tracking-wide text-muted">
            Sign in to your workspace
          </p>
        </header>

        {sent ? (
          <WindowCard
            title="~/login"
            className="animate-rise"
            bodyClassName="bg-[#eaf7ef] p-6 text-center"
          >
            <Mail className="mx-auto mb-2 text-grass" size={20} />
            <p className="text-[14px] font-semibold text-[#1f7a4d]">
              Check your email
            </p>
            <p className="mt-1 text-[12px] text-[#1f7a4d]">
              We sent a magic link to <span className="font-mono font-semibold">{email}</span>.
              Click it to sign in.
            </p>
            <button
              type="button"
              onClick={() => {
                setSent(false);
                setEmail("");
              }}
              className="mt-4 text-[11px] text-[#1f7a4d] underline hover:text-ink"
            >
              Use a different email
            </button>
          </WindowCard>
        ) : (
          <WindowCard
            title="~/login"
            className="animate-rise shadow-pixel-lg [animation-delay:90ms]"
          >
            <form onSubmit={onSubmit} className="space-y-3">
              <label className="block">
                <span className="mb-1.5 block font-pixel text-[0.55rem] tracking-wide text-ink-2">
                  Email
                </span>
                <input
                  type="email"
                  autoComplete="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  disabled={submitting}
                  placeholder="you@example.com"
                  className="w-full rounded-pixel border-2 border-ink bg-surface px-3 py-2 font-mono text-sm text-ink placeholder:text-muted focus:outline-none focus:shadow-pixel-sm disabled:opacity-50"
                  required
                />
              </label>

              {err && (
                <div className="rounded-pixel border-2 border-ink bg-[#fdece9] px-2.5 py-2 text-[12px] text-[#a23a2a]">
                  {err}
                </div>
              )}

              <PixelButton
                type="submit"
                variant="accent"
                className="w-full"
                disabled={submitting || !email.trim()}
              >
                {submitting && <Loader2 size={13} className="animate-spin" />}
                {submitting ? "Sending link…" : "Send magic link"}
              </PixelButton>

              <p className="text-center font-mono text-[10px] leading-snug text-muted">
                We&apos;ll email you a one-time sign-in link. No password needed.
              </p>
            </form>
          </WindowCard>
        )}

        <button
          type="button"
          onClick={() => router.push("/")}
          className="animate-rise mt-6 block w-full text-center font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-muted transition-colors hover:text-ink [animation-delay:160ms]"
        >
          ← Back to home
        </button>
      </div>
    </main>
  );
}
