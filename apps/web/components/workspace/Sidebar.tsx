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

// Sidebar mirrors Genspark 4.0's narrow capability rail. Two items are
// wired (新建 / 首页) — everything else is a future-skill placeholder
// kept visible so the workspace shape reads correctly, but disabled so
// clicking doesn't navigate to a dead anchor.
type Item = {
  label: string;
  href?: string;
  icon: React.ComponentType<{ size?: number; strokeWidth?: number }>;
  match?: (path: string) => boolean;
  // Soon-ness — when true the item paints grey, doesn't navigate, and
  // shows a tooltip on hover. The shape is intentional: this is a
  // "promise of a future skill" not "broken link".
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
  return (
    <aside className="fixed left-0 top-0 z-20 flex h-screen w-[72px] flex-col items-center justify-between border-r border-zinc-100 bg-white py-4">
      {/* Brand mark — always links home. */}
      <div className="flex flex-col items-center gap-4">
        <Link
          href="/"
          aria-label="Dream-Waver home"
          className="flex h-10 w-10 items-center justify-center rounded-xl bg-gradient-to-br from-indigo-500 to-fuchsia-500 text-white shadow-sm"
        >
          <Sparkles size={20} strokeWidth={2.4} />
        </Link>

        <nav className="mt-2 flex flex-col items-center gap-1">
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
          className="flex h-9 w-9 cursor-not-allowed items-center justify-center rounded-full text-zinc-300"
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
        className="group flex w-14 cursor-not-allowed flex-col items-center gap-1 rounded-lg py-2 text-[11px] font-medium text-zinc-300"
      >
        <Icon size={20} strokeWidth={1.8} />
        <span>{item.label}</span>
      </button>
    );
  }

  return (
    <Link
      href={item.href ?? "#"}
      className={clsx(
        "group flex w-14 flex-col items-center gap-1 rounded-lg py-2 text-[11px] font-medium transition-colors",
        active
          ? "bg-zinc-100 text-ink"
          : "text-zinc-500 hover:bg-zinc-50 hover:text-ink",
      )}
    >
      <Icon size={20} strokeWidth={1.8} />
      <span>{item.label}</span>
    </Link>
  );
}
