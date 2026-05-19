import type { NextConfig } from "next";
import { PHASE_DEVELOPMENT_SERVER } from "next/constants";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const apiBaseUrl = process.env.API_BASE_URL || "http://127.0.0.1:3000";
const webDir = dirname(fileURLToPath(import.meta.url));
const version = readFileSync(resolve(webDir, "../VERSION"), "utf8").trim() || "dev";

export default function nextConfig(phase: string): NextConfig {
  const isDev = phase === PHASE_DEVELOPMENT_SERVER;
  return {
    typescript: {
      ignoreBuildErrors: true,
    },
    env: {
      NEXT_PUBLIC_APP_VERSION: version,
    },
    async rewrites() {
      return isDev ? [{ source: "/api/:path*", destination: `${apiBaseUrl}/api/:path*` }] : [];
    },
  };
}
