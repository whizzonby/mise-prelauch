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
  // The floating dev badge covers real controls in the bottom-left corner.
  devIndicators: false,
  reactStrictMode: true,
  transpilePackages: ["@mise/ui", "@mise/validation", "@mise/api-client", "@mise/analytics"],
  images: {
    // WebP only. AVIF files are smaller but take seconds each to encode on a
    // small server, and a page asks for twenty at once: the first visitor at
    // each screen size saw empty frames. WebP encodes several times faster.
    formats: ["image/webp"],
    // Few widths, so there are few variants to create (and to warm after a deploy).
    deviceSizes: [640, 960, 1280, 1920],
    imageSizes: [256, 384],
    // The photographs are imported files with content-hashed names: a given
    // URL never changes, so an optimised copy can be kept for a year.
    minimumCacheTTL: 31536000,
  },
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
  /*
   * Optional same-origin API. When API_PROXY_TARGET is set at build time (and
   * NEXT_PUBLIC_API_URL is empty), the browser calls /api/v1/... on the site's
   * own address and this server forwards it to the API. Used when the site is
   * shared through a single tunnel (docker-compose.share.yml); in AWS the
   * browser calls the API's own host instead.
   *
   * Only the endpoints the public site uses are forwarded. The admin API is
   * deliberately left out, so it is never reachable through the public site.
   */
  async rewrites() {
    const target = process.env.API_PROXY_TARGET?.replace(/\/$/, "");
    if (!target) return [];
    return ["/api/v1/leads/:path*", "/api/v1/referrals/:path*", "/api/v1/events", "/api/v1/health", "/api/v1/ready"].map(
      (source) => ({ source, destination: `${target}${source}` }),
    );
  },
};

export default nextConfig;
