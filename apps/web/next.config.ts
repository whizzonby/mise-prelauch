import { existsSync } from "node:fs";
import { join } from "node:path";

import type { NextConfig } from "next";

// One .env at the repository root serves every app in local development.
const rootEnv = join(import.meta.dirname, "..", "..", ".env");
if (existsSync(rootEnv)) process.loadEnvFile(rootEnv);

const isDev = process.env.NODE_ENV !== "production";

// The customer app is a backend-for-frontend: the browser talks only to this
// app's server, which holds the session token and calls the API. So
// connect-src stays 'self', exactly as in the admin.
const csp = [
  "default-src 'self'",
  `script-src 'self' 'unsafe-inline'${isDev ? " 'unsafe-eval'" : ""}`,
  "style-src 'self' 'unsafe-inline'",
  `img-src 'self' data:${(process.env.CATALOG_IMAGE_HOSTS ?? "").split(",").map((h) => h.trim()).filter(Boolean).map((h) => ` https://${h}`).join("")}`,
  "font-src 'self'",
  `connect-src 'self'${isDev ? " ws: http://localhost:*" : ""}`,
  "frame-ancestors 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "object-src 'none'",
].join("; ");

const nextConfig: NextConfig = {
  output: "standalone",
  outputFileTracingRoot: join(import.meta.dirname, "..", ".."),
  poweredByHeader: false,
  devIndicators: false,
  reactStrictMode: true,
  transpilePackages: ["@mise/ui", "@mise/api-client"],
  images: {
    // Meal photographs live in /public/catalog, or on hosts the API also
    // accepts (CATALOG_IMAGE_HOSTS). Nothing else is optimised or proxied.
    remotePatterns: (process.env.CATALOG_IMAGE_HOSTS ?? "")
      .split(",")
      .map((h) => h.trim())
      .filter(Boolean)
      .map((hostname) => ({ protocol: "https" as const, hostname })),
  },
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "Content-Security-Policy", value: csp },
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          // Keeps the tokens in /verify-email and /reset-password links out of Referer headers.
          { key: "Referrer-Policy", value: "no-referrer" },
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
          ...(isDev ? [] : [{ key: "Strict-Transport-Security", value: "max-age=63072000; includeSubDomains" }]),
        ],
      },
      {
        // Account, checkout and sign-in pages are private. Only the public
        // catalogue (meals, chefs, plans) and the sitemap may be indexed.
        source: "/((?!meals|chefs|plans|sitemap\\.xml|robots\\.txt).*)",
        headers: [{ key: "X-Robots-Tag", value: "noindex, nofollow" }],
      },
    ];
  },
};

export default nextConfig;
