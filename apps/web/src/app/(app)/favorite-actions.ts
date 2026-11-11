"use server";

import { ApiError } from "@mise/api-client";
import { revalidatePath } from "next/cache";

import { api, requireCustomer } from "@/lib/session";

export interface FavoriteResult {
  ok: boolean;
  saved: boolean;
  error?: string;
}

/** Saves or removes a meal. The API decides what is allowed; this only relays it. */
export async function setFavorite(mealId: string, save: boolean): Promise<FavoriteResult> {
  const { token } = await requireCustomer();
  try {
    const res = save ? await api.account.addFavorite(token, mealId) : await api.account.removeFavorite(token, mealId);
    revalidatePath("/favorites");
    return { ok: true, saved: res.saved };
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      return { ok: false, saved: false, error: "This meal is no longer available." };
    }
    return { ok: false, saved: !save, error: "That did not save. Check your connection and try again." };
  }
}
