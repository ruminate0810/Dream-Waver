"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Plus,
  Home,
  Sparkles,
  Workflow,
  Users,
  HardDrive,
  MoreHorizontal,
  MessageCircle,
} from "lucide-react";
import clsx from "clsx";

import { Sprite } from "@/components/ui/pixel";

// Sidebar — narrow capability rail, pixel re-skin. Two items are wired
// (新建 / 首页); everything else is a future-skill placeholder kept visible
// (grey, non-navigating) so the workspace shape reads correctly.
//
// GSAP entrance unchanged: the rail slides in from -72px, the brand mark
// scales up + runs a slow "breath" loop, nav items stagger-fade.

type Item = {
  label: string;
  href?: string;
  icon: React.ComponentType<{ size?: number; strokeWidth?: number }>;
  match?: (path: string) => boolean;
  soon?: boolean;
};

const TOP: Item[] = [
  { label: "新建", href: "/slides/new", icon: Plus },
  { label: "首页", href: "/", icon: Home, match: (p) => p === "/" },
  { label: "Claw", icon: Sparkles, soon: true },
  { label: "工作流", icon: Workflow, soon: true },
  { label: "团队", icon: Users, soon: true },
  { label: "云盘", icon: HardDrive, soon: true },
  { label: "更多", icon: MoreHorizontal, soon: true },
];

export function Sidebar() {
  const path = usePathname() ?? "/";

  // Entrance is CSS-driven (.dw-sidebar-nav-item in globals.css) so nav
  // items can't strand at opacity:0 under React Strict Mode's dev mount.
  return (
    <aside className="fixed left-0 top-0 z-20 flex h-screen w-[72px] flex-col items-center justify-between border-r-2 border-ink bg-surface py-4">
      <div className="flex flex-col items-center gap-4">
        <Link
          href="/"
          aria-label="Dream-Waver home"
          className="dw-sidebar-brand grid h-10 w-10 place-items-center rounded-pixel border-2 border-ink bg-surface shadow-pixel-sm"
        >
          <Sprite name="boombox" size={16} />
        </Link>

        <nav className="mt-2 flex flex-col items-center gap-1.5">
          {TOP.map((item) => {
            const active = item.match ? item.match(path) : item.href === path;
            return <NavButton key={item.label} item={item} active={active} />;
          })}
        </nav>
      </div>

      <div className="flex flex-col items-center gap-2">
        <button
          aria-label="Help & feedback (coming soon)"
          title="反馈 · 即将上线"
          disabled
          className="grid h-9 w-9 cursor-not-allowed place-items-center rounded-pixel text-line-2"
        >
          <MessageCircle size={18} />
        </button>
      </div>
    </aside>
  );
}

function NavButton({ item, active }: { item: Item; active: boolean }) {
  const Icon = item.icon;

  if (item.soon) {
    return (
      <button
        type="button"
        title={`${item.label} · 即将上线`}
        aria-label={`${item.label} (coming soon)`}
        disabled
        className="dw-sidebar-nav-item flex w-14 cursor-not-allowed flex-col items-center gap-1 rounded-pixel border-2 border-transparent py-2 text-[10px] font-medium text-line-2"
      >
        <Icon size={19} strokeWidth={1.8} />
        <span>{item.label}</span>
      </button>
    );
  }

  return (
    <Link
      href={item.href ?? "#"}
      className={clsx(
        "dw-sidebar-nav-item flex w-14 flex-col items-center gap-1 rounded-pixel border-2 py-2 text-[10px] font-semibold transition-all",
        active
          ? "border-ink bg-accent-soft text-ink shadow-pixel-sm"
          : "border-transparent text-muted hover:border-line-2 hover:bg-surface-2 hover:text-ink",
      )}
    >
      <Icon size={19} strokeWidth={1.8} />
      <span>{item.label}</span>
    </Link>
  );
}
