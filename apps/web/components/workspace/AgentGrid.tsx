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

import { DUR, EASE, PREFERS_FULL_MOTION, STAGGER } from "@/lib/motion";

gsap.registerPlugin(useGSAP);

// AgentGrid mirrors Genspark's 5-section bottom rail. Slides is the only
// capability we've actually shipped — every other tile renders the same
// markup but its href is `#` and onClick shows a tiny "coming soon" badge
// underneath. Keeps the visual paradigm honest about today's surface.
//
// Sprint Y1 — GSAP entrance: groups cascade from below (8px rise), tiles
// inside each group stagger right-to-left so the eye lands on the next
// group's label before the icons paint in. Per-tile hover uses a magnetic
// micro-interaction: the icon container lifts 2px on pointer enter +
// scales 1.06, springs back on leave. Reduced-motion users get the same
// hover lift via CSS transition (already there) and skip the entrance.

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
      { label: "设计", icon: Layout, color: "text-zinc-800", href: "/design" },
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
  const sectionRef = useRef<HTMLElement | null>(null);

  useGSAP(
    () => {
      const mm = gsap.matchMedia();

      mm.add(PREFERS_FULL_MOTION, () => {
        // Whole grid arrives after the hero settles — delay 0.4s
        // matches the tail end of Hero's timeline so the page reads
        // as a single composed entrance, not five separate ones.
        gsap.from(".dw-grid-group", {
          y: 14,
          opacity: 0,
          stagger: 0.07,
          duration: DUR.secondary,
          ease: EASE.entrance,
          delay: 0.4,
        });
        gsap.from(".dw-tile", {
          y: 8,
          opacity: 0,
          stagger: STAGGER.tile,
          duration: DUR.micro,
          ease: EASE.feedback,
          delay: 0.55,
        });
      });

      mm.add("(prefers-reduced-motion: reduce)", () => {
        gsap.from(".dw-grid-group", {
          opacity: 0,
          stagger: 0.04,
          duration: 0.2,
          delay: 0.2,
        });
      });
    },
    { scope: sectionRef },
  );

  return (
    <section
      ref={sectionRef}
      className="mx-auto mt-20 w-full max-w-6xl px-6"
    >
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
    <div className="dw-grid-group flex flex-col items-stretch border-l border-zinc-100 px-4 first:border-l-0 first:pl-0">
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
  const iconRef = useRef<HTMLDivElement | null>(null);
  const tileRef = useRef<HTMLDivElement | null>(null);

  // Per-tile magnetic hover. Wrapped in useGSAP + contextSafe so the
  // pointer handlers get cleaned up on unmount (per gsap-react skill —
  // raw addEventListener inside useGSAP without contextSafe leaks).
  // Skipped entirely for coming-soon tiles; they're visual placeholders.
  useGSAP(
    (_ctx, contextSafe) => {
      if (tile.comingSoon || !iconRef.current || !tileRef.current || !contextSafe) {
        return;
      }
      const onEnter = contextSafe(() => {
        gsap.to(iconRef.current, {
          y: -3,
          scale: 1.06,
          duration: DUR.micro,
          ease: EASE.magnetic,
        });
      });
      const onLeave = contextSafe(() => {
        gsap.to(iconRef.current, {
          y: 0,
          scale: 1,
          duration: DUR.micro,
          ease: EASE.feedback,
        });
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
    <div
      ref={tileRef}
      className={clsx(
        "dw-tile group flex flex-col items-center gap-2 rounded-lg p-2 transition-colors",
        !tile.comingSoon && "hover:bg-zinc-50",
      )}
    >
      <div
        ref={iconRef}
        className={clsx(
          "flex h-12 w-12 items-center justify-center rounded-xl border border-zinc-100 bg-white shadow-[0_1px_2px_rgba(0,0,0,0.04)] will-change-transform",
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
