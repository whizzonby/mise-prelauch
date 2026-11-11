"use server";

import { revalidatePath } from "next/cache";

import { BUSINESS_TIME_ZONE } from "@/lib/greeting";
import { formError, type FormState } from "@/lib/messages";
import { api, requireCustomer } from "@/lib/session";

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const all = (form: FormData, key: string) => form.getAll(key).map(String).filter(Boolean);

function savedAt(what = "Saved"): FormState {
  const time = new Intl.DateTimeFormat("en-TT", { timeStyle: "short", timeZone: BUSINESS_TIME_ZONE }).format(new Date());
  return { message: `${what} at ${time}.` };
}

async function run(fn: (token: string) => Promise<unknown>, paths: string[], what?: string): Promise<FormState> {
  const { token } = await requireCustomer();
  try {
    await fn(token);
  } catch (error) {
    return formError(error);
  }
  for (const p of paths) revalidatePath(p);
  return savedAt(what);
}

export async function updateProfile(_: FormState, form: FormData): Promise<FormState> {
  return run(
    (token) =>
      api.account.updateProfile(token, {
        first_name: text(form, "first_name"),
        last_name: text(form, "last_name"),
        phone: text(form, "phone"),
        contact_channel: (text(form, "contact_channel") || "email") as "email" | "sms" | "whatsapp",
        delivery_country: text(form, "delivery_region") ? "TT" : "",
        delivery_region: text(form, "delivery_region"),
      }),
    ["/profile", "/dashboard"],
  );
}

export async function saveHousehold(_: FormState, form: FormData): Promise<FormState> {
  const num = (k: string) => (text(form, k) === "" ? null : Number(text(form, k)));
  return run(
    (token) =>
      api.account.putHousehold(token, {
        name: text(form, "name") || null,
        size: Number(text(form, "size")) || 0,
        adults: num("adults"),
        children: num("children"),
      }),
    ["/profile/household"],
  );
}

export async function savePreferences(_: FormState, form: FormData): Promise<FormState> {
  return run(
    (token) =>
      api.account.putPreferences(token, {
        dietary: all(form, "dietary"),
        meal_interests: all(form, "meal_interests"),
        proteins: all(form, "proteins"),
        max_cook_minutes: Number(text(form, "max_cook_minutes")) || 0,
        other_restrictions: text(form, "other_restrictions"),
      }),
    ["/profile/preferences", "/dashboard"],
  );
}

/** Allergies are saved on their own, so saving preferences can never change them. */
export async function saveAllergies(_: FormState, form: FormData): Promise<FormState> {
  const allergies = all(form, "allergen").map((allergen) =>
    allergen === "other" ? { allergen, detail: text(form, "other_detail") } : { allergen },
  );
  const state = await run((token) => api.account.putPreferences(token, { allergies }), ["/profile/preferences", "/dashboard"], "Allergies saved");
  // The API names the description field "allergies.other"; the form calls it other_detail.
  if (state.fields?.["allergies.other"]) state.fields.other_detail = state.fields["allergies.other"];
  return state;
}

export async function saveNotifications(_: FormState, form: FormData): Promise<FormState> {
  const topics = Object.fromEntries(all(form, "topic_keys").map((k) => [k, form.get(`topic_${k}`) === "on"]));
  return run((token) => api.account.putNotifications(token, topics), ["/profile/notifications"]);
}

function addressInput(form: FormData) {
  return {
    label: text(form, "label"),
    recipient_name: text(form, "recipient_name"),
    phone: text(form, "phone"),
    line1: text(form, "line1"),
    line2: text(form, "line2"),
    area: text(form, "area"),
    city: text(form, "city"),
    region: text(form, "region"),
    country: "TT" as const,
    instructions: text(form, "instructions"),
    is_default: form.get("is_default") === "on",
  };
}

export async function createAddress(_: FormState, form: FormData): Promise<FormState> {
  const state = await run((token) => api.account.createAddress(token, addressInput(form)), ["/profile/addresses"], "Address added");
  return state.error ? state : { ...state, done: true };
}

export async function updateAddress(_: FormState, form: FormData): Promise<FormState> {
  const id = text(form, "id");
  const input = addressInput(form);
  // The default address stays the default until another one takes over.
  const body = form.get("was_default") === "true" ? { ...input, is_default: undefined } : input;
  return run((token) => api.account.updateAddress(token, id, body), ["/profile/addresses"], "Address saved");
}

export async function deleteAddress(form: FormData): Promise<void> {
  const { token } = await requireCustomer();
  await api.account.deleteAddress(token, text(form, "id")).catch(() => undefined);
  revalidatePath("/profile/addresses");
}

export async function changePassword(_: FormState, form: FormData): Promise<FormState> {
  const state = await run(
    (token) => api.account.changePassword(token, String(form.get("current_password") ?? ""), String(form.get("new_password") ?? "")),
    ["/profile/security"],
  );
  return state.error ? state : { message: "Your password is changed. Every other device has been signed out." };
}

export async function requestEmailChange(_: FormState, form: FormData): Promise<FormState> {
  const state = await run(
    (token) => api.account.requestEmailChange(token, text(form, "new_email"), String(form.get("current_password") ?? "")),
    ["/profile", "/profile/security"],
  );
  return state.error
    ? state
    : { message: "Check the inbox of the new address and follow the link there. Until then, your current address stays in use." };
}

export async function revokeSession(form: FormData): Promise<void> {
  const { token } = await requireCustomer();
  await api.auth.revokeSession(token, text(form, "id")).catch(() => undefined);
  revalidatePath("/profile/security");
}

export async function signOutEverywhereElse(): Promise<void> {
  const { token } = await requireCustomer();
  await api.auth.revokeOtherSessions(token).catch(() => undefined);
  revalidatePath("/profile/security");
}
