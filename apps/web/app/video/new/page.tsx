"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { ArrowLeft, Loader2, Sparkles } from "lucide-react";

import { ApiError, createVideoRun } from "@/lib/api";

// /video/new is the entrance to the click-to-regen cinematic short
// pipeline. Compared to /slides/new this is intentionally bare-bones:
// the source-of-truth input is a `story_spec.json` document, and the
// pipeline expects it well-formed (Opendream's validator will reject
// anything else). So instead of a chat-style prompt input, we surface
// a textarea with a paste-or-write affordance and a "use sample" shortcut.
//
// Eventually a "generate spec from prompt" tool will land — when it
// does, it'll live alongside this form as a tab, not replace it. Power
// users always want direct spec access; the spec is the contract.

// Hand-verified to pass Opendream's spec_validator (no errors, only the
// "scene id should be zero-padded 2-digit" warning which is informational).
// Update both this sample AND the validator if you change the shape — the
// pipeline is the source of truth.
//
// Key shape quirks worth remembering:
//   - `char_scene_mapping` is { character_id: [scene_id, ...] },
//     NOT { scene_id: [character_id, ...] }. The validator catches the
//     inversion (E040/E041) but does not auto-flip.
//   - Scene ids should be zero-padded ("01", "02") so they sort
//     correctly in the timeline grid.
//   - sum(scene.duration) must be within ±5 s of target_duration_seconds.
//   - Every scene needs `act` (story act name) and `desc` (one-line
//     synopsis) on top of the camera/action/mood/prompt fields.
const SAMPLE_SPEC = {
  title: "晨光",
  slug: "dawn",
  target_duration_seconds: 12,
  aspect: "16:9",
  resolution: "1080p",
  characters: { ella_young: "Ella, mid-20s, brown hair, soft features" },
  char_prompts: {
    ella_young:
      "ELLA-YOUNG: 25yo woman, shoulder-length brown hair, gentle expression, wearing a beige linen shirt",
  },
  char_scene_mapping: { ella_young: ["01", "02"] },
  scenes: [
    {
      id: "01",
      act: "morning",
      desc: "Ella sits with her morning coffee by the kitchen window.",
      duration: 6,
      camera: "static medium shot, eye level",
      character_action: "ELLA-YOUNG sits by a window holding a steaming mug",
      mood_beat: "quiet introspection",
      transition_out: "hold",
      location: "kitchen, morning light",
      image_prompt:
        "ELLA-YOUNG at a wooden kitchen table beside a tall window, warm dawn light streaming in, holding a ceramic mug with steam rising, soft focus background",
      video_prompt:
        "static medium shot, ELLA-YOUNG slowly lifts the mug, steam curls upward, gentle morning ambience",
    },
    {
      id: "02",
      act: "morning",
      desc: "Sunlight grows; Ella turns toward it and softens into a smile.",
      duration: 6,
      camera: "slow push-in, eye level",
      character_action: "ELLA-YOUNG looks toward the window, faint smile",
      mood_beat: "warmth opening up",
      transition_out: "fade",
      location: "same kitchen, sun fully risen",
      image_prompt:
        "ELLA-YOUNG turning her head toward bright morning sunlight, faint smile forming, light catches her hair, same kitchen table",
      video_prompt:
        "slow push-in on ELLA-YOUNG's face, she turns toward the window, faint smile blooms, warm light intensifies",
    },
  ],
};

export default function NewVideoRunPage() {
  const router = useRouter();
  const [specText, setSpecText] = useState("");
  const [title, setTitle] = useState("");
  const [dryRun, setDryRun] = useState(true); // safer default — see comment
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<{ message: string; fieldErrors: string[] } | null>(null);

  // Why dry-run defaults ON: a real run can spend tens of dollars on
  // Seedance + Gemini calls. We'd rather the user opt INTO the spend
  // explicitly than discover the bill after a typo. The toggle is
  // right there in the form — uncheck and submit.

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr(null);

    let spec: Record<string, unknown>;
    try {
      spec = JSON.parse(specText);
    } catch (parseErr) {
      setErr({ message: `Invalid JSON: ${(parseErr as Error).message}`, fieldErrors: [] });
      return;
    }
    if (typeof spec !== "object" || spec === null || Array.isArray(spec)) {
      setErr({ message: "Spec must be a JSON object at the top level.", fieldErrors: [] });
      return;
    }

    setSubmitting(true);
    try {
      const resp = await createVideoRun({
        spec,
        title: title.trim() || undefined,
        dry_run: dryRun,
      });
      router.push(`/video/${resp.run_id}`);
    } catch (e) {
      if (e instanceof ApiError) {
        setErr({ message: e.message, fieldErrors: e.fieldErrors });
      } else {
        setErr({ message: String((e as Error).message ?? e), fieldErrors: [] });
      }
      setSubmitting(false);
    }
  }

  function loadSample() {
    setSpecText(JSON.stringify(SAMPLE_SPEC, null, 2));
    setTitle(SAMPLE_SPEC.title);
    setErr(null);
  }

  return (
    <main className="min-h-screen bg-white">
      <header className="border-b border-zinc-100">
        <div className="mx-auto flex max-w-4xl items-baseline justify-between px-6 py-4">
          <a
            href="/"
            className="inline-flex items-baseline gap-2 text-xs text-zinc-500 hover:text-zinc-800"
          >
            <ArrowLeft size={12} /> Dream-Waver
          </a>
          <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-400">
            Video · New run
          </span>
        </div>
      </header>

      <div className="mx-auto max-w-4xl px-6 py-10">
        <h1 className="text-2xl font-medium text-zinc-900">Start a video run</h1>
        <p className="mt-2 max-w-2xl text-sm text-zinc-600">
          Submit a <code className="rounded bg-zinc-100 px-1 py-0.5 text-[12px]">story_spec.json</code> describing
          characters and scenes; the click-to-regen timeline opens once the
          DAG is built. Opendream validates the spec before any provider
          call fires, so a structural error fails fast with a per-field
          message.
        </p>

        <form onSubmit={onSubmit} className="mt-8 space-y-5">
          <div>
            <label
              htmlFor="title"
              className="mb-1 block font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500"
            >
              Title <span className="text-zinc-400">(optional)</span>
            </label>
            <input
              id="title"
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={"defaults to spec.title"}
              className="w-full rounded-md border border-zinc-200 px-3 py-2 text-sm focus:border-zinc-400 focus:outline-none"
            />
          </div>

          <div>
            <div className="mb-1 flex items-baseline justify-between">
              <label
                htmlFor="spec"
                className="font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500"
              >
                story_spec.json
              </label>
              <button
                type="button"
                onClick={loadSample}
                className="inline-flex items-baseline gap-1 font-mono text-[10px] uppercase tracking-[0.16em] text-sky-600 hover:text-sky-800"
              >
                <Sparkles size={10} className="self-center" />
                load sample
              </button>
            </div>
            <textarea
              id="spec"
              value={specText}
              onChange={(e) => setSpecText(e.target.value)}
              placeholder='{ "title": "...", "characters": {...}, "scenes": [...] }'
              rows={20}
              className="w-full rounded-md border border-zinc-200 px-3 py-3 font-mono text-[12px] leading-relaxed focus:border-zinc-400 focus:outline-none"
              spellCheck={false}
              required
            />
          </div>

          <label className="flex items-start gap-2 rounded-md border border-amber-100 bg-amber-50 px-3 py-2 text-sm text-amber-900">
            <input
              type="checkbox"
              checked={dryRun}
              onChange={(e) => setDryRun(e.target.checked)}
              className="mt-1"
            />
            <span>
              <span className="font-medium">Dry run</span> — plan the DAG only,
              no provider calls. Uncheck to spend on real Seedance + Gemini
              generation.
            </span>
          </label>

          {err && (
            <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
              <div className="font-medium">{err.message}</div>
              {err.fieldErrors.length > 0 && (
                // Validator failures land here. Each line is one
                // Finding — formatted upstream as
                // `[CODE] scene N: message (fix: hint)`.
                <ul className="mt-1.5 list-disc space-y-0.5 pl-5 text-[13px] text-red-700/90">
                  {err.fieldErrors.map((line, i) => (
                    <li key={i}>{line}</li>
                  ))}
                </ul>
              )}
            </div>
          )}

          <button
            type="submit"
            disabled={submitting || !specText.trim()}
            className="inline-flex items-center gap-2 rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
          >
            {submitting && <Loader2 size={14} className="animate-spin" />}
            {submitting ? "Starting…" : dryRun ? "Plan run" : "Start generation"}
          </button>
        </form>
      </div>
    </main>
  );
}
