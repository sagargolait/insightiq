import type { NextConfig } from "next";

// The Go API has no CORS and sets a SameSite=Lax session cookie, so the
// browser talks to it same-origin through this rewrite.
const apiOrigin = process.env.API_ORIGIN ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  async rewrites() {
    return [{ source: "/api/v1/:path*", destination: `${apiOrigin}/api/v1/:path*` }];
  },
};

export default nextConfig;
