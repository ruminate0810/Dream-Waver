/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Proxy /api/* to the Go orchestrator during local dev so the frontend can
  // use same-origin fetches and websockets without CORS gymnastics.
  async rewrites() {
    const api = process.env.NEXT_PUBLIC_API_BASE || "http://localhost:8080";
    return [{ source: "/api/v1/:path*", destination: `${api}/api/v1/:path*` }];
  },
};
export default nextConfig;
