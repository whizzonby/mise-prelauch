import type { MetadataRoute } from "next";

import { api } from "@/lib/session";

const appUrl = (process.env.CUSTOMER_APP_URL ?? "http://localhost:3102").replace(/\/$/, "");

/** Refreshed hourly: a meal unpublished in the admin leaves the sitemap within the hour. */
export const revalidate = 3600;

/**
 * The public catalogue only: published meals (the API never lists an
 * unpublished meal, nor a demo meal unless demo content is switched on),
 * chefs with published profiles, and the plans customers can see.
 */
export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const meals: { slug: string }[] = [];
  for (let page = 1; page <= 20; page++) {
    const result = await api.catalog.meals({ page, page_size: 48 }).catch(() => null);
    if (!result) break;
    meals.push(...result.items.filter((m) => !m.is_demo));
    if (page * 48 >= result.total) break;
  }
  const [plans, chefs] = await Promise.all([api.shop.plans().catch(() => null), api.catalog.chefs().catch(() => null)]);
  return [
    { url: `${appUrl}/meals`, changeFrequency: "weekly", priority: 0.9 },
    { url: `${appUrl}/plans`, changeFrequency: "monthly", priority: 0.8 },
    ...meals.map((m) => ({ url: `${appUrl}/meals/${m.slug}`, changeFrequency: "weekly" as const, priority: 0.7 })),
    ...(plans?.items ?? []).filter((p) => !p.is_demo).map((p) => ({ url: `${appUrl}/plans/${p.slug}`, changeFrequency: "monthly" as const, priority: 0.6 })),
    ...(chefs?.items ?? []).map((c) => ({ url: `${appUrl}/chefs/${c.slug}`, changeFrequency: "monthly" as const, priority: 0.4 })),
  ];
}
