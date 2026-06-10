import { Sidebar } from "@/components/workspace/Sidebar";
import { Hero } from "@/components/workspace/Hero";
import { AgentGrid } from "@/components/workspace/AgentGrid";
import { TopRight } from "@/components/workspace/TopRight";
import { RecentDecks } from "@/components/workspace/RecentDecks";

// Workspace homepage — Genspark-4.0 style: fixed left rail of capability
// icons, big centered prompt input, agent grid underneath. Today only
// "AI 幻灯片" routes anywhere; the rest carry "敬请期待" subtitles so the
// surface stays honest about what's shipped.
export default function Home() {
  return (
    <div className="min-h-screen">
      <Sidebar />
      <TopRight />
      <main className="relative z-[1] ml-[72px] flex min-h-screen flex-col">
        <Hero />
        <AgentGrid />
        <RecentDecks />
        <footer className="mt-auto px-10 py-8 font-pixel text-[0.55rem] tracking-wide text-muted">
          © 2026 DREAM-WAVER · MADE WITH PIXELS ✦ · GO + RUST + DEEPSEEK
        </footer>
      </main>
    </div>
  );
}
