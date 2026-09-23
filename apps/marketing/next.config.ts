import { existsSync } from "node:fs";
import { join } from "node:path";

import type { NextConfig } from "next";

// One .env at the repository root serves every app in local development.
const rootEnv = join(import.meta.dirname, "..", "..", ".env");
if (existsSync(rootEnv)) process.loadEnvFile(rootEnv);

const isDev = process.env.NODE_ENV !== "production";
const apiOrigin = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8090";

/*
 * The site is static, so scripts cannot carry a per-request nonce; inline
 * scripts are limited to what Next.js itself emits. Nothing user-supplied is
 * ever rendered as HTML. `connect-src` pins network access to the Mise API.
 */
const csp = [
  "default-src 'self'",
  `script-src 'self' 'unsafe-inline'${isDev ? " 'unsafe-eval'" : ""}`,
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: blob:",
  "font-src 'self'",
  `connect-src 'self' ${apiOrigin}${isDev ? " ws: http://localhost:*" : ""}`,
  "frame-ancestors 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "object-src 'none'",
].join("; ");

const securityHeaders = [
  { key: "Content-Security-Policy", value: csp },
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "DENY" },
  // Keeps tokens in /verify, /welcome and /unsubscribe URLs out of Referer headers.
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), interest-cohort=()" },
  ...(isDev ? [] : [{ key: "Strict-Transport-Security", value: "max-age=63072000; includeSubDomains; preload" }]),
];

const nextConfig: NextConfig = {
  output: "standalone",
  outputFileTracingRoot: join(import.meta.dirname, "..", ".."),
  poweredByHeader: false,
  reactStrictMode: true,
  transpilePackages: ["@mise/ui", "@mise/validation", "@mise/api-client", "@mise/analytics"],
  images: {
    formats: ["image/avif", "image/webp"],
    deviceSizes: [420, 640, 828, 1080, 1280, 1680, 2048],
  },
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
};

export default nextConfig;
