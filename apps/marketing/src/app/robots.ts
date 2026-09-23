import type { MetadataRoute } from "next";

import { env } from "@/lib/env";

export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      {
        userAgent: "*",
        allow: "/",
        // Personal pages reached from email links, and per-person invitation URLs.
        disallow: ["/welcome", "/verify", "/unsubscribe", "/join/"],
      },
    ],
    sitemap: `${env.siteUrl}/sitemap.xml`,
    host: env.siteUrl,
  };
}
