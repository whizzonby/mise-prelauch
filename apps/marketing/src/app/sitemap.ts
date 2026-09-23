import type { MetadataRoute } from "next";

import { env } from "@/lib/env";

export default function sitemap(): MetadataRoute.Sitemap {
  const lastModified = new Date();
  return [
    { url: `${env.siteUrl}/`, lastModified, changeFrequency: "weekly", priority: 1 },
    { url: `${env.siteUrl}/privacy`, lastModified, changeFrequency: "yearly", priority: 0.2 },
    { url: `${env.siteUrl}/terms`, lastModified, changeFrequency: "yearly", priority: 0.2 },
  ];
}
