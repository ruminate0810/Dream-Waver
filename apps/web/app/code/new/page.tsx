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
import { PixelButton, Sprite, WindowCard } from "@/components/ui/pixel";

// /code/new is the chat-first entrance to a new HTML5 game. Mirrors
// /slides/new so the two surfaces feel like siblings: one big textarea,
// optional fine-tune strip (genre / difficulty), Begin button. On submit
// we POST createGame and push to /code/{id} where the iframe + chat live.
//
// Pixel re-skin: the brief lives inside a WindowCard terminal prompt;
// entrances are CSS-driven (animate-rise) so nothing strands at opacity:0
// under React Strict Mode.

const GENRES: Array<{ value: string; label: string; note: string }> = [
  { value: "arcade", label: "Arcade", note: "街机 · 单局短" },
  { value: "puzzle", label: "Puzzle", note: "解谜 · 烧脑" },
  { value: "platformer", label: "Platformer", note: "平台跳跃" },
  { value: "shooter", label: "Shooter", note: "射击 · 弹幕" },
  { value: "rogue", label: "Roguelike", note: "随机 · 重玩" },
];

const AESTHETICS: Array<{ value: string; label: string; note: string }> = [
  { value: "minimalist", label: "Minimalist", note: "极简 · 大面积留白" },
  { value: "neon", label: "Neon", note: "霓虹 · 深紫底荧光" },
  { value: "paper", label: "Paper", note: "纸感 · 水彩墨笔" },
  { value: "pixel", label: "Pixel", note: "像素 · 8-bit 复古" },
  { value: "editorial", label: "Editorial", note: "编辑设计 · 杂志感" },
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
  const [aesthetic, setAesthetic] = useState<string>("");
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
        aesthetic: aesthetic || undefined,
        difficulty,
      });
      router.push(`/code/${res.job_id}?session=${res.session_id}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Unknown error");
      setSubmitting(false);
    }
  }

  return (
    <main className="dot-grid relative min-h-[100dvh] bg-paper text-ink antialiased">
      <header className="relative z-10 border-b-2 border-ink bg-paper/85 backdrop-blur-[2px]">
        <div className="mx-auto flex max-w-[1480px] items-center justify-between px-6 py-3.5 md:px-10">
          <a
            href="/"
            className="group inline-flex items-center gap-2 font-mono text-[11px] font-semibold tracking-wide text-ink-2 transition-colors hover:text-ink"
          >
            <span className="grid h-6 w-6 place-items-center rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm transition-transform group-hover:-translate-x-0.5">
              <ArrowLeft size={11} strokeWidth={2} />
            </span>
            <span>DREAM-WAVER / INDEX</span>
          </a>
          <span className="hidden font-pixel text-[0.55rem] tracking-wide text-muted md:inline">
            ~/code/new
          </span>
        </div>
      </header>

      <div className="relative z-10 mx-auto flex max-w-3xl flex-col px-6 pt-20 md:px-12">
        <div className="relative mb-10 animate-rise">
          <p className="mb-3 font-mono text-[10px] font-semibold uppercase tracking-wide text-muted">
            Brief · 描述你脑中的小游戏
          </p>
          <h1 className="font-mono text-[34px] font-extrabold leading-[1.12] tracking-tight text-ink md:text-[44px]">
            想做
            <span className="relative whitespace-nowrap text-accent">
              什么样
              <span className="absolute -bottom-0.5 left-0 right-0 -z-10 h-3 bg-accent-soft" />
            </span>
            的小游戏？
          </h1>
          <div className="pointer-events-none absolute -top-3 right-0 hidden rotate-6 md:block">
            <Sprite name="robot" size={44} />
          </div>
        </div>

        <form
          onSubmit={submit}
          className="flex animate-rise flex-col gap-6"
          style={{ animationDelay: "0.08s" }}
        >
          <WindowCard
            title="~/new-game — 一句话变成可玩的游戏"
            className="shadow-pixel-lg"
            bodyClassName="p-0"
          >
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
              className="w-full resize-none bg-transparent px-5 pb-3 pt-4 text-[16px] leading-relaxed text-ink placeholder:text-muted focus:outline-none disabled:opacity-50"
            />

            <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t-2 border-line px-4 py-3">
              <button
                type="button"
                onClick={() => setShowOptions((v) => !v)}
                className="group inline-flex items-center gap-2 font-mono text-[11px] font-semibold tracking-wide text-ink-2 transition-colors hover:text-ink"
              >
                <ChevronDown
                  size={12}
                  strokeWidth={2}
                  className={clsx(
                    "transition-transform",
                    showOptions && "rotate-180",
                  )}
                />
                <span>Fine-tune</span>
                <span className="text-muted">·</span>
                <span>{genre ? labelFor(genre, GENRES) : "Auto genre"}</span>
                <span className="text-muted">·</span>
                <span>{aesthetic ? labelFor(aesthetic, AESTHETICS) : "Auto style"}</span>
                <span className="text-muted">·</span>
                <span className="capitalize">{difficulty}</span>
              </button>

              <div className="ml-auto flex items-center gap-3">
                {err ? (
                  <span className="font-mono text-[12px] font-semibold text-[#a23a2a]">
                    {err}
                  </span>
                ) : null}
                <PixelButton
                  type="submit"
                  variant={prompt.trim() && !submitting ? "accent" : "default"}
                  disabled={!prompt.trim() || submitting}
                >
                  {submitting ? (
                    <Loader2 size={14} strokeWidth={2.2} className="animate-spin" />
                  ) : (
                    <ArrowUpRight size={14} strokeWidth={2.2} />
                  )}
                  <span>{submitting ? "Building" : "Begin"}</span>
                </PixelButton>
              </div>
            </div>
          </WindowCard>

          {showOptions ? (
            <FineTuneStrip
              genre={genre}
              setGenre={setGenre}
              aesthetic={aesthetic}
              setAesthetic={setAesthetic}
              difficulty={difficulty}
              setDifficulty={setDifficulty}
            />
          ) : null}

          <p className="font-mono text-[11px] text-muted">
            ⌘ / Ctrl + Enter 提交 · 一句话变成可玩的 HTML5 小游戏
          </p>

          <SuggestionsStrip onPick={(s) => setPrompt(s)} />
        </form>
      </div>
    </main>
  );
}

function labelFor(value: string, table: Array<{ value: string; label: string }>): string {
  return table.find((g) => g.value === value)?.label ?? value;
}

function FineTuneStrip({
  genre,
  setGenre,
  aesthetic,
  setAesthetic,
  difficulty,
  setDifficulty,
}: {
  genre: string;
  setGenre: (s: string) => void;
  aesthetic: string;
  setAesthetic: (s: string) => void;
  difficulty: (typeof DIFFICULTIES)[number];
  setDifficulty: (s: (typeof DIFFICULTIES)[number]) => void;
}) {
  return (
    <div className="flex animate-rise flex-col gap-6 rounded-pixel border-2 border-ink bg-surface-2 p-5 shadow-pixel-sm">
      <PickerGroup
        label="Genre"
        sub="决定机制（街机短局 / 解谜 / 平台跳跃 / 射击 / Rogue）"
        value={genre}
        setValue={setGenre}
        options={GENRES}
      />
      <PickerGroup
        label="Aesthetic"
        sub="决定视觉与音效风格（覆盖默认色板）"
        value={aesthetic}
        setValue={setAesthetic}
        options={AESTHETICS}
      />
      <div>
        <p className="mb-2 font-pixel text-[0.55rem] uppercase tracking-wide text-muted">
          Difficulty
        </p>
        <div className="flex flex-wrap gap-2">
          {DIFFICULTIES.map((d) => (
            <button
              key={d}
              type="button"
              onClick={() => setDifficulty(d)}
              className={clsx(
                "rounded-pixel border-2 px-3 py-2 text-left transition-all",
                difficulty === d
                  ? "border-ink bg-accent-soft shadow-pixel-sm"
                  : "border-line-2 bg-surface hover:border-ink",
              )}
            >
              <span className="block font-mono text-[11px] font-bold capitalize tracking-wide text-ink">
                {d}
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

function PickerGroup({
  label,
  sub,
  value,
  setValue,
  options,
}: {
  label: string;
  sub: string;
  value: string;
  setValue: (s: string) => void;
  options: Array<{ value: string; label: string; note: string }>;
}) {
  return (
    <div>
      <p className="mb-1 font-pixel text-[0.55rem] uppercase tracking-wide text-muted">
        {label}
      </p>
      <p className="mb-2.5 font-mono text-[12px] text-ink-2">{sub}</p>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          onClick={() => setValue("")}
          className={clsx(
            "rounded-pixel border-2 px-3 py-2 text-left transition-all",
            !value
              ? "border-ink bg-accent-soft shadow-pixel-sm"
              : "border-line-2 bg-surface hover:border-ink",
          )}
        >
          <span className="block font-mono text-[11px] font-bold tracking-wide text-ink">
            Auto
          </span>
          <span className="font-mono text-[12px] text-ink-2">让模型决定</span>
        </button>
        {options.map((o) => (
          <button
            key={o.value}
            type="button"
            onClick={() => setValue(o.value)}
            className={clsx(
              "rounded-pixel border-2 px-3 py-2 text-left transition-all",
              value === o.value
                ? "border-ink bg-accent-soft shadow-pixel-sm"
                : "border-line-2 bg-surface hover:border-ink",
            )}
          >
            <span className="block font-mono text-[11px] font-bold tracking-wide text-ink">
              {o.label}
            </span>
            <span className="font-mono text-[12px] text-ink-2">{o.note}</span>
          </button>
        ))}
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
          className="rounded-pixel border-2 border-line-2 bg-surface px-3 py-1.5 text-left font-mono text-[12.5px] text-ink-2 transition-all hover:-translate-x-[1px] hover:-translate-y-[1px] hover:border-ink hover:text-ink hover:shadow-pixel-sm"
        >
          {s}
        </button>
      ))}
    </div>
  );
}
