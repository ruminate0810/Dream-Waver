"use client";

import {
  Suspense,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ArrowLeft, ArrowUpRight, ChevronDown, Loader2 } from "lucide-react";
import clsx from "clsx";

import { createGame } from "@/lib/api";

// /code/new is the chat-first entrance to a new HTML5 game. Mirrors
// /slides/new so the two surfaces feel like siblings: one big textarea,
// optional fine-tune strip (genre / difficulty), Begin button. On submit
// we POST createGame and push to /code/{id} where the iframe + chat live.

const GENRES: Array<{ value: string; label: string; note: string }> = [
  { value: "arcade", label: "Arcade", note: "街机 · 单局短" },
  { value: "puzzle", label: "Puzzle", note: "解谜 · 烧脑" },
  { value: "platformer", label: "Platformer", note: "平台跳跃" },
  { value: "shooter", label: "Shooter", note: "射击 · 弹幕" },
  { value: "rogue", label: "Roguelike", note: "随机 · 重玩" },
];

const DIFFICULTIES = ["easy", "normal", "hard"] as const;

export default function NewGamePage() {
  return (
    <Suspense fallback={null}>
      <NewGameChat />
    </Suspense>
  );
}

function NewGameChat() {
  const router = useRouter();
  const search = useSearchParams();
  const [prompt, setPrompt] = useState("");
  const [genre, setGenre] = useState<string>("");
  const [difficulty, setDifficulty] = useState<(typeof DIFFICULTIES)[number]>("normal");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [showOptions, setShowOptions] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    const t = search?.get("prompt");
    if (t && !prompt) setPrompt(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search]);

  useEffect(() => {
    textareaRef.current?.focus();
  }, []);

  async function submit(e?: FormEvent | KeyboardEvent) {
    e?.preventDefault();
    const t = prompt.trim();
    if (!t || submitting) return;
    setSubmitting(true);
    setErr(null);
    try {
      const res = await createGame({
        prompt: t,
        genre: genre || undefined,
        difficulty,
      });
      router.push(`/code/${res.job_id}?session=${res.session_id}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Unknown error");
      setSubmitting(false);
    }
  }

  return (
    <main className="relative min-h-[100dvh] bg-[color:var(--paper)] text-[color:var(--ink)] antialiased">
      <Grain />

      <header className="relative z-10 border-b border-[color:var(--rule)] bg-[color:var(--paper)]/85 backdrop-blur-[2px]">
        <div className="mx-auto flex max-w-[1480px] items-baseline justify-between px-6 py-4 md:px-10">
          <a
            href="/"
            className="group inline-flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
          >
            <ArrowLeft
              size={11}
              strokeWidth={1.8}
              className="translate-y-[1px] transition-transform group-hover:-translate-x-0.5"
            />
            <span>Dream-Waver / Index</span>
          </a>
          <span className="hidden font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)] md:inline">
            New Game
          </span>
        </div>
      </header>

      <div className="relative z-10 mx-auto flex max-w-3xl flex-col px-6 pt-20 md:px-12">
        <div className="mb-10">
          <p className="mb-3 font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
            Brief · 描述你脑中的小游戏
          </p>
          <h1 className="font-display text-[44px] leading-[1.05] tracking-tight text-[color:var(--ink)] md:text-[56px]">
            想做<span className="text-[color:var(--vermillion)]">什么样</span>的小游戏？
          </h1>
        </div>

        <form onSubmit={submit} className="flex flex-col gap-6">
          <div className="border-b border-[color:var(--ink)]/40 pb-3 transition-colors focus-within:border-[color:var(--vermillion)]">
            <textarea
              ref={textareaRef}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit(e);
              }}
              rows={3}
              disabled={submitting}
              placeholder='例: "贪吃蛇，但每吃一个食物速度增加 10%，地图边缘可穿越"'
              className="w-full resize-none bg-transparent font-display text-[22px] leading-snug text-[color:var(--ink)] placeholder:font-display placeholder:text-[20px] placeholder:italic placeholder:text-[color:var(--ink-faint)] focus:outline-none disabled:opacity-50"
            />
          </div>

          <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
            <button
              type="button"
              onClick={() => setShowOptions((v) => !v)}
              className="group inline-flex items-baseline gap-2 font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-soft)] transition-colors hover:text-[color:var(--ink)]"
            >
              <ChevronDown
                size={11}
                strokeWidth={1.6}
                className={clsx(
                  "translate-y-[1px] transition-transform",
                  showOptions && "rotate-180",
                )}
              />
              <span>Fine-tune</span>
              <span className="text-[color:var(--ink-faint)]">·</span>
              <span>{genre ? labelFor(genre) : "Auto genre"}</span>
              <span className="text-[color:var(--ink-faint)]">·</span>
              <span className="capitalize">{difficulty}</span>
            </button>

            <div className="ml-auto flex items-center gap-3">
              {err ? (
                <span className="font-display text-[14px] italic text-red-800">
                  {err}
                </span>
              ) : null}
              <button
                type="submit"
                disabled={!prompt.trim() || submitting}
                className={clsx(
                  "group inline-flex h-11 items-center gap-2 px-5 transition-all duration-200",
                  !prompt.trim() || submitting
                    ? "cursor-not-allowed border border-[color:var(--rule)] text-[color:var(--ink-faint)]"
                    : "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--vermillion)] active:translate-y-[1px]",
                )}
              >
                {submitting ? (
                  <Loader2 size={13} strokeWidth={1.8} className="animate-spin" />
                ) : (
                  <ArrowUpRight
                    size={14}
                    strokeWidth={1.8}
                    className="transition-transform group-hover:-translate-y-[1px] group-hover:translate-x-[1px]"
                  />
                )}
                <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em]">
                  {submitting ? "Building" : "Begin"}
                </span>
              </button>
            </div>
          </div>

          {showOptions ? (
            <FineTuneStrip
              genre={genre}
              setGenre={setGenre}
              difficulty={difficulty}
              setDifficulty={setDifficulty}
            />
          ) : null}

          <p className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-[color:var(--ink-faint)]">
            ⌘ / Ctrl + Enter 提交 · 一句话变成可玩的 HTML5 小游戏
          </p>

          <SuggestionsStrip onPick={(s) => setPrompt(s)} />
        </form>
      </div>
    </main>
  );
}

function labelFor(value: string): string {
  return GENRES.find((g) => g.value === value)?.label ?? value;
}

function FineTuneStrip({
  genre,
  setGenre,
  difficulty,
  setDifficulty,
}: {
  genre: string;
  setGenre: (s: string) => void;
  difficulty: (typeof DIFFICULTIES)[number];
  setDifficulty: (s: (typeof DIFFICULTIES)[number]) => void;
}) {
  return (
    <div className="grid gap-6 border-t border-[color:var(--rule)] pt-5 md:grid-cols-[2fr_1fr]">
      <div>
        <p className="mb-2 font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
          Genre
        </p>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={() => setGenre("")}
            className={clsx(
              "border px-3 py-2 text-left transition-colors",
              !genre
                ? "border-[color:var(--ink)] bg-[color:var(--ink)] text-[color:var(--paper)]"
                : "border-[color:var(--rule)] text-[color:var(--ink-soft)] hover:border-[color:var(--ink)]/30 hover:text-[color:var(--ink)]",
            )}
          >
            <span className="block font-mono-jb text-[10px] uppercase tracking-[0.22em]">
              Auto
            </span>
            <span className="font-display text-[13px] italic opacity-80">
              让模型决定
            </span>
          </button>
          {GENRES.map((g) => (
            <button
              key={g.value}
              type="button"
              onClick={() => setGenre(g.value)}
              className={clsx(
                "border px-3 py-2 text-left transition-colors",
                genre === g.value
                  ? "border-[color:var(--ink)] bg-[color:var(--ink)] text-[color:var(--paper)]"
                  : "border-[color:var(--rule)] text-[color:var(--ink-soft)] hover:border-[color:var(--ink)]/30 hover:text-[color:var(--ink)]",
              )}
            >
              <span className="block font-mono-jb text-[10px] uppercase tracking-[0.22em]">
                {g.label}
              </span>
              <span className="font-display text-[13px] italic opacity-80">
                {g.note}
              </span>
            </button>
          ))}
        </div>
      </div>
      <div>
        <p className="mb-2 font-mono-jb text-[10px] uppercase tracking-[0.26em] text-[color:var(--ink-faint)]">
          Difficulty
        </p>
        <div className="flex flex-col gap-2">
          {DIFFICULTIES.map((d) => (
            <button
              key={d}
              type="button"
              onClick={() => setDifficulty(d)}
              className={clsx(
                "border px-3 py-2 text-left transition-colors",
                difficulty === d
                  ? "border-[color:var(--ink)] bg-[color:var(--ink)] text-[color:var(--paper)]"
                  : "border-[color:var(--rule)] text-[color:var(--ink-soft)] hover:border-[color:var(--ink)]/30 hover:text-[color:var(--ink)]",
              )}
            >
              <span className="block font-mono-jb text-[10px] uppercase capitalize tracking-[0.22em]">
                {d}
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

const STARTERS = [
  "贪吃蛇，但每吃一个食物速度增加 10%，地图边缘可穿越",
  "Flappy Bird，但每过一根柱子小鸟会换一种颜色",
  "2048，但每次合并瓷砖时屏幕会轻微抖动",
  "经典打砖块，加一个会反弹三次的弹球道具",
  "太空入侵者风格的射击游戏，加一个吸引子弹的护盾",
];

function SuggestionsStrip({ onPick }: { onPick: (s: string) => void }) {
  return (
    <div className="mt-2 flex flex-wrap gap-2">
      {STARTERS.map((s) => (
        <button
          key={s}
          type="button"
          onClick={() => onPick(s)}
          className="border border-[color:var(--rule)] px-3 py-1.5 font-display text-[13px] italic text-[color:var(--ink-soft)] transition-colors hover:border-[color:var(--ink)]/30 hover:text-[color:var(--ink)]"
        >
          {s}
        </button>
      ))}
    </div>
  );
}

function Grain() {
  return (
    <div
      aria-hidden
      className="pointer-events-none fixed inset-0 z-0 opacity-[0.05] mix-blend-multiply"
      style={{
        backgroundImage:
          "url(\"data:image/svg+xml;utf8,%3Csvg viewBox='0 0 240 240' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix values='0 0 0 0 0.1 0 0 0 0 0.07 0 0 0 0 0.06 0 0 0 0 0.06 0 0 0 0.8 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
        backgroundSize: "240px 240px",
      }}
    />
  );
}
