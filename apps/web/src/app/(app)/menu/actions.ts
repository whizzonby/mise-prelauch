"use server";

import type { SelectionItem, SelectionPage } from "@mise/api-client";
import { ApiError } from "@mise/api-client";
import { revalidatePath } from "next/cache";

import { formError } from "@/lib/messages";
import { api, requireCustomer } from "@/lib/session";

export interface SaveResult {
  page?: SelectionPage;
  error?: string;
  /** The server's latest, when the save was refused because it changed elsewhere. */
  latest?: SelectionPage;
}

/**
 * Saves (or saves and confirms) a week's meals. The API checks every rule:
 * the menu, the allowance, per-meal limits and the cutoff. The screen shows
 * a choice as confirmed only when this returns a page saying so.
 */
export async function saveMeals(cycleId: string, items: SelectionItem[], version: number | null, confirm: boolean): Promise<SaveResult> {
  const { token } = await requireCustomer();
  try {
    const page = confirm
      ? await api.commerce.finalizeSelection(token, cycleId, items, version)
      : await api.commerce.saveSelection(token, cycleId, items, version);
    revalidatePath("/subscription");
    revalidatePath("/dashboard");
    return { page };
  } catch (error) {
    const state = formError(error);
    const message = state.fields?.items ?? state.error ?? "Your meals could not be saved.";
    if (error instanceof ApiError && (error.code === "version_conflict" || error.code === "selection_closed")) {
      const latest = await api.commerce.selection(token, cycleId).catch(() => undefined);
      return { error: message, latest };
    }
    return { error: message };
  }
}
