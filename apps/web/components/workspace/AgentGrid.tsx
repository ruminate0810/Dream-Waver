"use client";

import Link from "next/link";
import {
  Presentation,
  Sheet,
  FileText,
  Layout,
  Code2,
  MessageSquare,
  Image as ImageIcon,
  Film,
  ClipboardList,
  Sparkles,
  ChevronDown,
} from "lucide-react";
import clsx from "clsx";

// AgentGrid mirrors Genspark's 5-section bottom rail. Slides is the only
// capability we've actually shipped — every other tile renders the same
// markup but its href is `#` and onClick shows a tiny "coming soon" badge
// underneath. Keeps the visual paradigm honest about today's surface.

type Tile = {
  label: string;
  icon: React.ComponentType<{ size?: number; strokeWidth?: number }>;
  color: string; // tailwind text class for the icon
  href: string;
  comingSoon?: boolean;
};

type Group = {
  label: string;
  expandable?: boolean;
  tiles: Tile[];
};

const GROUPS: Group[] = [
  {
    label: "AI 员工",
    tiles: [
      { label: "Claw", icon: Sparkles, color: "text-red-500", href: "#", comingSoon: true },
    ],
  },
  {
    label: "办公套件",
    expandable: true,
    tiles: [
      { label: "AI 幻灯片", icon: Presentation, color: "text-orange-500", href: "/slides/new" },
      { label: "AI 表格",   icon: Sheet,        color: "text-emerald-500", href: "#", comingSoon: true },
      { label: "AI 文档",   icon: FileText,     color: "text-blue-500", href: "#", comingSoon: true },
    ],
  },
  {
    label: "设计与代码",
    tiles: [
      { label: "设计", icon: Layout, color: "text-zinc-800", href: "#", comingSoon: true },
      { label: "代码", icon: Code2,  color: "text-sky-500", href: "/code/new" },
    ],
  },
  {
    label: "内容创作",
    expandable: true,
    tiles: [
      { label: "AI 聊天", icon: MessageSquare, color: "text-indigo-500", href: "#", comingSoon: true },
      { label: "AI 图片", icon: ImageIcon,     color: "text-fuchsia-500", href: "#", comingSoon: true },
      { label: "AI 视频", icon: Film,          color: "text-teal-600", href: "/video/new" },
    ],
  },
  {
    label: "工具",
    tiles: [
      { label: "AI 会议纪要", icon: ClipboardList, color: "text-amber-500", href: "#", comingSoon: true },
      { label: "所有智能体", icon: Sparkles,        color: "text-violet-500", href: "#", comingSoon: true },
    ],
  },
];

export function AgentGrid() {
  return (
    <section className="mx-auto mt-20 w-full max-w-6xl px-6">
      <div className="grid grid-cols-2 gap-x-6 gap-y-8 md:grid-cols-3 lg:grid-cols-5">
        {GROUPS.map((g) => (
          <GroupBlock key={g.label} group={g} />
        ))}
      </div>
    </section>
  );
}

function GroupBlock({ group }: { group: Group }) {
  return (
    <div className="flex flex-col items-stretch border-l border-zinc-100 px-4 first:border-l-0 first:pl-0">
      <div className="mb-4 flex items-center gap-1 text-xs text-zinc-400">
        <span>{group.label}</span>
        {group.expandable && <ChevronDown size={12} strokeWidth={1.6} />}
      </div>
      <div className={clsx("flex", group.tiles.length > 1 ? "gap-3" : "", "items-start justify-center")}>
        {group.tiles.map((t) => (
          <AgentTile key={t.label} tile={t} />
        ))}
      </div>
    </div>
  );
}

function AgentTile({ tile }: { tile: Tile }) {
  const Icon = tile.icon;
  const inner = (
    <div
      className={clsx(
        "group flex flex-col items-center gap-2 rounded-lg p-2 transition-colors",
        !tile.comingSoon && "hover:bg-zinc-50",
      )}
    >
      <div
        className={clsx(
          "flex h-12 w-12 items-center justify-center rounded-xl border border-zinc-100 bg-white shadow-[0_1px_2px_rgba(0,0,0,0.04)]",
          tile.color,
        )}
      >
        <Icon size={22} strokeWidth={1.8} />
      </div>
      <span className="text-[13px] text-zinc-700">{tile.label}</span>
      {tile.comingSoon && (
        <span className="text-[10px] text-zinc-400">敬请期待</span>
      )}
    </div>
  );

  if (tile.comingSoon) {
    // Render as a non-link so the tile stays visually present but doesn't navigate.
    return <div className="cursor-not-allowed opacity-90">{inner}</div>;
  }
  return <Link href={tile.href}>{inner}</Link>;
}
