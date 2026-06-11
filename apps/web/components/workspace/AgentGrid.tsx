"use client";

import Link from "next/link";
import { useRef } from "react";
import { gsap } from "gsap";
import { useGSAP } from "@gsap/react";
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

import { DUR, EASE } from "@/lib/motion";

gsap.registerPlugin(useGSAP);

// AgentGrid — the 5-section capability rail, pixel re-skin. Slides / 设计 /
// 代码 / 视频 are wired; every other tile renders the same pixel card but
// is greyed + non-navigating with a 敬请期待 label. GSAP entrance +
// per-tile magnetic hover unchanged.

type Tile = {
  label: string;
  icon: React.ComponentType<{ size?: number; strokeWidth?: number }>;
  color: string; // tailwind text color for the icon
  href: string;
  comingSoon?: boolean;
};

type Group = { label: string; expandable?: boolean; tiles: Tile[] };

const GROUPS: Group[] = [
  {
    label: "AI 员工",
    tiles: [{ label: "Claw", icon: Sparkles, color: "text-vermillion", href: "/claw/new" }],
  },
  {
    label: "办公套件",
    expandable: true,
    tiles: [
      { label: "AI 幻灯片", icon: Presentation, color: "text-orange-500", href: "/slides/new" },
      { label: "AI 表格", icon: Sheet, color: "text-emerald-500", href: "#", comingSoon: true },
      { label: "AI 文档", icon: FileText, color: "text-blue-500", href: "#", comingSoon: true },
    ],
  },
  {
    label: "设计与代码",
    tiles: [
      { label: "设计", icon: Layout, color: "text-ink", href: "/design" },
      { label: "代码", icon: Code2, color: "text-sky-500", href: "/code/new" },
    ],
  },
  {
    label: "内容创作",
    expandable: true,
    tiles: [
      { label: "AI 聊天", icon: MessageSquare, color: "text-indigo-500", href: "#", comingSoon: true },
      { label: "AI 图片", icon: ImageIcon, color: "text-fuchsia-500", href: "#", comingSoon: true },
      { label: "AI 视频", icon: Film, color: "text-teal-600", href: "/video/new" },
    ],
  },
  {
    label: "工具",
    tiles: [
      { label: "AI 会议纪要", icon: ClipboardList, color: "text-gold", href: "#", comingSoon: true },
      { label: "所有智能体", icon: Sparkles, color: "text-accent", href: "#", comingSoon: true },
    ],
  },
];

export function AgentGrid() {
  // Entrance is CSS-driven (see .dw-grid-group in globals.css) so it can't
  // strand at opacity:0 under React Strict Mode. GSAP stays for the
  // per-tile magnetic hover (which can't strand — it only fires on
  // pointer events).
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
    <div className="dw-grid-group flex flex-col items-stretch border-l-2 border-line px-4 first:border-l-0 first:pl-0">
      <div className="mb-4 flex items-center gap-1 font-pixel text-[0.58rem] tracking-wide text-muted">
        <span>{group.label}</span>
        {group.expandable && <ChevronDown size={11} strokeWidth={2} />}
      </div>
      <div className={clsx("flex items-start justify-center", group.tiles.length > 1 && "gap-3")}>
        {group.tiles.map((t) => (
          <AgentTile key={t.label} tile={t} />
        ))}
      </div>
    </div>
  );
}

function AgentTile({ tile }: { tile: Tile }) {
  const Icon = tile.icon;
  const iconRef = useRef<HTMLDivElement | null>(null);
  const tileRef = useRef<HTMLDivElement | null>(null);

  useGSAP(
    (_ctx, contextSafe) => {
      if (tile.comingSoon || !iconRef.current || !tileRef.current || !contextSafe) return;
      const onEnter = contextSafe(() => {
        gsap.to(iconRef.current, { y: -3, scale: 1.06, duration: DUR.micro, ease: EASE.magnetic });
      });
      const onLeave = contextSafe(() => {
        gsap.to(iconRef.current, { y: 0, scale: 1, duration: DUR.micro, ease: EASE.feedback });
      });
      const el = tileRef.current;
      el.addEventListener("pointerenter", onEnter);
      el.addEventListener("pointerleave", onLeave);
      return () => {
        el.removeEventListener("pointerenter", onEnter);
        el.removeEventListener("pointerleave", onLeave);
      };
    },
    { scope: tileRef },
  );

  const inner = (
    <div ref={tileRef} className="dw-tile group flex flex-col items-center gap-2 p-1">
      <div
        ref={iconRef}
        className={clsx(
          "grid h-14 w-14 place-items-center rounded-pixel border-2 will-change-transform",
          tile.comingSoon
            ? "border-line-2 bg-surface-2 text-line-2"
            : clsx("border-ink bg-surface shadow-pixel-sm transition-shadow group-hover:shadow-pixel", tile.color),
        )}
      >
        <Icon size={22} strokeWidth={1.9} />
      </div>
      <span className={clsx("text-[13px] font-semibold", tile.comingSoon ? "text-muted" : "text-ink")}>
        {tile.label}
      </span>
      {tile.comingSoon && (
        <span className="font-pixel text-[0.5rem] tracking-wide text-line-2">敬请期待</span>
      )}
    </div>
  );

  if (tile.comingSoon) {
    return <div className="cursor-not-allowed opacity-95">{inner}</div>;
  }
  return <Link href={tile.href}>{inner}</Link>;
}
