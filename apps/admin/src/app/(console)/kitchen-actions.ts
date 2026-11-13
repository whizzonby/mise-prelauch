"use server";

import { ApiError, type DraftInput, type RecipeLineInput } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "./meals/actions";

/*
 * Sprint 5 actions: ingredients, recipes and their reviews, chefs,
 * production plans, batches, kitchen tasks, quality, packaging, waste and
 * settings. The API checks every permission and rule again; these only
 * pass the form along and report the outcome.
 */

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const all = (form: FormData, key: string) => form.getAll(key).map(String).map((v) => v.trim());
const int = (form: FormData, key: string) => {
  const v = text(form, key);
  return v === "" ? undefined : Number.parseInt(v, 10);
};
const flag = (form: FormData, key: string) => form.get(key) === "on" || form.get(key) === "true";

function failure(error: unknown): CatalogState {
  if (error instanceof ApiError) {
    if (error.code === "validation_failed") return { error: "Some fields need attention.", fields: error.fields };
    if (error.status === 403) return { error: error.code === "forbidden" ? "Your role does not allow this." : error.message };
    return { error: error.message };
  }
  return { error: "The Mise API could not be reached. Try again in a moment." };
}

async function run(fn: (token: string) => Promise<unknown>, paths: string[], message: string): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await fn(token);
  } catch (e) {
    return failure(e);
  }
  for (const p of paths) revalidatePath(p);
  return { message };
}

// ---------------------------------------------------------------- ingredients

function ingredientFrom(form: FormData) {
  // Only what the form carries: other fields keep their value.
  const out: Record<string, unknown> = {};
  for (const key of ["name", "description", "category", "base_unit", "purchasing_unit", "preparation_state", "storage", "storage_notes", "shelf_life_note"]) {
    if (form.has(key)) out[key] = text(form, key);
  }
  if (form.has("allergens_present")) out.allergens = all(form, "allergens").filter(Boolean);
  if (form.has("active_present")) out.active = flag(form, "active");
  if (form.has("aliases"))
    out.aliases = text(form, "aliases")
      .split(/[,\n]/)
      .map((a) => a.trim())
      .filter(Boolean);
  return out;
}

export async function saveIngredient(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id");
  let saved: string;
  try {
    saved = id ? (await api.kitchen.updateIngredient(token, id, ingredientFrom(form))).id : (await api.kitchen.createIngredient(token, ingredientFrom(form))).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/ingredients");
  if (!id) redirect(`/ingredients/${saved}`);
  revalidatePath(`/ingredients/${id}`);
  return { message: "Ingredient saved." };
}

export async function setConversion(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run(
    (t) =>
      api.kitchen.setConversion(t, id, {
        from_unit: text(form, "from_unit"),
        to_unit: text(form, "to_unit"),
        factor: text(form, "factor"),
        approximate: flag(form, "approximate"),
        source: text(form, "source"),
      }),
    [`/ingredients/${id}`],
    "Conversion saved.",
  );
}

export async function addCost(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "ingredient_id");
  const pounds = text(form, "amount");
  const amount = Math.round(Number(pounds) * 100);
  if (!Number.isFinite(amount)) return { error: "Enter the amount in dollars and cents.", fields: [{ field: "amount", message: "Enter a number." }] };
  return run(
    (t) =>
      api.kitchen.addCost(t, {
        kind: text(form, "kind") === "packaging" ? "packaging" : "ingredient",
        ingredient_id: id,
        label: text(form, "label"),
        amount_minor: amount,
        per_quantity: text(form, "per_quantity"),
        unit: text(form, "unit"),
        source: text(form, "source"),
        effective_from: text(form, "effective_from"),
      }),
    [id ? `/ingredients/${id}` : "/ingredients"],
    "Reference cost recorded.",
  );
}

// ---------------------------------------------------------------- chefs

export async function updateChef(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run(
    (t) =>
      api.kitchen.updateChef(t, id, {
        user_id: text(form, "user_id"),
        specialities: text(form, "specialities")
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        work_status: text(form, "work_status") === "inactive" ? "inactive" : "active",
      }),
    ["/chefs"],
    "Chef saved.",
  );
}

// ---------------------------------------------------------------- recipes

export async function createRecipe(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    const r = await api.kitchen.createRecipe(token, {
      name: text(form, "name"),
      category: text(form, "category"),
      meal_id: text(form, "meal_id") || undefined,
    });
    id = r.id;
  } catch (e) {
    return failure(e);
  }
  redirect(`/recipes/${id}?step=ingredients`);
}

export async function updateRecipe(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const input: Record<string, unknown> = { name: text(form, "name"), description: text(form, "description"), category: text(form, "category") };
  if (form.has("meal_id")) input.meal_id = text(form, "meal_id");
  if (form.has("chef_id")) input.chef_id = text(form, "chef_id");
  return run((t) => api.kitchen.updateRecipe(t, id, input), [`/recipes/${id}`, "/recipes"], "Recipe details saved.");
}

/** Saves one step of the draft: only the fields that step's form carries. */
export async function saveDraft(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const step = text(form, "step");
  const input: DraftInput = {};
  if (step === "basics") {
    input.yield_servings = int(form, "yield_servings");
    input.serving_size = text(form, "serving_size");
    input.serving_size_g = text(form, "serving_size_g") || null;
    input.kitchen_prep_minutes = int(form, "kitchen_prep_minutes") ?? -1;
    input.customer_cook_minutes = int(form, "customer_cook_minutes") ?? -1;
    input.dietary_tags = all(form, "dietary_tags").filter(Boolean);
    input.change_summary = text(form, "change_summary");
  }
  if (step === "ingredients") {
    const ids = all(form, "line_ingredient");
    const lines: RecipeLineInput[] = [];
    ids.forEach((ingredient, i) => {
      const quantity = all(form, "line_quantity")[i] ?? "";
      if (!ingredient && !quantity) return;
      lines.push({
        ingredient_id: ingredient,
        quantity,
        unit: all(form, "line_unit")[i] ?? "",
        preparation: all(form, "line_preparation")[i] ?? "",
        yield_percent: (all(form, "line_yield")[i] ?? "") || null,
        optional: all(form, "line_optional").includes(String(i)),
        substitution: all(form, "line_substitution")[i] || "none",
        note: all(form, "line_note")[i] ?? "",
      });
    });
    input.ingredients = lines;
  }
  if (step === "method") {
    const steps = (key: string) =>
      text(form, key)
        .split(/\n\s*\n|\n(?=\d+[.)]\s)/)
        .map((s) => s.replace(/^\d+[.)]\s*/, "").trim())
        .filter(Boolean);
    input.preparation_steps = steps("preparation_steps");
    input.assembly_steps = steps("assembly_steps");
    input.customer_instructions = text(form, "customer_instructions");
    input.storage_instructions = text(form, "storage_instructions");
    input.packaging_instructions = text(form, "packaging_instructions");
  }
  if (step === "safety") {
    input.allergens_contains = all(form, "contains").filter(Boolean);
    input.allergens_may_contain = all(form, "may_contain").filter(Boolean);
    input.cross_contact_note = text(form, "cross_contact_note");
    const n = (k: string) => text(form, k) || null;
    input.nutrition = {
      energy_kcal: n("energy_kcal"),
      protein_g: n("protein_g"),
      carbs_g: n("carbs_g"),
      fat_g: n("fat_g"),
      sat_fat_g: n("sat_fat_g"),
      fibre_g: n("fibre_g"),
      sugar_g: n("sugar_g"),
      sodium_mg: n("sodium_mg"),
      source: n("source"),
      method: text(form, "method"),
    };
  }
  return run((t) => api.kitchen.saveDraft(t, id, input), [`/recipes/${id}`], "Saved. Earlier reviews of this version no longer count.");
}

const RECIPE_MESSAGES: Record<string, string> = {
  submit: "Submitted for review.",
  review: "Culinary review recorded.",
  "request-changes": "Sent back to the chef.",
  approve: "Approved. This version can no longer change.",
  publish: "Published for production.",
  archive: "Withdrawn from future production.",
  revise: "A new draft was started.",
};

export async function recipeAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action") as "submit" | "review" | "request-changes" | "approve" | "publish" | "archive" | "revise";
  const body: Record<string, unknown> = {};
  for (const key of ["notes", "decision", "effective_from", "reason", "change_summary"]) if (text(form, key)) body[key] = text(form, key);
  if (int(form, "version")) body.version = int(form, "version");
  return run(
    (t) => api.kitchen.recipeAction(t, id, action, body),
    [`/recipes/${id}`, "/recipes", "/recipe-approvals", "/nutrition"],
    RECIPE_MESSAGES[action] ?? "Done.",
  );
}

export async function nutritionAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action") === "approve" ? "approve" : "request-changes";
  const body: Record<string, unknown> = { notes: text(form, "notes") };
  if (action === "request-changes") body.decision = text(form, "decision") || "changes_requested";
  return run(
    (t) => api.kitchen.nutritionAction(t, id, action, body),
    [`/recipes/${id}`, "/nutrition"],
    action === "approve" ? "Nutrition and allergens signed off." : "Sent back to the chef.",
  );
}

// ---------------------------------------------------------------- planning

export async function snapshotDemand(_: CatalogState, form: FormData): Promise<CatalogState> {
  const week = text(form, "week");
  const { token } = await requireAdmin();
  try {
    const r = await api.kitchen.snapshot(token, week);
    revalidatePath("/production/plans");
    return { message: r.created ? `Revision ${r.snapshot.revision} saved: ${r.snapshot.kits} kits.` : "Demand has not changed since the last snapshot." };
  } catch (e) {
    return failure(e);
  }
}

export async function createPlan(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    const p = await api.kitchen.createPlan(token, {
      week: text(form, "week"),
      production_date: text(form, "production_date") || undefined,
      notes: text(form, "notes") || undefined,
    });
    id = p.id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/production/plans");
  redirect(`/production/plans/${id}`);
}

export async function adjustItem(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run(
    (t) => api.kitchen.adjustItem(t, id, text(form, "item"), { planned_kits: int(form, "planned_kits") ?? 0, reason: text(form, "reason") || undefined }),
    [`/production/plans/${id}`],
    "Quantity saved; requirements recalculated.",
  );
}

export async function planAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action") as "approve" | "release" | "cancel";
  const body = action === "cancel" ? { reason: text(form, "reason") } : undefined;
  const msg = { approve: "Plan approved; batches prepared.", release: "Released to the kitchen.", cancel: "Plan cancelled." }[action];
  return run((t) => api.kitchen.planAction(t, id, action, body), [`/production/plans/${id}`, "/production/plans", "/production"], msg);
}

// ---------------------------------------------------------------- batches and the kitchen

export async function batchAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action") as "start" | "output" | "complete" | "cancel";
  let body: Record<string, unknown> | undefined;
  if (action === "start") body = { assigned_team: text(form, "assigned_team") };
  if (action === "output")
    body = {
      produced_kits: int(form, "produced_kits") ?? -1,
      rejected_kits: int(form, "rejected_kits") ?? 0,
      reworked_kits: int(form, "reworked_kits") ?? 0,
      notes: text(form, "notes"),
    };
  if (action === "cancel") body = { reason: text(form, "reason") };
  const msg = { start: "Batch started.", output: "Output recorded.", complete: "Batch complete: ready for fulfilment.", cancel: "Batch cancelled." }[action];
  return run((t) => api.kitchen.batchAction(t, id, action, body), [`/production/batches/${id}`, "/production/batches", "/production", "/production/tasks"], msg);
}

export async function savePackaging(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const complete = text(form, "intent") !== "progress";
  return run(
    (t) =>
      api.kitchen.packaging(t, id, {
        packaging_type: text(form, "packaging_type"),
        packed_kits: int(form, "packed_kits") ?? 0,
        labels_verified: flag(form, "labels_verified"),
        exceptions: text(form, "exceptions"),
        assigned_to: form.has("assign_me") ? "me" : undefined,
        complete,
      }),
    [`/production/batches/${id}`, "/production/packaging"],
    complete ? "Packing complete." : "Packing progress saved.",
  );
}

export async function updateTask(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const input: Record<string, string> = {};
  for (const key of ["status", "assigned_to", "note", "exception"]) if (form.has(key)) input[key] = text(form, key);
  const back = text(form, "back");
  return run(
    (t) => api.kitchen.updateTask(t, id, input),
    ["/production/tasks", ...(back ? [back] : [])],
    input.status === "done" ? "Task done." : input.status === "blocked" ? "Task blocked; the manager is alerted." : "Task updated.",
  );
}

export async function recordCheck(_: CatalogState, form: FormData): Promise<CatalogState> {
  const batch = text(form, "batch_id");
  const result = text(form, "result") === "fail" ? "fail" : "pass";
  return run(
    (t) => api.kitchen.recordCheck(t, { batch_id: batch, template_id: text(form, "template_id"), result, notes: text(form, "notes"), evidence_ref: text(form, "evidence_ref") }),
    [`/production/batches/${batch}`, "/production/quality"],
    result === "pass" ? "Check passed." : "Check failed and recorded.",
  );
}

export async function placeHold(_: CatalogState, form: FormData): Promise<CatalogState> {
  const batch = text(form, "batch_id");
  return run((t) => api.kitchen.placeHold(t, { batch_id: batch, reason: text(form, "reason") }), [`/production/batches/${batch}`, "/production/quality"], "Batch put on hold.");
}

export async function resolveHold(_: CatalogState, form: FormData): Promise<CatalogState> {
  const decision = text(form, "decision") === "discard" ? "discard" : "release";
  const batch = text(form, "batch_id");
  return run(
    (t) => api.kitchen.resolveHold(t, text(form, "id"), { decision, notes: text(form, "notes") }),
    ["/production/quality", `/production/batches/${batch}`],
    decision === "release" ? "Hold released. The failed checks must be inspected again." : "Batch discarded.",
  );
}

export async function recordWaste(_: CatalogState, form: FormData): Promise<CatalogState> {
  const batch = text(form, "batch_id");
  return run(
    (t) =>
      api.kitchen.recordWaste(t, {
        batch_id: batch,
        ingredient_id: text(form, "ingredient_id"),
        item: text(form, "item"),
        quantity: text(form, "quantity"),
        unit: text(form, "unit"),
        category: text(form, "category"),
        reason: text(form, "reason"),
      }),
    ["/production/waste", ...(batch ? [`/production/batches/${batch}`] : [])],
    "Waste recorded.",
  );
}

// ---------------------------------------------------------------- settings

export async function saveTaskTemplate(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id") || undefined;
  return run(
    (t) =>
      api.kitchen.saveTaskTemplate(
        t,
        {
          name: text(form, "name"),
          kind: text(form, "kind"),
          instructions: text(form, "instructions"),
          position: int(form, "position") ?? 0,
          priority: text(form, "priority") || "normal",
          due_offset_minutes: int(form, "due_offset_minutes") ?? -1,
          active: id ? flag(form, "active") : true,
        },
        id,
      ),
    ["/production/settings"],
    "Task template saved.",
  );
}

export async function saveQualityTemplate(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id") || undefined;
  return run(
    (t) =>
      api.kitchen.saveQualityTemplate(
        t,
        {
          name: text(form, "name"),
          check_type: text(form, "check_type"),
          criteria: text(form, "criteria"),
          critical: flag(form, "critical"),
          position: int(form, "position") ?? 0,
          active: id ? flag(form, "active") : true,
          procedure_ref: text(form, "procedure_ref"),
        },
        id,
      ),
    ["/production/settings"],
    "Checklist item saved.",
  );
}

export async function saveProductionSettings(_: CatalogState, form: FormData): Promise<CatalogState> {
  const slots: Record<string, string> = {};
  for (const [k, v] of form.entries()) if (k.startsWith("slot:")) slots[k.slice(5)] = String(v);
  return run(
    (t) =>
      api.kitchen.saveSettings(t, {
        capacity_kits_per_day: int(form, "capacity_kits_per_day") ?? 0,
        capacity_note: text(form, "capacity_note"),
        slot_status: slots,
      }),
    ["/production/settings", "/production/calendar"],
    "Settings saved.",
  );
}
