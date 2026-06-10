"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { ArrowLeft, Loader2, Sparkles } from "lucide-react";

import { ApiError, createVideoRun } from "@/lib/api";
import { PixelButton, WindowCard } from "@/components/ui/pixel";

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

  // Pixel re-skin: entrances are CSS-driven (`animate-rise` + per-block
  // animation-delay) so nothing strands at opacity:0 under React Strict
  // Mode / a backgrounded tab. No GSAP needed on this page.

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
    <main className="dot-grid min-h-screen bg-paper text-ink antialiased">
      <header className="border-b-2 border-ink bg-paper/85 backdrop-blur-[2px]">
        <div className="mx-auto flex max-w-4xl items-center justify-between px-6 py-3">
          <a
            href="/"
            className="group inline-flex items-center gap-2 font-mono text-[11px] font-semibold tracking-wide text-ink-2 transition-colors hover:text-ink"
          >
            <span className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm transition-transform group-hover:-translate-x-0.5">
              <ArrowLeft size={11} strokeWidth={2} />
            </span>
            Dream-Waver
          </a>
          <span className="font-pixel text-[0.55rem] uppercase tracking-wide text-muted">
            Video · New run
          </span>
        </div>
      </header>

      <div className="mx-auto max-w-4xl px-6 py-10">
        <h1 className="animate-rise font-mono text-2xl font-extrabold tracking-tight text-ink">
          Start a video run
        </h1>
        <p className="animate-rise mt-3 max-w-2xl font-mono text-sm leading-relaxed text-ink-2 [animation-delay:60ms]">
          Submit a{" "}
          <code className="rounded-[4px] border border-line bg-surface-2 px-1 py-0.5 font-mono text-[12px] text-ink">
            story_spec.json
          </code>{" "}
          describing
          characters and scenes; the click-to-regen timeline opens once the
          DAG is built. Opendream validates the spec before any provider
          call fires, so a structural error fails fast with a per-field
          message.
        </p>

        <form onSubmit={onSubmit} className="animate-rise mt-8 space-y-5 [animation-delay:120ms]">
          <div>
            <label
              htmlFor="title"
              className="mb-1.5 block font-pixel text-[0.55rem] uppercase tracking-wide text-ink-2"
            >
              Title <span className="text-muted">(optional)</span>
            </label>
            <input
              id="title"
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={"defaults to spec.title"}
              className="w-full rounded-pixel border-2 border-ink bg-surface px-3 py-2 font-mono text-sm text-ink transition-shadow placeholder:text-muted focus:shadow-pixel-sm focus:outline-none"
            />
          </div>

          {/* The spec editor is the centerpiece — frame it as a little
              editor window so the filename reads as window chrome. */}
          <WindowCard
            title={
              <label htmlFor="spec" className="font-pixel text-[0.55rem] tracking-wide text-ink-2">
                story_spec.json
              </label>
            }
            right={
              <button
                type="button"
                onClick={loadSample}
                className="inline-flex items-center gap-1.5 font-pixel text-[0.55rem] uppercase tracking-wide text-accent transition-colors hover:text-ink"
              >
                <Sparkles size={10} className="self-center" />
                load sample
              </button>
            }
            className="transition-shadow focus-within:shadow-pixel-lg"
            bodyClassName="p-0"
          >
            <textarea
              id="spec"
              value={specText}
              onChange={(e) => setSpecText(e.target.value)}
              placeholder='{ "title": "...", "characters": {...}, "scenes": [...] }'
              rows={20}
              className="block w-full bg-surface px-3 py-3 font-mono text-[12px] leading-relaxed text-ink placeholder:text-muted focus:outline-none"
              spellCheck={false}
              required
            />
          </WindowCard>

          <label className="flex items-start gap-2.5 rounded-pixel border-2 border-ink bg-[#fff7e8] px-3 py-2.5 font-mono text-sm text-[#9a6b15] shadow-pixel-sm">
            <input
              type="checkbox"
              checked={dryRun}
              onChange={(e) => setDryRun(e.target.checked)}
              className="mt-1 accent-gold"
            />
            <span>
              <span className="font-semibold">Dry run</span> — plan the DAG only,
              no provider calls. Uncheck to spend on real Seedance + Gemini
              generation.
            </span>
          </label>

          {err && (
            <div className="rounded-pixel border-2 border-ink bg-[#fdece9] px-3 py-2.5 font-mono text-sm text-[#a23a2a] shadow-pixel-sm">
              <div className="font-semibold">{err.message}</div>
              {err.fieldErrors.length > 0 && (
                // Validator failures land here. Each line is one
                // Finding — formatted upstream as
                // `[CODE] scene N: message (fix: hint)`.
                <ul className="mt-1.5 list-disc space-y-0.5 pl-5 text-[13px] text-[#a23a2a]/90">
                  {err.fieldErrors.map((line, i) => (
                    <li key={i}>{line}</li>
                  ))}
                </ul>
              )}
            </div>
          )}

          <PixelButton
            type="submit"
            variant="accent"
            disabled={submitting || !specText.trim()}
          >
            {submitting && <Loader2 size={14} className="animate-spin" />}
            {submitting ? "Starting…" : dryRun ? "Plan run" : "Start generation"}
          </PixelButton>
        </form>
      </div>
    </main>
  );
}
