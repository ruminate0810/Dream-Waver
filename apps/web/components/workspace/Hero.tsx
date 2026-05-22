"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Paperclip, Mic, Dumbbell, AudioLines, Loader2 } from "lucide-react";
import clsx from "clsx";

import { createSlides } from "@/lib/api";
import { parseTopic } from "@/lib/parseHints";

// Hero is the workspace's primary entrance — a single big input. Type
// a sentence, hit Enter, and you're dropped into the live chat surface
// (/slides/{id}) on the very next paint. No middle form.
//
// Two design choices worth flagging:
//   1. Plain Enter submits; Shift+Enter inserts a newline. Genspark /
//      ChatGPT muscle memory wins over textarea-default semantics.
//   2. We POST createSlides here directly and router.push to the
//      created job — bypassing /slides/new entirely. /slides/new still
//      exists as a fine-tune entrance (reached from the AI 幻灯片 card
//      or the sidebar "新建" button) where the user can pick page count
//      and style before kicking off. Two intentional doors: quick
//      (Hero) and intentional (form).
export function Hero() {
  const router = useRouter();
  const [topic, setTopic] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    const t = topic.trim();
    if (!t || submitting) return;
    setSubmitting(true);
    setErr(null);
    try {
      // Honor natural-language hints in the topic — "10 页" / "10
      // pages" sets slide_count=10. Without this the API silently
      // uses the default (8) and the deck ignores what the user said.
      const { cleanTopic, hints } = parseTopic(t);
      const res = await createSlides({
        topic: cleanTopic,
        slide_count: hints.slideCount,
      });
      router.push(`/slides/${res.job_id}?session=${res.session_id}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "unknown error");
      setSubmitting(false);
    }
  };

  return (
    <section className="flex flex-col items-center px-6 pt-24">
      <h1 className="font-serif text-5xl md:text-6xl tracking-tight text-ink">
        Dream-Waver AI 工作空间
        <span className="ml-3 align-middle text-2xl font-sans font-light text-zinc-400">
          0.1
        </span>
      </h1>

      <form
        onSubmit={submit}
        className="mt-12 w-full max-w-3xl rounded-3xl border border-zinc-200 bg-white shadow-[0_1px_2px_rgba(0,0,0,0.04),0_8px_24px_rgba(0,0,0,0.04)]"
      >
        <textarea
          value={topic}
          onChange={(e) => setTopic(e.target.value)}
          onKeyDown={(e) => {
            // Plain Enter submits; Shift+Enter is the multi-line escape
            // hatch. Matches the chat-app muscle memory the rest of the
            // app already uses (ChatBubbles, FollowupInput keep
            // Cmd+Enter as the override; this surface is the entrance
            // so we keep it as cheap as possible).
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
          rows={2}
          disabled={submitting}
          placeholder="询问任何问题，创造任何事物"
          className="w-full resize-none rounded-3xl px-7 pt-6 pb-2 text-lg text-ink placeholder:text-zinc-400 focus:outline-none disabled:opacity-50"
        />

        <div className="flex items-center justify-between px-4 py-3">
          {/* Left: attach + model. Attach + voice are not wired yet —
              the buttons stay so the surface reads as a chat app, not a
              search box, but they're disabled with a tooltip. */}
          <div className="flex items-center gap-2">
            <IconButton aria-label="Attach files (coming soon)" disabled title="附件 · 即将上线">
              <Paperclip size={18} strokeWidth={1.8} />
            </IconButton>

            <button
              type="button"
              className="flex items-center gap-1.5 rounded-full border border-zinc-200 px-3 py-1.5 text-sm text-zinc-600 hover:bg-zinc-50"
              title="DeepSeek v4-pro (planner) · v4-flash (worker)"
            >
              <Dumbbell size={15} strokeWidth={1.8} />
              <span>v4-pro</span>
            </button>
          </div>

          {/* Right: voice + send. Voice is unwired; send is the
              load-bearing button. */}
          <div className="flex items-center gap-2">
            {err && (
              <span className="px-2 text-sm italic text-red-700">{err}</span>
            )}
            <IconButton aria-label="Voice input (coming soon)" disabled title="语音 · 即将上线">
              <Mic size={18} strokeWidth={1.8} />
            </IconButton>

            <button
              type="submit"
              disabled={!topic.trim() || submitting}
              className={clsx(
                "flex items-center gap-2 rounded-full px-5 py-2 text-sm font-medium transition-colors",
                topic.trim() && !submitting
                  ? "bg-ink text-white hover:bg-zinc-800"
                  : "bg-zinc-100 text-zinc-400",
              )}
            >
              {submitting ? (
                <Loader2 size={15} strokeWidth={2} className="animate-spin" />
              ) : (
                <AudioLines size={15} strokeWidth={2} />
              )}
              <span>{submitting ? "对话中" : "对话"}</span>
            </button>
          </div>
        </div>
      </form>

      <p className="mt-4 text-[12px] text-zinc-400">
        Enter 直接对话 · Shift+Enter 换行 · 进入对话后可继续微调
      </p>
    </section>
  );
}

function IconButton({
  children,
  disabled,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      {...rest}
      className={clsx(
        "flex h-9 w-9 items-center justify-center rounded-full transition-colors",
        disabled
          ? "cursor-not-allowed text-zinc-300"
          : "text-zinc-500 hover:bg-zinc-100",
      )}
    >
      {children}
    </button>
  );
}
