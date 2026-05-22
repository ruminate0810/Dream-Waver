"use client";

import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { createSlides } from "@/lib/api";

// Next.js 15 requires every component that calls useSearchParams() to
// be inside a Suspense boundary so static prerender can defer the
// search-params read until client hydration. We wrap the form in one
// Suspense at the page boundary; the child component is the real work.
export default function NewSlidesPage() {
  return (
    <Suspense fallback={null}>
      <NewSlidesForm />
    </Suspense>
  );
}

function NewSlidesForm() {
  const router = useRouter();
  const search = useSearchParams();
  const [topic, setTopic] = useState("");
  const [audience, setAudience] = useState("");
  const [slideCount, setSlideCount] = useState(8);
  const [style, setStyle] = useState("minimalist");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Prefill from `?topic=` so the workspace hero ("对话" button) hands off
  // the user's prompt directly into this form.
  useEffect(() => {
    const t = search?.get("topic");
    if (t && !topic) setTopic(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search]);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setErr(null);
    try {
      const res = await createSlides({
        topic,
        audience,
        slide_count: slideCount,
        force_theme: style,
      });
      router.push(`/slides/${res.job_id}?session=${res.session_id}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "unknown error");
      setSubmitting(false);
    }
  }

  return (
    <main className="min-h-screen px-10 py-12 max-w-3xl mx-auto">
      <a href="/" className="text-sm text-slate-400 hover:text-ink">← Back</a>
      <h1 className="font-serif text-5xl mt-6 mb-10">New presentation</h1>

      <form className="space-y-8" onSubmit={onSubmit}>
        <Field label="Topic / brief" required>
          <textarea
            className="w-full rounded-xl border border-slate-200 p-4 text-lg focus:outline-none focus:ring-2 focus:ring-accent"
            rows={3}
            value={topic}
            placeholder="e.g. A 10-slide investor pitch for our AI agent platform targeting Series A"
            onChange={(e) => setTopic(e.target.value)}
            required
          />
        </Field>

        <div className="grid grid-cols-2 gap-6">
          <Field label="Audience">
            <input
              className="w-full rounded-xl border border-slate-200 p-3 focus:outline-none focus:ring-2 focus:ring-accent"
              value={audience}
              placeholder="Series A VCs"
              onChange={(e) => setAudience(e.target.value)}
            />
          </Field>
          <Field label="Slide count">
            <input
              type="number"
              min={3}
              max={40}
              className="w-full rounded-xl border border-slate-200 p-3"
              value={slideCount}
              onChange={(e) => setSlideCount(Number(e.target.value))}
            />
          </Field>
        </div>

        <Field label="Style">
          <select
            className="w-full rounded-xl border border-slate-200 p-3"
            value={style}
            onChange={(e) => setStyle(e.target.value)}
          >
            <option value="minimalist">Minimalist · 通用 / 极简</option>
            <option value="corporate">Corporate · 商务 / 提案</option>
            <option value="pitch-deck">Pitch Deck · 投资路演 (Linear 风)</option>
            <option value="academic">Academic · 学术 / 研究</option>
            <option value="playful">Playful · 创作者 / 课程</option>
          </select>
        </Field>

        {err && <div className="text-red-600 text-sm">{err}</div>}

        <button
          type="submit"
          disabled={submitting || !topic.trim()}
          className="bg-accent text-white font-semibold px-8 py-4 rounded-full hover:bg-blue-700 disabled:opacity-50"
        >
          {submitting ? "Starting…" : "Generate deck →"}
        </button>
      </form>
    </main>
  );
}

function Field({ label, required, children }: { label: string; required?: boolean; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="block mb-2 text-sm font-medium text-slate-600">
        {label}{required && <span className="text-red-500 ml-1">*</span>}
      </span>
      {children}
    </label>
  );
}
