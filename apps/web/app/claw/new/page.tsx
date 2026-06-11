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
import { ArrowLeft, ArrowUpRight, Loader2 } from "lucide-react";

import { createClawRun } from "@/lib/api";
import { PixelButton, Sprite, WindowCard } from "@/components/ui/pixel";

// /claw/new is the entrance to a new Claw worker run. Mirrors /code/new but
// simpler: one brief textarea + example-task chips + Begin. On submit we
// POST createClawRun and push to /claw/{id} where the worker desk + report
// live. Entrances are CSS-driven (animate-rise) so nothing strands at
// opacity:0 under React Strict Mode.

const STARTERS = [
  "调研 2026 国产大模型价格战,出一份对比报告,含价格表和趋势判断",
  "对比 Next.js、Remix、SvelteKit 三个框架,给选型建议",
  "整理 2026 年 AI Agent 领域的关键进展,按主题分节",
  "分析电动车与燃油车的全生命周期成本,给一张对比表",
];

export default function NewClawPage() {
  return (
    <Suspense fallback={null}>
      <NewClawForm />
    </Suspense>
  );
}

function NewClawForm() {
  const router = useRouter();
  const search = useSearchParams();
  const [prompt, setPrompt] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
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
      const res = await createClawRun(t);
      router.push(`/claw/${res.job_id}?session=${res.session_id}`);
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
            ~/claw/new
          </span>
        </div>
      </header>

      <div className="relative z-10 mx-auto flex max-w-3xl flex-col px-6 pt-20 md:px-12">
        <div className="relative mb-10 animate-rise">
          <p className="mb-3 font-mono text-[10px] font-semibold uppercase tracking-wide text-muted">
            Brief · 给 Claw 派个活
          </p>
          <h1 className="font-mono text-[34px] font-extrabold leading-[1.12] tracking-tight text-ink md:text-[44px]">
            交给
            <span className="relative whitespace-nowrap text-accent">
              AI 员工
              <span className="absolute -bottom-0.5 left-0 right-0 -z-10 h-3 bg-accent-soft" />
            </span>
            去办
          </h1>
          <p className="mt-3 font-mono text-[13px] text-ink-2">
            它会先拆任务、逐项推进、联网查证,最后交付一份可下载的报告 — 全程你都看得见。
          </p>
          <div className="pointer-events-none absolute -top-3 right-0 hidden rotate-6 md:block">
            <Sprite name="clapper" size={44} />
          </div>
        </div>

        <form onSubmit={submit} className="flex animate-rise flex-col gap-6" style={{ animationDelay: "0.08s" }}>
          <WindowCard
            title="~/claw — 一句话,派给 AI 员工去完成"
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
              placeholder='例: "调研 2026 国产大模型价格战,出一份对比报告,含价格表和趋势判断"'
              className="w-full resize-none bg-transparent px-5 pb-3 pt-4 text-[16px] leading-relaxed text-ink placeholder:text-muted focus:outline-none disabled:opacity-50"
            />

            <div className="flex items-center justify-end gap-3 border-t-2 border-line px-4 py-3">
              {err ? (
                <span className="mr-auto font-mono text-[12px] font-semibold text-[#a23a2a]">{err}</span>
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
                <span>{submitting ? "派活中" : "开工"}</span>
              </PixelButton>
            </div>
          </WindowCard>

          <p className="font-mono text-[11px] text-muted">⌘ / Ctrl + Enter 提交 · 示例任务 ↓</p>

          <div className="mt-1 flex flex-wrap gap-2">
            {STARTERS.map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setPrompt(s)}
                className="rounded-pixel border-2 border-line-2 bg-surface px-3 py-1.5 text-left font-mono text-[12.5px] text-ink-2 transition-all hover:-translate-x-[1px] hover:-translate-y-[1px] hover:border-ink hover:text-ink hover:shadow-pixel-sm"
              >
                {s}
              </button>
            ))}
          </div>
        </form>
      </div>
    </main>
  );
}
