/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Default proxy timeout is 30s — way too short for the synchronous
  // design endpoints. NanoBanana typically lands in 30s but spikes to
  // 60s+, Seedance i2v runs 60-180s for 720p and can hit 240s+ for
  // 1080p, sometimes more when the upstream gateway is under load.
  // 15 min is the conservative upper bound that won't dangle a
  // forgotten tab forever but never trips a real generation.
  experimental: {
    proxyTimeout: 900_000,
  },
  // Proxy /api/* to the Go orchestrator during local dev so the frontend can
  // use same-origin fetches and websockets without CORS gymnastics.
  async rewrites() {
    const api = process.env.NEXT_PUBLIC_API_BASE || "http://localhost:8080";
    return [{ source: "/api/v1/:path*", destination: `${api}/api/v1/:path*` }];
  },
};
export default nextConfig;
