"use client";

import { useEffect, useRef, useState } from "react";
import {
  Briefcase,
  Film,
  Frame,
  Image as ImageIcon,
  LayoutGrid,
  Megaphone,
  Moon,
  Package,
  Palette,
  Sparkles,
  Sun,
  User,
  Users,
  Video,
  X,
} from "lucide-react";
import clsx from "clsx";

import { CATEGORY_LABEL, SKILLS, skillsByCategory, type Skill } from "./skills";

// SkillsLibrary is the "design task picker" popover the chat opens
// from its Skills button. Browsing by category. Clicking a card
// activates the skill — the parent pre-fills the input with the
// skill's example prompt, switches the chat into "skill mode", and
// changes the model/aspect defaults to the skill's recommendations.
//
// Mirrors open-design's skill-driven flow without their CLI-agent
// stack: every skill here is just a curated scaffold + recommended
// settings + suggested follow-ups, all resolved client-side.

// Icon lookup — we statically import only the icons referenced by the
// skill catalogue to keep the bundle lean. Add new icons here as new
// skills need them.
const ICON_BY_NAME: Record<
  string,
  React.ComponentType<{ size?: number; className?: string }>
> = {
  briefcase: Briefcase,
  film: Film,
  frame: Frame,
  image: ImageIcon,
  "layout-grid": LayoutGrid,
  megaphone: Megaphone,
  moon: Moon,
  package: Package,
  palette: Palette,
  sparkles: Sparkles,
  sun: Sun,
  user: User,
  users: Users,
  video: Video,
};

function SkillIcon({
  name,
  className,
  size = 14,
}: {
  name: string;
  className?: string;
  size?: number;
}) {
  const Comp = ICON_BY_NAME[name] ?? Sparkles;
  return <Comp size={size} className={className} />;
}

export type SkillsLibraryProps = {
  /** Currently-active skill id, or null when chat is in free-form
   *  mode. Drives the "Active" badge on the matching card. */
  activeSkillId: string | null;
  /** Called when the user picks a skill. Parent should:
   *   - set its active skill state to the picked id
   *   - pre-fill the chat textarea with skill.examplePrompt
   *   - update model + aspect to the skill's recommendations
   *   - close the library popover */
  onPick: (skill: Skill) => void;
  /** Clear the active skill (return chat to free-form mode). */
  onClear: () => void;
};

export function SkillsLibrary({
  activeSkillId,
  onPick,
  onClear,
}: SkillsLibraryProps) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const containerRef = useRef<HTMLDivElement | null>(null);

  // Outside-click closes.
  useEffect(() => {
    if (!open) return;
    function onDocClick(ev: MouseEvent) {
      if (!containerRef.current) return;
      if (containerRef.current.contains(ev.target as Node)) return;
      setOpen(false);
    }
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [open]);

  const groups = skillsByCategory();
  const filterLower = filter.trim().toLowerCase();
  const visibleGroups = filterLower
    ? groups
        .map((g) => ({
          ...g,
          skills: g.skills.filter(
            (s) =>
              s.label.toLowerCase().includes(filterLower) ||
              s.description.toLowerCase().includes(filterLower),
          ),
        }))
        .filter((g) => g.skills.length > 0)
    : groups;

  const activeSkill = activeSkillId
    ? SKILLS.find((s) => s.id === activeSkillId)
    : null;

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        title={
          activeSkill
            ? `Active skill: ${activeSkill.label} — click to change`
            : "Pick a design task to scaffold your prompt"
        }
        className={clsx(
          "inline-flex items-center gap-1 rounded border px-1.5 py-0.5 font-mono text-[9px] uppercase tracking-[0.12em] transition",
          activeSkill
            ? "border-fuchsia-500 bg-fuchsia-50 text-fuchsia-700"
            : open
              ? "border-zinc-400 bg-zinc-100 text-zinc-900"
              : "border-zinc-200 text-zinc-600 hover:border-zinc-300 hover:text-zinc-900",
        )}
      >
        <LayoutGrid size={10} />
        <span>{activeSkill ? activeSkill.label : "Skills"}</span>
        {activeSkill && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onClear();
            }}
            title="Clear active skill"
            className="ml-0.5 text-fuchsia-400 hover:text-fuchsia-700"
          >
            <X size={9} />
          </button>
        )}
      </button>

      {open && (
        // Anchored to the chip's RIGHT edge (right-0) so the popover
        // grows leftward into the chat panel rather than rightward off
        // the viewport. Width is capped to fit common chat widths
        // (340-480px); the inner grid uses min-w-0 columns so the
        // skill cards truncate cleanly instead of forcing overflow.
        <div className="absolute bottom-full right-0 z-30 mb-1 w-80 max-w-[calc(100vw-2rem)] rounded-md border border-zinc-200 bg-white p-2 shadow-md">
          <header className="mb-2 flex items-center gap-2 px-1">
            <input
              type="text"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Search 16 skills…"
              className="flex-1 rounded border border-zinc-200 px-2 py-1 text-[12px] focus:border-zinc-400 focus:outline-none"
            />
            <button
              type="button"
              onClick={() => setOpen(false)}
              className="text-zinc-400 hover:text-zinc-700"
              aria-label="Close skills library"
            >
              <X size={13} />
            </button>
          </header>

          <div className="max-h-[60vh] space-y-2 overflow-y-auto pr-1">
            {visibleGroups.length === 0 ? (
              <p className="px-2 py-6 text-center text-[12px] text-zinc-400">
                No skills match &quot;{filter}&quot;.
              </p>
            ) : (
              visibleGroups.map((group) => (
                <div key={group.category}>
                  <p className="mb-0.5 px-1 font-mono text-[9px] uppercase tracking-[0.14em] text-zinc-400">
                    {group.label}
                  </p>
                  <div className="grid grid-cols-2 gap-1">
                    {group.skills.map((skill) => {
                      const isActive = skill.id === activeSkillId;
                      return (
                        <button
                          key={skill.id}
                          type="button"
                          onClick={() => {
                            onPick(skill);
                            setOpen(false);
                          }}
                          className={clsx(
                            // min-w-0 lets the inner flex children's
                            // `truncate` actually work inside a grid
                            // column (grid items default to min-width:
                            // auto which respects content's natural
                            // width and breaks truncation).
                            "flex min-w-0 flex-col items-start gap-0.5 overflow-hidden rounded border px-2 py-1.5 text-left transition",
                            isActive
                              ? "border-fuchsia-500 bg-fuchsia-50"
                              : "border-zinc-200 hover:border-zinc-400 hover:bg-zinc-50",
                          )}
                        >
                          <div className="flex w-full min-w-0 items-center gap-1.5">
                            <SkillIcon
                              name={skill.icon}
                              className={clsx(
                                "shrink-0",
                                isActive ? "text-fuchsia-600" : "text-zinc-500",
                              )}
                            />
                            <span
                              className={clsx(
                                "min-w-0 flex-1 truncate text-[12px] font-medium",
                                isActive ? "text-fuchsia-900" : "text-zinc-800",
                              )}
                            >
                              {skill.label}
                            </span>
                            <span className="shrink-0 font-mono text-[8px] uppercase tracking-[0.1em] text-zinc-400">
                              {skill.aspect}
                            </span>
                          </div>
                          <p className="line-clamp-2 w-full text-[10px] leading-tight text-zinc-500">
                            {skill.description}
                          </p>
                        </button>
                      );
                    })}
                  </div>
                </div>
              ))
            )}
          </div>

          {activeSkill && (
            <footer className="mt-2 border-t border-zinc-100 px-1 pt-2">
              <button
                type="button"
                onClick={() => {
                  onClear();
                  setOpen(false);
                }}
                className="w-full rounded border border-zinc-200 px-2 py-1 text-[11px] text-zinc-600 transition hover:border-zinc-400 hover:text-zinc-900"
              >
                Clear active skill ({activeSkill.label})
              </button>
            </footer>
          )}
        </div>
      )}
    </div>
  );
}
