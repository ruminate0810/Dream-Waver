import type { Config } from "tailwindcss";

// Dream-Waver pixel / retro-terminal theme.
// Everything is tokenized so the whole app shifts by editing this file +
// the matching CSS variables in app/globals.css. Aesthetic: warm paper,
// monospace type, hard pixel shadows, violet accent.
export default {
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        // Mono is the workhorse — the whole UI is monospace (terminal vibe).
        // `sans` is aliased to mono so any un-migrated `font-sans` usage and
        // the default body text also render mono.
        mono: ["JetBrains Mono", "ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
        sans: ["JetBrains Mono", "ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
        // Silkscreen — crisp pixel face. Logo + tiny eyebrow labels only
        // (too blocky for long text).
        pixel: ["Silkscreen", "JetBrains Mono", "monospace"],
        // Legacy editorial serif, still referenced by a few un-migrated
        // surfaces via `.font-display`.
        serif: ["Fraunces", "DM Serif Display", "Georgia", "serif"],
      },
      colors: {
        paper: "#f3efe5",          // warm cream page
        "paper-2": "#ece6d8",      // deeper band
        surface: "#ffffff",
        "surface-2": "#faf8f2",
        ink: "#16140f",            // warm near-black (was cool slate)
        "ink-2": "#403a30",
        muted: "#938b7b",          // warm gray
        line: "#ddd5c3",           // warm hairline
        "line-2": "#c9bfa9",       // deeper hairline
        accent: "#6a55ff",         // violet / indigo (was blue)
        "accent-2": "#5743e6",
        "accent-soft": "#ece8ff",
        gold: "#e3a13a",           // queued / warning (named `gold` so it
                                   // doesn't clobber Tailwind's amber-* scale,
                                   // which the video surfaces still use)
        grass: "#3ea96a",          // done / synced
        vermillion: "#B5371E",     // legacy editorial accent
      },
      boxShadow: {
        // Hard offset shadows (0 blur) — the soul of the pixel look.
        pixel: "4px 4px 0 #16140f",
        "pixel-sm": "3px 3px 0 #16140f",
        "pixel-lg": "6px 6px 0 #16140f",
        "pixel-hover": "2px 2px 0 #16140f",
        "pixel-accent": "4px 4px 0 #6a55ff",
      },
      borderRadius: {
        pixel: "6px",
      },
      keyframes: {
        rise: {
          from: { opacity: "0", transform: "translateY(18px)" },
          to: { opacity: "1", transform: "none" },
        },
        pixpulse: {
          "0%,100%": { transform: "scale(1)", opacity: "1" },
          "50%": { transform: "scale(1.5)", opacity: ".5" },
        },
        wireflow: { to: { backgroundPosition: "12px 0" } },
        blink: { "50%": { opacity: "0" } },
      },
      animation: {
        rise: "rise .6s cubic-bezier(.16,1,.3,1) backwards",
        pixpulse: "pixpulse 1.1s ease-in-out infinite",
        wireflow: "wireflow 1.1s linear infinite",
        blink: "blink 1s steps(1) infinite",
      },
    },
  },
  plugins: [],
} satisfies Config;
