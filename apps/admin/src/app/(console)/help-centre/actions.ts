"use server";

import { ApiError } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "../meals/actions";

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const TRANSITIONS = ["submit", "return", "publish", "archive", "revise"] as const;

function failure(e: unknown): CatalogState {
  if (e instanceof ApiError) {
    if (e.code === "validation_failed") return { error: "Some fields need attention.", fields: e.fields };
    if (e.status === 403) return { error: "Your role cannot do this. Publishing needs a help centre publisher." };
    return { error: e.message };
  }
  return { error: "The Mise API could not be reached. Try again." };
}

const fields = (form: FormData) => ({
  slug: text(form, "slug"),
  category: text(form, "category"),
  title: text(form, "title"),
  summary: text(form, "summary"),
  body: String(form.get("body") ?? "").replace(/\r\n/g, "\n").trim(),
  note: text(form, "note"),
});

export async function createArticle(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    id = (await api.admin.createHelpArticle(token, fields(form))).id;
  } catch (e) {
    return failure(e);
  }
  redirect(`/help-centre/${id}`);
}

export async function updateArticle(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id");
  try {
    await api.admin.updateHelpArticle(token, id, fields(form));
  } catch (e) {
    return failure(e);
  }
  revalidatePath(`/help-centre/${id}`);
  return { message: "Draft saved." };
}

export async function transitionArticle(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id");
  const action = text(form, "action") as (typeof TRANSITIONS)[number];
  if (!TRANSITIONS.includes(action)) return { error: "Unknown step." };
  try {
    await api.admin.transitionHelpArticle(token, id, action, text(form, "note"));
  } catch (e) {
    return failure(e);
  }
  revalidatePath(`/help-centre/${id}`);
  revalidatePath("/help-centre");
  const done = { submit: "Sent for review.", return: "Returned to draft.", publish: "Published.", archive: "Archived.", revise: "Back to draft: the article is off the site until it is published again." };
  return { message: done[action] };
}
