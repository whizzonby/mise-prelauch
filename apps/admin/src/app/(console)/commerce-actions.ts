"use server";

import { ApiError, type MenuInput, type PlanInput } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "./meals/actions";

/*
 * Commerce administration. Every action is checked again by the API against
 * the admin's permissions; nothing here sets a payment status by hand.
 */

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const all = (form: FormData, key: string) => form.getAll(key).map(String).filter(Boolean);
const num = (form: FormData, key: string) => (text(form, key) === "" ? null : Number(text(form, key)));

function failure(error: unknown): CatalogState {
  if (error instanceof ApiError) {
    if (error.code === "validation_failed") return { error: "Some fields need attention.", fields: error.fields };
    if (error.status === 403) return { error: "Your role does not allow this." };
    return { error: error.message };
  }
  return { error: "The Mise API could not be reached. Try again in a moment." };
}

/** "TTD 330.00" or "330" typed by a person, into minor units. */
function money(raw: string): number {
  const cleaned = raw.replace(/[^0-9.]/g, "");
  if (!cleaned) return 0;
  const [whole, frac = ""] = cleaned.split(".");
  return Number(whole) * 100 + Number((frac + "00").slice(0, 2));
}

function localInstant(raw: string): string | null {
  // <input type="datetime-local"> gives Trinidad and Tobago wall time (UTC-4, no DST).
  return raw ? new Date(`${raw}:00-04:00`).toISOString() : null;
}

// ---------------------------------------------------------------- plans

function planFrom(form: FormData): PlanInput {
  return {
    slug: text(form, "slug"),
    name: text(form, "name"),
    summary: text(form, "summary"),
    description: text(form, "description"),
    meals_per_week: Number(text(form, "meals_per_week")) || 0,
    segment: text(form, "segment"),
    benefits: text(form, "benefits").split(/\r?\n/).map((b) => b.trim()).filter(Boolean),
    self_serve: form.get("self_serve") === "on",
    available_from: localInstant(text(form, "available_from")),
    available_until: localInstant(text(form, "available_until")),
    sort_order: Number(text(form, "sort_order")) || 0,
    version: Number(text(form, "version")) || 0,
  };
}

export async function createPlan(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    id = (await api.admin.createPlan(token, planFrom(form))).id;
  } catch (e) {
    return failure(e);
  }
  redirect(`/plans/${id}`);
}

export async function updatePlan(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await api.admin.updatePlan(token, text(form, "id"), planFrom(form));
  } catch (e) {
    return failure(e);
  }
  revalidatePath(`/plans/${text(form, "id")}`);
  return { message: "Plan saved." };
}

export async function addPrice(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await api.admin.addPlanPrice(token, text(form, "id"), {
      amount_minor: money(text(form, "amount")),
      effective_from: localInstant(text(form, "effective_from")),
      note: text(form, "note"),
    });
  } catch (e) {
    return failure(e);
  }
  revalidatePath(`/plans/${text(form, "id")}`);
  return { message: "Price added. Existing subscribers keep the price they bought at." };
}

export async function setPlanStatus(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await api.admin.setPlanStatus(token, text(form, "id"), text(form, "action") as "activate" | "retire");
  } catch (e) {
    return failure(e);
  }
  revalidatePath(`/plans/${text(form, "id")}`);
  revalidatePath("/plans");
  return { message: "Status changed." };
}

// ---------------------------------------------------------------- menus

function menuFrom(form: FormData): MenuInput {
  const caps = (id: string) => num(form, `cap_${id}`);
  return {
    week: text(form, "week"),
    title: text(form, "title"),
    note: text(form, "note"),
    cutoff_at: localInstant(text(form, "cutoff_at")),
    eligible_plan_ids: all(form, "eligible_plan_ids"),
    meals: all(form, "meal_ids").map((id) => ({ meal_id: id, max_per_customer: caps(id) })),
    version: Number(text(form, "version")) || 0,
  };
}

export async function createMenu(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    id = (await api.admin.createMenu(token, menuFrom(form))).id;
  } catch (e) {
    return failure(e);
  }
  redirect(`/menus/${id}`);
}

export async function updateMenu(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await api.admin.updateMenu(token, text(form, "id"), menuFrom(form));
  } catch (e) {
    return failure(e);
  }
  revalidatePath(`/menus/${text(form, "id")}`);
  return { message: "Menu saved." };
}

export async function publishMenu(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id");
  try {
    if (text(form, "action") === "unpublish") await api.admin.unpublishMenu(token, id);
    else await api.admin.publishMenu(token, id, localInstant(text(form, "publish_at")) ?? undefined);
  } catch (e) {
    return failure(e);
  }
  revalidatePath(`/menus/${id}`);
  revalidatePath("/menus");
  return { message: text(form, "action") === "unpublish" ? "Menu unpublished." : "Menu published." };
}

// ---------------------------------------------------------------- payments and promotions

export async function refund(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id");
  try {
    await api.admin.refund(token, id, { amount_minor: money(text(form, "amount")), reason: text(form, "reason"), request_key: text(form, "request_key") });
  } catch (e) {
    return failure(e);
  }
  revalidatePath(`/payments/${id}`);
  return { message: "Refund recorded with the provider." };
}

export async function createPromotion(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const kind = text(form, "kind") as "percent" | "fixed";
  try {
    await api.admin.createPromotion(token, {
      code: text(form, "code"),
      name: text(form, "name"),
      kind,
      percent_bps: kind === "percent" ? Math.round(Number(text(form, "value")) * 100) : null,
      amount_minor: kind === "fixed" ? money(text(form, "value")) : null,
      cycles: Number(text(form, "cycles")) || 1,
      max_redemptions: num(form, "max_redemptions"),
      valid_from: localInstant(text(form, "valid_from")),
      valid_until: localInstant(text(form, "valid_until")),
      plan_ids: all(form, "plan_ids"),
    });
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/promotions");
  return { message: "Promotion created as a draft. It applies only once activated." };
}

export async function setPromotionStatus(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await api.admin.setPromotionStatus(token, text(form, "id"), text(form, "action") as "activate" | "archive");
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/promotions");
  return { message: "Promotion updated." };
}
