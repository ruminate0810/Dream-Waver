"use client";

import { useCallback, useEffect, useState } from "react";

import { WORKERS, type WorkerDef } from "./workers";
import { OFFICE_CONFIG } from "./officeSim";

// outfits.ts — the wardrobe. A handful of preset looks (shirt/pants/hair/
// skin combos) that can be applied to ANY worker from the stats popover and
// persist in localStorage, merged over the registry defaults at render time
// so every consumer (sprites, desks, flights) just uses the merged defs.
// "默认" (index -1 / absent) = the worker's built-in look from workers.ts.

export type Outfit = {
  name: string;
  shirt: string;
  shirtDark: string;
  pants: string;
  hair: string;
  hairKind: "short" | "long" | "none";
  skin?: string;
};

export const OUTFITS: Outfit[] = [
  { name: "薄荷", shirt: "#2bb6a3", shirtDark: "#1f8f80", pants: "#33424a", hair: "#2a2620", hairKind: "short" },
  { name: "落日", shirt: "#e8744f", shirtDark: "#c25a39", pants: "#4a3a35", hair: "#5a3a1e", hairKind: "short", skin: "#f3c9a8" },
  { name: "黑客", shirt: "#2c2c34", shirtDark: "#1d1d24", pants: "#16140f", hair: "#3ea96a", hairKind: "long" },
  { name: "樱花", shirt: "#f2a3c0", shirtDark: "#d57fa2", pants: "#6a5560", hair: "#7a4a23", hairKind: "long" },
  { name: "天空", shirt: "#5aa8e8", shirtDark: "#3f87c4", pants: "#2f3d52", hair: "#e3d9c2", hairKind: "short", skin: "#e8b48c" },
  { name: "柠檬", shirt: "#e8d44f", shirtDark: "#c4b034", pants: "#4f4a2e", hair: "#16140f", hairKind: "short" },
  { name: "葡萄", shirt: "#8a6fe8", shirtDark: "#6d54c4", pants: "#3a3354", hair: "#d9569b", hairKind: "long" },
  { name: "工装", shirt: "#7a8471", shirtDark: "#5f6858", pants: "#3a3531", hair: "#2a2620", hairKind: "short", skin: "#f3c9a8" },
];

type WardrobeMap = Record<string, number>; // workerKey → OUTFITS index

function load(): WardrobeMap {
  try {
    return JSON.parse(localStorage.getItem(OFFICE_CONFIG.storage.wardrobe) ?? "{}") as WardrobeMap;
  } catch {
    return {};
  }
}

// useWardrobe merges persisted outfit picks over the WORKERS registry and
// hands back a setter. Consumers render `defs` and never know the difference.
export function useWardrobe(): {
  defs: WorkerDef[];
  outfitOf: (key: string) => number; // -1 = default look
  setOutfit: (key: string, idx: number) => void; // -1 resets to default
} {
  const [map, setMap] = useState<WardrobeMap>({});
  useEffect(() => setMap(load()), []);

  const setOutfit = useCallback((key: string, idx: number) => {
    setMap((prev) => {
      const next = { ...prev };
      if (idx < 0) delete next[key];
      else next[key] = ((idx % OUTFITS.length) + OUTFITS.length) % OUTFITS.length;
      try {
        localStorage.setItem(OFFICE_CONFIG.storage.wardrobe, JSON.stringify(next));
      } catch {
        /* private mode etc. — wardrobe just won't persist */
      }
      return next;
    });
  }, []);

  const defs = WORKERS.map((w) => {
    const idx = map[w.key];
    if (idx === undefined) return w;
    const o = OUTFITS[idx];
    return { ...w, shirt: o.shirt, shirtDark: o.shirtDark, pants: o.pants, hair: o.hair, hairKind: o.hairKind, skin: o.skin ?? w.skin };
  });

  return { defs, outfitOf: (key) => map[key] ?? -1, setOutfit };
}
