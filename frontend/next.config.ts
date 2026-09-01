import type { NextConfig } from "next";
import path from "node:path";

const nextConfig: NextConfig = {
  // Standalone output for Docker: creates minimal deployable app
  output: "standalone",
  // This repo tree contains other package-lock.json files; keep file tracing
  // rooted at the frontend directory instead of letting Next infer one.
  outputFileTracingRoot: path.join(import.meta.dirname),
  // All browser traffic targets this server only: /api/* is proxied server-side
  // to the Go API (API_URL, default http://localhost:8080). The Go API is
  // therefore never exposed to the browser directly — same-origin fetches, no CORS.
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${process.env.API_URL || "http://localhost:8080"}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
