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
