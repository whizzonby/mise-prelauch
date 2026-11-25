import "server-only";

import type { Reference } from "@mise/api-client";
import { cache } from "react";

import { api } from "./session";

/** Ids of the meals the customer has saved, for marking cards. */
export const savedMealIds = cache(async (token: string): Promise<Set<string>> => {
  const list = await api.account.favorites(token).catch(() => null);
  return new Set(list?.meal_ids ?? []);
});

/** The fixed value lists and their labels, from the API (one source of truth). */
export const reference = cache(async (): Promise<Reference> => api.catalog.reference());

export function labelFor(list: { key: string; label: string }[], key: string | null | undefined): string {
  if (!key) return "";
  return list.find((t) => t.key === key)?.label ?? key;
}

/**
 * The published weekly menus still open for choosing, with their meals. A
 * meal's availability is read from these: being published in the catalogue
 * does not mean it is on a menu anyone can order from this week.
 */
export const openMenus = cache(async () => {
  const { items } = await api.shop.menus();
  const open = items.filter((m) => m.status === "published" && m.is_open).slice(0, 4);
  return Promise.all(open.map((m) => api.shop.menu(m.week)));
});
