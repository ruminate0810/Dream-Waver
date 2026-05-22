import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Dream-Waver — AI Multi-Agent Platform",
  description: "Manus / Genspark-style multi-agent platform. PPT generation, then Sheets, Docs, Code.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
