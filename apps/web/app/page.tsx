import { Sidebar } from "@/components/workspace/Sidebar";
import { Hero } from "@/components/workspace/Hero";
import { AgentGrid } from "@/components/workspace/AgentGrid";
import { TopRight } from "@/components/workspace/TopRight";

// Workspace homepage — Genspark-4.0 style: fixed left rail of capability
// icons, big centered prompt input, agent grid underneath. Today only
// "AI 幻灯片" routes anywhere; the rest carry "敬请期待" subtitles so the
// surface stays honest about what's shipped.
export default function Home() {
  return (
    <div className="min-h-screen bg-[#FAFAFA]">
      <Sidebar />
      <TopRight />
      <main className="ml-[72px] flex min-h-screen flex-col">
        <Hero />
        <AgentGrid />
        <footer className="mt-auto px-10 py-8 text-xs text-zinc-400">
          © 2026 Dream-Waver · MIT · Powered by Go + Rust + DeepSeek
        </footer>
      </main>
    </div>
  );
}
