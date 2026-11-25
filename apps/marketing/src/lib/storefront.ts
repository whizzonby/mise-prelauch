import type { MealCard, Plan, SubscriptionInfo } from "@mise/api-client";

import { env } from "./env";

/*
 * Live catalogue reads for the public site. The meals and plans shown here
 * are the ones the API publishes (published, non-demo meals in production;
 * active plans with their configured prices), never copies kept in this app.
 * Results are cached for five minutes; a failed read shows an honest empty
 * state instead of stale or sample content.
 */

const REVALIDATE_SECONDS = 300;

async function get<T>(path: string): Promise<T | null> {
  const base = (process.env.API_INTERNAL_URL ?? env.apiUrl).replace(/\/$/, "");
  try {
    const res = await fetch(`${base}/api/v1${path}`, { next: { revalidate: REVALIDATE_SECONDS } });
    if (!res.ok) return null;
    return ((await res.json()) as { data: T }).data;
  } catch {
    return null;
  }
}

/** A handful of the newest published meals, or null if the API could not be read. */
export async function featuredMeals(count = 6): Promise<MealCard[] | null> {
  const page = await get<{ items: MealCard[] }>(`/meals?page_size=${count}`);
  return page ? page.items : null;
}

/** The plans customers can see, in their configured order, or null if the API could not be read. */
export async function livePlans(): Promise<Plan[] | null> {
  const list = await get<{ items: Plan[] }>("/plans");
  return list ? list.items : null;
}

/** Subscription rules (cutoff, pause limit, payment mode), or null. */
export async function subscriptionInfo(): Promise<SubscriptionInfo | null> {
  return get<SubscriptionInfo>("/subscription-info");
}

/**
 * Where a catalogue photograph is served from. Hosted photographs (an https
 * URL on an approved host) go through image optimisation; a path such as
 * /catalog/... belongs to the customer app and is shown as it is.
 */
export function catalogImage(src: string): { src: string; unoptimized: boolean } {
  if (/^https:\/\//.test(src)) return { src, unoptimized: false };
  return { src: `${env.appUrl}${src.startsWith("/") ? "" : "/"}${src}`, unoptimized: true };
}

export { formatMoney } from "@mise/api-client";
