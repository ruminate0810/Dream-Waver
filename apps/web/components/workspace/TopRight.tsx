"use client";

// Sign-in / sign-up buttons pinned to the top right. Auth isn't wired
// yet (Supabase/Clerk lands in the SaaS sprint). For now the buttons
// stay visible so the workspace reads complete, but they're disabled
// with an honest tooltip — no fake redirects.
export function TopRight() {
  return (
    <div className="fixed right-8 top-6 z-30 flex items-center gap-3">
      <button
        type="button"
        disabled
        title="登录 · 即将上线"
        className="cursor-not-allowed rounded-full bg-zinc-100 px-6 py-2 text-sm font-medium text-zinc-400"
      >
        登录
      </button>
      <button
        type="button"
        disabled
        title="注册 · 即将上线"
        className="cursor-not-allowed rounded-full border border-zinc-200 bg-white px-6 py-2 text-sm font-medium text-zinc-400"
      >
        注册
      </button>
    </div>
  );
}
