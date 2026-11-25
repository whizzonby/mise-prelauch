import type { MetadataRoute } from "next";

import { env } from "@/lib/env";

/** The public pages of this site. Meal and plan pages are in the customer app's own sitemap. */
export default function sitemap(): MetadataRoute.Sitemap {
  const lastModified = new Date();
  const page = (path: string, priority: number, changeFrequency: "weekly" | "monthly" | "yearly") => ({
    url: `${env.siteUrl}${path}`,
    lastModified,
    changeFrequency,
    priority,
  });
  return [
    page("/", 1, "weekly"),
    page("/how-it-works", 0.7, "monthly"),
    page("/faq", 0.6, "monthly"),
    page("/about", 0.5, "monthly"),
    page("/contact", 0.4, "yearly"),
    page("/privacy", 0.2, "yearly"),
    page("/terms", 0.2, "yearly"),
  ];
}
