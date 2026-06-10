"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Paperclip, Mic, Cpu, Loader2, CornerDownLeft } from "lucide-react";
import clsx from "clsx";

import { createSlides } from "@/lib/api";
import { parseTopic } from "@/lib/parseHints";
import { Eyebrow, PixelButton, WindowCard } from "@/components/ui/pixel";

// Hero — the workspace's primary entrance, pixel re-skin. Type a sentence,
// hit Enter, and you're dropped into the live chat surface (/slides/{id}).
// The input lives inside a WindowCard so the entrance reads as a terminal
// prompt. GSAP entrance hooks (.dw-hero-title / -form / -hint) unchanged.

export function Hero() {
  const router = useRouter();
  const [topic, setTopic] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Entrance is CSS-driven (.dw-hero-* in globals.css, transform-only) so
  // it can't strand at opacity:0 under React Strict Mode or a throttled/
  // backgrounded tab.
  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    const t = topic.trim();
    if (!t || submitting) return;
    setSubmitting(true);
    setErr(null);
    try {
      const { cleanTopic, hints } = parseTopic(t);
      const res = await createSlides({ topic: cleanTopic, slide_count: hints.slideCount });
      router.push(`/slides/${res.job_id}?session=${res.session_id}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "unknown error");
      setSubmitting(false);
    }
  };

  return (
    <section className="flex flex-col items-center px-6 pt-20 text-center">
      <div className="dw-hero-title flex flex-col items-center">
        <Eyebrow className="mb-7">
          <span className="text-accent">✦</span> AGENT-NATIVE CREATIVE OS
        </Eyebrow>
        <h1 className="font-mono text-4xl font-extrabold leading-[1.06] tracking-tight text-ink md:text-[3.25rem]">
          Dream-Waver{" "}
          <span className="relative whitespace-nowrap text-accent">
            AI 工作空间
            <span className="absolute -bottom-0.5 left-0 right-0 -z-10 h-3 bg-accent-soft" />
          </span>
          <span className="ml-3 align-middle font-pixel text-base font-normal text-muted">v0.1</span>
        </h1>
      </div>

      <form onSubmit={submit} className="dw-hero-form mt-11 w-full max-w-3xl text-left">
        <WindowCard
          title="~/new-deck — 描述你想做什么"
          className="shadow-pixel-lg"
          bodyClassName="p-0"
        >
          <textarea
            value={topic}
            onChange={(e) => setTopic(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                submit();
              }
            }}
            rows={2}
            disabled={submitting}
            placeholder="询问任何问题，创造任何事物…"
            className="w-full resize-none bg-transparent px-6 pb-2 pt-5 text-lg text-ink placeholder:text-muted focus:outline-none disabled:opacity-50"
          />

          <div className="flex items-center justify-between gap-2 border-t-2 border-line px-3.5 py-3">
            <div className="flex items-center gap-2">
              <IconButton aria-label="附件 · 即将上线" disabled title="附件 · 即将上线">
                <Paperclip size={17} strokeWidth={1.8} />
              </IconButton>
              <span
                title="DeepSeek v4-pro (planner) · v4-flash (worker)"
                className="inline-flex items-center gap-1.5 rounded-pixel border-2 border-line-2 px-2.5 py-1.5 text-xs font-semibold text-ink-2"
              >
                <Cpu size={14} strokeWidth={1.8} />
                <span>v4-pro</span>
              </span>
            </div>

            <div className="flex items-center gap-2">
              {err && <span className="px-1 text-sm text-vermillion">{err}</span>}
              <IconButton aria-label="语音 · 即将上线" disabled title="语音 · 即将上线">
                <Mic size={17} strokeWidth={1.8} />
              </IconButton>
              <PixelButton
                type="submit"
                variant={topic.trim() && !submitting ? "accent" : "default"}
                disabled={!topic.trim() || submitting}
              >
                {submitting ? (
                  <Loader2 size={15} strokeWidth={2.2} className="animate-spin" />
                ) : (
                  <CornerDownLeft size={15} strokeWidth={2.2} />
                )}
                <span>{submitting ? "对话中" : "对话"}</span>
              </PixelButton>
            </div>
          </div>
        </WindowCard>
      </form>

      <p className="dw-hero-hint mt-4 font-mono text-[12px] text-muted">
        Enter 直接对话 · Shift+Enter 换行 · 进入对话后可继续微调
      </p>
    </section>
  );
}

function IconButton({
  children,
  disabled,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { children: React.ReactNode }) {
  return (
    <button
      type="button"
      disabled={disabled}
      {...rest}
      className={clsx(
        "grid h-9 w-9 place-items-center rounded-pixel transition-colors",
        disabled ? "cursor-not-allowed text-line-2" : "text-muted hover:bg-surface-2 hover:text-ink",
      )}
    >
      {children}
    </button>
  );
}
