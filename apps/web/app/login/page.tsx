"use client";

import { Suspense, useEffect, useRef, useState, type FormEvent } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { gsap } from "gsap";
import { useGSAP } from "@gsap/react";
import { Loader2, Mail } from "lucide-react";

import { getSupabaseClient } from "@/lib/supabase/client";
import { DUR, EASE, PREFERS_FULL_MOTION } from "@/lib/motion";

gsap.registerPlugin(useGSAP);

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

  const containerRef = useRef<HTMLDivElement | null>(null);

  // Sprint Y1 — mount entrance: brand wordmark + caption settle in,
  // form rises shortly after. Reduced-motion gets a 200ms fade with
  // no transform.
  useGSAP(
    () => {
      const mm = gsap.matchMedia();
      mm.add(PREFERS_FULL_MOTION, () => {
        const tl = gsap.timeline({
          defaults: { ease: EASE.entrance, duration: DUR.entrance },
        });
        tl.from(".dw-login-brand", { y: 14, opacity: 0 })
          .from(".dw-login-caption", { y: 8, opacity: 0, duration: DUR.micro }, "-=0.35")
          .from(".dw-login-card", { y: 18, opacity: 0, duration: DUR.secondary }, "-=0.2")
          .from(".dw-login-back", { opacity: 0, duration: DUR.micro }, "-=0.15");
      });
      mm.add("(prefers-reduced-motion: reduce)", () => {
        gsap.from(".dw-login-brand, .dw-login-caption, .dw-login-card, .dw-login-back", {
          opacity: 0,
          duration: 0.2,
          stagger: 0.04,
        });
      });
    },
    { scope: containerRef },
  );

  // Magic-link sent → success card scales in (separate from mount
  // entrance because the state flip is what triggers it, not first
  // paint). useEffect + a quick gsap.from on the card when `sent`
  // flips to true.
  useEffect(() => {
    if (!sent) return;
    const card = containerRef.current?.querySelector(".dw-login-success");
    if (!card) return;
    const mm = gsap.matchMedia();
    mm.add(PREFERS_FULL_MOTION, () => {
      gsap.from(card, {
        scale: 0.94,
        opacity: 0,
        duration: DUR.secondary,
        ease: EASE.feedback,
      });
      // Mail icon gets a tiny bounce so the "we did the thing" feels
      // discrete from the card itself appearing.
      gsap.from(card.querySelector(".dw-login-success-icon"), {
        scale: 0.4,
        opacity: 0,
        duration: 0.45,
        ease: "back.out(2)",
        delay: 0.18,
      });
    });
    return () => {
      mm.revert();
    };
  }, [sent]);

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
    <main
      ref={containerRef}
      className="flex min-h-screen items-center justify-center bg-zinc-50 px-6 text-zinc-900"
    >
      <div className="w-full max-w-sm">
        <header className="mb-8 text-center">
          <h1 className="dw-login-brand text-2xl font-semibold tracking-tight">
            Dream-Waver
          </h1>
          <p className="dw-login-caption mt-1 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-400">
            Sign in to your workspace
          </p>
        </header>

        {sent ? (
          <div className="dw-login-success rounded-md border border-emerald-200 bg-emerald-50 p-6 text-center">
            <Mail
              className="dw-login-success-icon mx-auto mb-2 text-emerald-600"
              size={20}
            />
            <p className="text-[14px] font-medium text-emerald-900">
              Check your email
            </p>
            <p className="mt-1 text-[12px] text-emerald-800">
              We sent a magic link to <span className="font-mono">{email}</span>.
              Click it to sign in.
            </p>
            <button
              type="button"
              onClick={() => {
                setSent(false);
                setEmail("");
              }}
              className="mt-4 text-[11px] text-emerald-700 underline hover:text-emerald-900"
            >
              Use a different email
            </button>
          </div>
        ) : (
          <form onSubmit={onSubmit} className="dw-login-card space-y-3">
            <label className="block">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">
                Email
              </span>
              <input
                type="email"
                autoComplete="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                disabled={submitting}
                placeholder="you@example.com"
                className="w-full rounded-md border border-zinc-200 px-3 py-2 text-sm focus:border-zinc-400 focus:outline-none"
                required
              />
            </label>

            {err && (
              <div className="rounded border border-red-200 bg-red-50 px-2.5 py-2 text-[12px] text-red-700">
                {err}
              </div>
            )}

            <button
              type="submit"
              disabled={submitting || !email.trim()}
              className="inline-flex w-full items-center justify-center gap-2 rounded-md bg-zinc-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
            >
              {submitting && <Loader2 size={13} className="animate-spin" />}
              {submitting ? "Sending link…" : "Send magic link"}
            </button>

            <p className="text-center text-[10px] leading-snug text-zinc-400">
              We&apos;ll email you a one-time sign-in link. No password needed.
            </p>
          </form>
        )}

        <button
          type="button"
          onClick={() => router.push("/")}
          className="dw-login-back mt-6 block w-full text-center font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-400 transition-colors hover:text-zinc-700"
        >
          ← Back to home
        </button>
      </div>
    </main>
  );
}
