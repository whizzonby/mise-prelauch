"use server";

import { ApiError, type ChatSettings } from "@mise/api-client";
import { revalidatePath } from "next/cache";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "../../../meals/actions";

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const DAYS = [0, 1, 2, 3, 4, 5, 6];

function failure(e: unknown): CatalogState {
  if (e instanceof ApiError) {
    if (e.code === "validation_failed") return { error: e.fields[0]?.message ?? "Some fields need attention.", fields: e.fields };
    if (e.status === 403) return { error: "Only chat managers can change this." };
    return { error: e.message };
  }
  return { error: "The Mise API could not be reached. Try again." };
}

/**
 * Saves availability. Hours are one optional open–close period per weekday;
 * closed days are "YYYY-MM-DD Name" lines. Empty hours mean no hours are
 * claimed: the widget then promises nothing beyond the expectation text.
 */
export async function saveChatSettings(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const hours = DAYS.flatMap((d) => {
    const open = text(form, `open_${d}`);
    const close = text(form, `close_${d}`);
    return open && close ? [{ day: d, open, close }] : [];
  });
  const holidays = text(form, "holidays")
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const [day, ...name] = l.split(/\s+/);
      return { day: day ?? "", name: name.join(" ") };
    });
  const input: ChatSettings = {
    enabled: form.get("enabled") === "on",
    timezone: text(form, "timezone"),
    business_hours: hours,
    holidays,
    online_message: text(form, "online_message"),
    offline_message: text(form, "offline_message"),
    response_expectation: text(form, "response_expectation"),
    email_cooldown_minutes: Number(text(form, "email_cooldown_minutes")) || 0,
  };
  try {
    await api.admin.updateChatSettings(token, input);
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/support/chat/settings");
  return { message: "Chat settings saved." };
}

export async function saveSavedReply(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id");
  try {
    if (id) {
      await api.admin.updateSavedReply(token, id, { title: text(form, "title"), body: text(form, "body"), archived: form.get("archived") === "on" });
    } else {
      await api.admin.createSavedReply(token, { title: text(form, "title"), body: text(form, "body") });
    }
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/support/chat/settings");
  return { message: id ? "Saved reply updated." : "Saved reply added." };
}
