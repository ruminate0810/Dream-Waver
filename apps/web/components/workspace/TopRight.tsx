"use client";

import { PixelButton } from "@/components/ui/pixel";

// Sign-in / sign-up pinned top-right. Auth isn't wired yet — buttons stay
// visible (so the workspace reads complete) but disabled with an honest
// tooltip. Pixel re-skin.
export function TopRight() {
  return (
    <div className="fixed right-8 top-6 z-30 flex items-center gap-2.5">
      <PixelButton variant="ghost" disabled title="登录 · 即将上线">
        登录
      </PixelButton>
      <PixelButton variant="primary" disabled title="注册 · 即将上线">
        注册
      </PixelButton>
    </div>
  );
}
