"use server";

import { ApiError, type MealInput } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { api, requireAdmin } from "@/lib/session";

export interface CatalogState {
  error?: string;
  fields?: { field: string; message: string }[];
  message?: string;
}

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const all = (form: FormData, key: string) => form.getAll(key).map(String).filter(Boolean);

function failure(error: unknown): CatalogState {
  if (error instanceof ApiError) {
    if (error.code === "validation_failed") return { error: "Some fields need attention.", fields: error.fields };
    if (error.status === 403) return { error: "Your role does not allow this." };
    return { error: error.message };
  }
  return { error: "The Mise API could not be reached. Try again in a moment." };
}

/**
 * Lines of "a | b | c" into parts. Photographs: "src | alt text | credit".
 * Ingredients: "name | quantity".
 */
function rows(raw: string): string[][] {
  return raw
    .split(/\r?\n/)
    .map((line) => line.split("|").map((p) => p.trim()))
    .filter((parts) => parts[0]);
}

function mealFrom(form: FormData): MealInput {
  const minutes = (k: string) => Number(text(form, k)) || 0;
  return {
    slug: text(form, "slug"),
    name: text(form, "name"),
    summary: text(form, "summary"),
    description: text(form, "description"),
    cooking_overview: text(form, "cooking_overview"),
    category_id: text(form, "category_id"),
    chef_id: text(form, "chef_id") || null,
    prep_minutes: minutes("prep_minutes"),
    cook_minutes: minutes("cook_minutes"),
    servings: minutes("servings"),
    protein: text(form, "protein") || null,
    dietary_tags: all(form, "dietary_tags"),
    allergens: all(form, "allergens"),
    allergen_status: text(form, "allergen_status") === "declared" ? "declared" : "unconfirmed",
    images: rows(text(form, "images")).map(([src, alt, credit]) => ({ src: src!, alt: alt ?? "", credit: credit || null })),
    ingredients: rows(text(form, "ingredients")).map(([name, quantity]) => ({ name: name!, quantity: quantity || null })),
    is_demo: form.get("is_demo") === "on",
    version: Number(text(form, "version")) || undefined,
  };
}

export async function saveMeal(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id");
  let savedId: string;
  try {
    const meal = id ? await api.admin.updateMeal(token, id, mealFrom(form)) : await api.admin.createMeal(token, mealFrom(form));
    savedId = meal.id;
  } catch (error) {
    if (error instanceof ApiError && error.code === "version_conflict") return { error: error.message };
    return failure(error);
  }
  revalidatePath("/meals");
  if (!id) redirect(`/meals/${savedId}?created=1`);
  revalidatePath(`/meals/${id}`);
  return { message: "Saved." };
}

export async function setStatus(form: FormData): Promise<void> {
  const { token } = await requireAdmin();
  const id = text(form, "id");
  const action = text(form, "action") === "publish" ? "publish" : "unpublish";
  let problem = "";
  try {
    await api.admin.setMealStatus(token, id, action);
  } catch (error) {
    problem = error instanceof ApiError ? error.code : "unreachable";
  }
  revalidatePath("/meals");
  redirect(`/meals/${id}${problem ? `?problem=${encodeURIComponent(problem)}` : `?${action}ed=1`}`);
}

export async function saveNutrition(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id");
  const num = (k: string) => (text(form, k) === "" ? null : Number(text(form, k)));
  try {
    await api.admin.setNutrition(token, id, {
      calories: num("calories") ?? 0,
      protein_g: num("protein_g") ?? 0,
      carbs_g: num("carbs_g") ?? 0,
      fat_g: num("fat_g") ?? 0,
      fibre_g: num("fibre_g"),
      sodium_mg: num("sodium_mg"),
      verified: form.get("verified") === "on",
    });
  } catch (error) {
    return failure(error);
  }
  revalidatePath(`/meals/${id}`);
  return { message: form.get("verified") === "on" ? "Verified. Customers now see these figures." : "Saved as pending. Customers do not see them yet." };
}

export async function createCategory(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await api.admin.createCategory(token, {
      name: text(form, "name"),
      description: text(form, "description"),
      position: Number(text(form, "position")) || 0,
      is_demo: form.get("is_demo") === "on",
    });
  } catch (error) {
    return failure(error);
  }
  revalidatePath("/meals/setup");
  return { message: "Category added." };
}

export async function saveChef(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id") || undefined;
  try {
    await api.admin.saveChef(
      token,
      {
        display_name: text(form, "display_name"),
        role_title: text(form, "role_title") || null,
        bio: text(form, "bio"),
        image_src: text(form, "image_src") || null,
        image_alt: text(form, "image_alt") || null,
        status: (text(form, "status") || "draft") as "draft" | "published" | "unpublished",
        is_demo: form.get("is_demo") === "on",
      },
      id,
    );
  } catch (error) {
    return failure(error);
  }
  revalidatePath("/meals/setup");
  return { message: "Chef profile saved." };
}
