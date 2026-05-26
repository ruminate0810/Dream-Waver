"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useRef } from "react";
import { gsap } from "gsap";
import { useGSAP } from "@gsap/react";
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

import { DUR, EASE, PREFERS_FULL_MOTION, STAGGER } from "@/lib/motion";

gsap.registerPlugin(useGSAP);

// Sidebar mirrors Genspark 4.0's narrow capability rail. Two items are
// wired (新建 / 首页) — everything else is a future-skill placeholder
// kept visible so the workspace shape reads correctly, but disabled so
// clicking doesn't navigate to a dead anchor.
//
// Sprint Y1 — GSAP entrance: the rail slides in from -72px (its own
// width) so it reads as material settling onto the page edge. Nav
// items stagger-fade in once the rail is seated. The brand mark gets
// a slow continuous "breath" loop — barely-perceptible scale 1.0→1.03,
// 4s symmetric, sine.inOut — to make the otherwise-static logo feel
// alive without competing with anything else on the page.

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
  const asideRef = useRef<HTMLElement | null>(null);

  useGSAP(
    () => {
      const mm = gsap.matchMedia();

      mm.add(PREFERS_FULL_MOTION, () => {
        const tl = gsap.timeline({
          defaults: { ease: EASE.entrance, duration: DUR.entrance },
        });
        // Whole rail slides in from off-screen left.
        tl.from(asideRef.current, { x: -72, opacity: 0 })
          // Brand mark scales up after the rail seats.
          .from(
            ".dw-sidebar-brand",
            { scale: 0.6, opacity: 0, duration: DUR.secondary, ease: EASE.feedback },
            "-=0.35",
          )
          // Nav items cascade — staggered, very quick.
          .from(
            ".dw-sidebar-nav-item",
            {
              y: 8,
              opacity: 0,
              duration: DUR.micro,
              stagger: STAGGER.tile,
              ease: EASE.feedback,
            },
            "-=0.25",
          );

        // Brand mark breath loop — once the entrance settles. Yoyo so
        // it inhales / exhales symmetrically. Lasts forever; useGSAP's
        // cleanup kills it on unmount.
        gsap.to(".dw-sidebar-brand", {
          scale: 1.035,
          duration: DUR.breath / 2,
          ease: EASE.breath,
          yoyo: true,
          repeat: -1,
          delay: 1.2,
        });
      });

      // Reduced motion: opacity-only, no transforms, no breath loop.
      mm.add("(prefers-reduced-motion: reduce)", () => {
        gsap.from(asideRef.current, { opacity: 0, duration: 0.2 });
        gsap.from(".dw-sidebar-nav-item", {
          opacity: 0,
          duration: 0.15,
          stagger: 0.02,
        });
      });
    },
    { scope: asideRef },
  );

  return (
    <aside
      ref={asideRef}
      className="fixed left-0 top-0 z-20 flex h-screen w-[72px] flex-col items-center justify-between border-r border-zinc-100 bg-white py-4"
    >
      {/* Brand mark — always links home. */}
      <div className="flex flex-col items-center gap-4">
        <Link
          href="/"
          aria-label="Dream-Waver home"
          className="dw-sidebar-brand flex h-10 w-10 items-center justify-center rounded-xl bg-gradient-to-br from-indigo-500 to-fuchsia-500 text-white shadow-sm"
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
        className="dw-sidebar-nav-item group flex w-14 cursor-not-allowed flex-col items-center gap-1 rounded-lg py-2 text-[11px] font-medium text-zinc-300"
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
        "dw-sidebar-nav-item group flex w-14 flex-col items-center gap-1 rounded-lg py-2 text-[11px] font-medium transition-colors",
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
