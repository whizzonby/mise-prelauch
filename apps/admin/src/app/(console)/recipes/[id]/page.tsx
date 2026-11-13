import { ApiError, type RecipeVersion } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import type { ReactNode } from "react";

import { PageHeader, StatusTag, Timeline } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Checks, Field, Notice, NUTRITION_SOURCES, Options, Steps } from "@/components/kitchen-ui";
import { formatDate, formatDateTime, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { nutritionAction, recipeAction, saveDraft, updateRecipe } from "../../kitchen-actions";

export const metadata: Metadata = { title: "Recipe" };

type Search = { step?: string; v?: string };

const STEP_LIST = [
  { key: "details", label: "Details" },
  { key: "ingredients", label: "Ingredients" },
  { key: "method", label: "Method" },
  { key: "safety", label: "Allergens & nutrition" },
  { key: "review", label: "Review" },
  { key: "versions", label: "Versions" },
  { key: "costing", label: "Costing" },
];

const NUTRIENTS: [keyof RecipeVersion["nutrition"], string][] = [
  ["energy_kcal", "Energy (kcal)"],
  ["protein_g", "Protein (g)"],
  ["carbs_g", "Carbohydrate (g)"],
  ["fat_g", "Fat (g)"],
  ["sat_fat_g", "Saturated fat (g)"],
  ["fibre_g", "Fibre (g)"],
  ["sugar_g", "Sugars (g)"],
  ["sodium_mg", "Sodium (mg)"],
];

const STAGE_LABEL: Record<string, string> = { culinary: "Culinary review", nutrition: "Nutrition review", final: "Final approval", publish: "Publication" };

export default async function RecipePage({ params, searchParams }: { params: Promise<{ id: string }>; searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "recipes.read")) notFound();
  const { id } = await params;
  const sp = await searchParams;
  const step = STEP_LIST.some((s) => s.key === sp.step) ? sp.step! : "details";
  const n = Number(sp.v) || 0;
  const missing = (e: unknown) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  };
  const [recipe, v] = await Promise.all([api.kitchen.recipe(token, id).catch(missing), api.kitchen.version(token, id, n).catch(missing)]);
  const latest = v.number === recipe.latest.number;
  const editable = latest && recipe.can_edit && (v.status === "draft" || v.status === "changes_requested");
  const stepBase = `/recipes/${id}`;
  const done = {
    details: true,
    ingredients: v.ingredients.length > 0,
    method: v.preparation_steps.length > 0,
    safety: v.nutrition.complete && v.missing_allergens.length === 0,
    review: v.status === "approved" || v.status === "published",
  } as Record<string, boolean>;

  return (
    <>
      <PageHeader
        back={{ href: "/recipes", label: "All recipes" }}
        title={recipe.name}
        description={
          <>
            {recipe.code} · version {v.number} <StatusTag status={v.status} />
            {recipe.meal_name ? <> · makes the meal “{recipe.meal_name}”</> : <> · not linked to a catalogue meal</>}
            {recipe.in_use ? <> · production uses v{recipe.in_use.number}</> : <> · no published version in use</>}
            {!latest ? (
              <>
                {" "}
                · <Link href={stepBase} className="underline underline-offset-4">viewing an older version; open the latest</Link>
              </>
            ) : null}
          </>
        }
      />
      <Steps steps={STEP_LIST.map((s) => ({ ...s, done: done[s.key] }))} current={step} base={n ? `${stepBase}?v=${n}` : stepBase} />
      <div className="mt-6 grid gap-6 [&>*]:min-w-0">
        {!editable && step !== "review" && step !== "versions" && step !== "costing" && latest ? (
          <p className="type-body-sm border-l-2 border-border-strong pl-3 text-muted">
            {recipe.can_edit
              ? `Version ${v.number} is ${humanize(v.status).toLowerCase()} and cannot be edited. Start a revision on the Review step to change it.`
              : "You can view this recipe; only its chef, collaborators or the culinary director edit it."}
          </p>
        ) : null}
        {step === "details" ? <Details recipe={recipe} v={v} editable={editable} token={token} canCatalog={can(admin, "catalog.read")} isDirector={can(admin, "recipes.approve")} /> : null}
        {step === "ingredients" ? <Ingredients v={v} recipeId={id} editable={editable} token={token} /> : null}
        {step === "method" ? <Method v={v} recipeId={id} editable={editable} /> : null}
        {step === "safety" ? <Safety v={v} recipeId={id} editable={editable} /> : null}
        {step === "review" ? <Review v={v} recipeId={id} latest={latest} canEdit={recipe.can_edit} perms={admin.permissions} /> : null}
        {step === "versions" ? (
          <Panel title="Versions" note="Approved and published versions are immutable. Batches keep the exact version they were made from.">
            <DataTable head={["Version", "Status", "Effective from", "Change", "Updated"]} minWidth="36rem">
              {(recipe.versions ?? []).map((x) => (
                <tr key={x.id}>
                  <td className="p-3">
                    <Link href={`/recipes/${id}?v=${x.number}&step=ingredients`} className="font-semibold underline underline-offset-4">
                      v{x.number}
                    </Link>
                  </td>
                  <td className="p-3">
                    <StatusTag status={x.status} />
                  </td>
                  <td className="p-3">{x.effective_from ? `Week of ${formatDate(x.effective_from)}` : "—"}</td>
                  <td className="p-3">{x.change_summary || "—"}</td>
                  <td className="p-3">{formatDateTime(x.updated_at)}</td>
                </tr>
              ))}
            </DataTable>
          </Panel>
        ) : null}
        {step === "costing" ? <CostingView token={token} recipeId={id} n={v.number} /> : null}
      </div>
    </>
  );
}

async function Details({
  recipe,
  v,
  editable,
  token,
  canCatalog,
  isDirector,
}: {
  recipe: Awaited<ReturnType<typeof api.kitchen.recipe>>;
  v: RecipeVersion;
  editable: boolean;
  token: string;
  canCatalog: boolean;
  isDirector: boolean;
}) {
  const [reference, meals, chefs] = await Promise.all([
    api.catalog.reference(),
    canCatalog && recipe.can_edit ? api.admin.catalogMeals(token, {}) : Promise.resolve(null),
    isDirector ? api.kitchen.chefs(token) : Promise.resolve(null),
  ]);
  const published = (recipe.versions ?? []).some((x) => x.status === "published" || x.status === "archived");
  return (
    <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
      <Panel title="Recipe" note="The recipe's name and links. The customer-facing description and photographs stay on the catalogue meal.">
        {recipe.can_edit ? (
          <KForm action={updateRecipe} submit="Save details" pending="Saving…" label="Recipe details">
            <input type="hidden" name="id" value={recipe.id} />
            <Labelled label="Name">
              <TextInput name="name" defaultValue={recipe.name} required maxLength={100} />
            </Labelled>
            <Labelled label="Category">
              <TextInput name="category" defaultValue={recipe.category} maxLength={60} />
            </Labelled>
            <Labelled label="Kitchen notes">
              <TextArea name="description" defaultValue={recipe.description} rows={3} maxLength={2000} />
            </Labelled>
            {meals && !published ? (
              <Labelled label="Catalogue meal" hint="Fixed once a version is published.">
                <SelectInput name="meal_id" defaultValue={recipe.meal_id ?? ""}>
                  <option value="">Not linked</option>
                  {meals.items.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.name}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
            ) : null}
            {chefs ? (
              <Labelled label="Chef">
                <SelectInput name="chef_id" defaultValue={recipe.chef_id ?? ""}>
                  <option value="">None</option>
                  {chefs.items.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.display_name}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
            ) : null}
          </KForm>
        ) : (
          <dl className="type-body-sm grid gap-2">
            <div>Chef: {recipe.chef_name ?? "none"}</div>
            <div>Category: {recipe.category || "—"}</div>
            <div>{recipe.description}</div>
          </dl>
        )}
      </Panel>
      <Panel title={`Version ${v.number}: yield and timing`} note="The ingredient quantities are for this many servings; production scales them to the kits ordered.">
        {editable ? (
          <KForm action={saveDraft} submit="Save" pending="Saving…" label="Yield and timing">
            <input type="hidden" name="id" value={recipe.id} />
            <input type="hidden" name="step" value="basics" />
            <Field>
              <Labelled label="Servings this recipe makes">
                <TextInput type="number" name="yield_servings" min={1} max={100} defaultValue={v.yield_servings} required />
              </Labelled>
              <Labelled label="Serving size" hint="As written, for example “1 bowl”.">
                <TextInput name="serving_size" defaultValue={v.serving_size} maxLength={60} />
              </Labelled>
              <Labelled label="Serving weight (g)" hint="If measured.">
                <TextInput name="serving_size_g" inputMode="decimal" defaultValue={v.serving_size_g ?? ""} />
              </Labelled>
              <Labelled label="Kitchen preparation (minutes)">
                <TextInput type="number" name="kitchen_prep_minutes" min={0} defaultValue={v.kitchen_prep_minutes ?? ""} />
              </Labelled>
              <Labelled label="Customer cooking time (minutes)">
                <TextInput type="number" name="customer_cook_minutes" min={0} defaultValue={v.customer_cook_minutes ?? ""} />
              </Labelled>
            </Field>
            <Checks legend="Dietary tags" name="dietary_tags" options={reference.dietary} selected={v.dietary_tags} hint="Tag only what the recipe truly is." />
            <Labelled label="What changed in this version">
              <TextInput name="change_summary" defaultValue={v.change_summary} maxLength={500} />
            </Labelled>
          </KForm>
        ) : (
          <dl className="type-body-sm grid gap-2">
            <div>
              Makes {v.yield_servings} servings{v.serving_size ? ` of ${v.serving_size}` : ""}
              {v.serving_size_g ? ` (${v.serving_size_g} g)` : ""}
            </div>
            <div>Kitchen preparation: {v.kitchen_prep_minutes ?? "—"} min · customer cooking: {v.customer_cook_minutes ?? "—"} min</div>
            <div>Dietary tags: {v.dietary_tags.length ? v.dietary_tags.map(humanize).join(", ") : "none"}</div>
          </dl>
        )}
      </Panel>
    </div>
  );
}

async function Ingredients({ v, recipeId, editable, token }: { v: RecipeVersion; recipeId: string; editable: boolean; token: string }) {
  if (!editable) {
    return (
      <Panel title="Bill of materials" note={`For ${v.yield_servings} servings. Usable share is applied only where a verified figure was recorded.`}>
        <DataTable head={["Ingredient", "Quantity", "Preparation", "Usable share", "Substitution"]} minWidth="40rem">
          {v.ingredients.map((l) => (
            <tr key={l.id}>
              <td className="p-3 font-semibold">
                {l.ingredient_name}
                {l.optional ? <span className="type-caption block text-muted">Optional</span> : null}
              </td>
              <td className="p-3">
                {l.quantity} {l.unit}
              </td>
              <td className="p-3">{l.preparation || "—"}</td>
              <td className="p-3">{l.yield_percent ? `${l.yield_percent}%` : "—"}</td>
              <td className="p-3">{humanize(l.substitution)}</td>
            </tr>
          ))}
        </DataTable>
      </Panel>
    );
  }
  const [ingredients, units] = await Promise.all([api.kitchen.ingredients(token, { active: "true", page_size: 200 }), api.kitchen.units(token)]);
  const rows = [...v.ingredients.map((l) => ({ ...l, blank: false })), ...Array.from({ length: Math.max(3, 8 - v.ingredients.length) }, () => null)];
  return (
    <Panel
      title="Ingredients"
      note={`Quantities for ${v.yield_servings} servings, as they go into the kit. Each line must convert to the ingredient's base unit: grams and kilograms always do; millilitres or pieces only if the ingredient has a recorded conversion. Leave a row blank to drop it.`}
    >
      <KForm action={saveDraft} submit="Save ingredients" pending="Saving…" label="Ingredients">
        <input type="hidden" name="id" value={recipeId} />
        <input type="hidden" name="step" value="ingredients" />
        <ol className="grid gap-4">
          {rows.map((l, i) => (
            <li key={l?.id ?? `new-${i}`} className="grid gap-3 border-b border-border pb-4 sm:grid-cols-[2fr_1fr_1fr_2fr] lg:grid-cols-[2fr_7rem_6rem_1.5fr_6rem_10rem]">
              <Labelled label={`Line ${i + 1}: ingredient`}>
                <SelectInput name="line_ingredient" defaultValue={l?.ingredient_id ?? ""}>
                  <option value="">—</option>
                  {ingredients.items.map((x) => (
                    <option key={x.id} value={x.id}>
                      {x.name} ({x.base_unit})
                    </option>
                  ))}
                  {l && !ingredients.items.some((x) => x.id === l.ingredient_id) ? <option value={l.ingredient_id}>{l.ingredient_name} (inactive)</option> : null}
                </SelectInput>
              </Labelled>
              <Labelled label="Quantity">
                <TextInput name="line_quantity" inputMode="decimal" defaultValue={l?.quantity ?? ""} />
              </Labelled>
              <Labelled label="Unit">
                <SelectInput name="line_unit" defaultValue={l?.unit ?? "g"}>
                  {units.items.map((u) => (
                    <option key={u.code}>{u.code}</option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Preparation" hint="diced, peeled…">
                <TextInput name="line_preparation" defaultValue={l?.preparation ?? ""} maxLength={100} />
              </Labelled>
              <Labelled label="Usable %" hint="If verified">
                <TextInput name="line_yield" inputMode="decimal" defaultValue={l?.yield_percent ?? ""} />
              </Labelled>
              <Labelled label="Substitution">
                <SelectInput name="line_substitution" defaultValue={l?.substitution ?? "none"}>
                  <option value="none">None</option>
                  <option value="equivalent_allowed">Equivalent allowed</option>
                  <option value="approval_required">Needs approval</option>
                </SelectInput>
              </Labelled>
              <input type="hidden" name="line_note" value={l?.note ?? ""} />
              <label className="type-caption inline-flex min-h-11 items-center gap-2">
                <input type="checkbox" name="line_optional" value={String(i)} defaultChecked={l?.optional ?? false} className="size-5" /> Optional
              </label>
            </li>
          ))}
        </ol>
        <p className="type-caption text-muted">
          Need an ingredient that is not listed? <Link href="/ingredients" className="underline underline-offset-4">Search the catalogue</Link> (the culinary team adds new ones). Saving
          more rows than shown? Save, and new blank rows appear.
        </p>
      </KForm>
    </Panel>
  );
}

function Method({ v, recipeId, editable }: { v: RecipeVersion; recipeId: string; editable: boolean }) {
  if (!editable) {
    const list = (title: string, steps: string[]) => (
      <Panel title={title}>
        {steps.length ? (
          <ol className="type-body grid list-decimal gap-2 pl-6">
            {steps.map((s, i) => (
              <li key={i}>{s}</li>
            ))}
          </ol>
        ) : (
          <p className="type-body-sm text-muted">None.</p>
        )}
      </Panel>
    );
    return (
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
        {list("Kitchen preparation", v.preparation_steps)}
        {list("Kit assembly", v.assembly_steps)}
        <Panel title="Customer, storage and packing">
          <dl className="type-body-sm grid gap-3">
            <div>
              <dt className="font-semibold">Recipe card (customer)</dt>
              <dd className="whitespace-pre-line">{v.customer_instructions || "—"}</dd>
            </div>
            <div>
              <dt className="font-semibold">Storage</dt>
              <dd>{v.storage_instructions || "—"}</dd>
            </div>
            <div>
              <dt className="font-semibold">Packing</dt>
              <dd>{v.packaging_instructions || "—"}</dd>
            </div>
          </dl>
        </Panel>
      </div>
    );
  }
  return (
    <Panel title="Method" note="One step per paragraph (leave a blank line between steps). Kitchen preparation is required before submission.">
      <KForm action={saveDraft} submit="Save method" pending="Saving…" label="Method">
        <input type="hidden" name="id" value={recipeId} />
        <input type="hidden" name="step" value="method" />
        <div className="grid gap-4 xl:grid-cols-2">
          <Labelled label="Kitchen preparation steps" hint="Washing, trimming, portioning, sauces: what Mise's kitchen does.">
            <TextArea name="preparation_steps" rows={10} defaultValue={v.preparation_steps.join("\n\n")} />
          </Labelled>
          <Labelled label="Kit assembly steps" hint="What goes in which bag, in what order.">
            <TextArea name="assembly_steps" rows={10} defaultValue={v.assembly_steps.join("\n\n")} />
          </Labelled>
        </div>
        <Labelled label="Recipe card for the customer" hint="How the customer cooks the kit at home.">
          <TextArea name="customer_instructions" rows={6} defaultValue={v.customer_instructions} maxLength={4000} />
        </Labelled>
        <Field>
          <Labelled label="Storage instructions" hint="Printed on the label. Only what an approved procedure says.">
            <TextArea name="storage_instructions" rows={2} defaultValue={v.storage_instructions} maxLength={1000} />
          </Labelled>
          <Labelled label="Packing instructions">
            <TextArea name="packaging_instructions" rows={2} defaultValue={v.packaging_instructions} maxLength={1000} />
          </Labelled>
        </Field>
      </KForm>
    </Panel>
  );
}

async function Safety({ v, recipeId, editable }: { v: RecipeVersion; recipeId: string; editable: boolean }) {
  const reference = await api.catalog.reference();
  const label = (k: string) => reference.meal_allergens.find((a) => a.key === k)?.label ?? humanize(k);
  return (
    <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
      <Panel
        title="Allergens"
        note="Declared allergens become the meal's customer-facing declaration when this version is published. An unticked allergen is never a “free from” claim."
      >
        <p className="type-body-sm mb-3">
          The ingredients contain: <strong>{v.ingredient_allergens.length ? v.ingredient_allergens.map(label).join(", ") : "none recorded"}</strong>
        </p>
        {v.missing_allergens.length ? <Notice tone="error" title="Not yet declared" items={v.missing_allergens.map(label)} /> : null}
        {editable ? (
          <div className="mt-4">
            <KForm action={saveDraft} submit="Save allergens and nutrition" pending="Saving…" label="Allergens and nutrition">
              <input type="hidden" name="id" value={recipeId} />
              <input type="hidden" name="step" value="safety" />
              <Checks legend="Contains" name="contains" options={reference.meal_allergens} selected={v.allergens_contains} />
              <Checks legend="May contain (cross-contact)" name="may_contain" options={reference.meal_allergens} selected={v.allergens_may_contain} />
              <Labelled label="Cross-contact note">
                <TextArea name="cross_contact_note" rows={2} defaultValue={v.cross_contact_note} maxLength={500} />
              </Labelled>
              <fieldset className="grid gap-3">
                <legend className="type-label mb-1">Nutrition per serving</legend>
                <p className="type-caption text-muted">Leave a figure blank if it is not known: incomplete data is shown as incomplete, and only verified figures reach customers.</p>
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                  {NUTRIENTS.map(([k, l]) => (
                    <Labelled key={k} label={l}>
                      <TextInput name={k} inputMode="decimal" defaultValue={(v.nutrition[k] as string | null) ?? ""} />
                    </Labelled>
                  ))}
                </div>
                <Field>
                  <Labelled label="Source of the figures">
                    <SelectInput name="source" defaultValue={v.nutrition.source ?? ""}>
                      <option value="">Not stated</option>
                      <Options values={NUTRITION_SOURCES} />
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Method or reference" hint="Lab report number, database and version…">
                    <TextInput name="method" defaultValue={v.nutrition.method} maxLength={500} />
                  </Labelled>
                </Field>
              </fieldset>
            </KForm>
          </div>
        ) : (
          <dl className="type-body-sm mt-3 grid gap-2">
            <div>Contains: {v.allergens_contains.length ? v.allergens_contains.map(label).join(", ") : "none declared"}</div>
            <div>May contain: {v.allergens_may_contain.length ? v.allergens_may_contain.map(label).join(", ") : "none declared"}</div>
            {v.cross_contact_note ? <div>{v.cross_contact_note}</div> : null}
          </dl>
        )}
      </Panel>
      <Panel title="Nutrition status">
        <p className="type-body-sm">
          <StatusTag status={v.nutrition.verified ? "passed" : v.nutrition.complete ? "pending" : "draft"} label={v.nutrition.verified ? "Verified by a nutritionist" : v.nutrition.complete ? "Complete, not yet verified" : "Incomplete"} />
        </p>
        <DataTable head={["Per serving", "Value"]} minWidth="16rem">
          {NUTRIENTS.map(([k, l]) => (
            <tr key={k}>
              <td className="p-3">{l}</td>
              <td className="p-3">{(v.nutrition[k] as string | null) ?? <span className="text-muted">Not provided</span>}</td>
            </tr>
          ))}
        </DataTable>
        <p className="type-caption mt-2 text-muted">
          Source: {v.nutrition.source ? humanize(v.nutrition.source) : "not stated"}
          {v.nutrition.method ? ` · ${v.nutrition.method}` : ""}
        </p>
      </Panel>
    </div>
  );
}

function Review({ v, recipeId, latest, canEdit, perms }: { v: RecipeVersion; recipeId: string; latest: boolean; canEdit: boolean; perms: string[] }) {
  const has = (p: string) => perms.includes(p);
  const actions: ReactNode[] = [];
  const hidden = (action: string) => (
    <>
      <input type="hidden" name="id" value={recipeId} />
      <input type="hidden" name="action" value={action} />
    </>
  );
  if (latest) {
    if ((v.status === "draft" || v.status === "changes_requested") && canEdit && has("recipes.submit")) {
      actions.push(
        <Panel key="submit" title="Submit for review" note="The culinary team reviews first, then a nutritionist. Editing after submission is not possible until changes are requested.">
          <KForm action={recipeAction} submit="Submit for review" pending="Submitting…" label="Submit for review">
            {hidden("submit")}
            <Labelled label="Note to reviewers (optional)">
              <TextArea name="notes" rows={2} maxLength={500} />
            </Labelled>
          </KForm>
        </Panel>,
      );
    }
    if (v.status === "submitted" && has("recipes.review")) {
      actions.push(
        <Panel key="culinary" title="Culinary review" note="You cannot review a version you wrote.">
          <KForm action={recipeAction} submit="Approve (to nutrition review)" pending="Saving…" label="Culinary review">
            {hidden("review")}
            <input type="hidden" name="decision" value="approved" />
            <Labelled label="Notes (optional)">
              <TextArea name="notes" rows={2} maxLength={2000} />
            </Labelled>
          </KForm>
        </Panel>,
      );
    }
    if ((v.status === "submitted" || v.status === "under_review") && has("recipes.review")) {
      actions.push(
        <Panel key="changes" title="Send back to the chef">
          <KForm action={recipeAction} submit="Send back" pending="Saving…" label="Request changes" variant="secondary">
            {hidden("request-changes")}
            <Labelled label="Decision">
              <SelectInput name="decision" defaultValue="changes_requested">
                <option value="changes_requested">Changes requested</option>
                <option value="rejected">Rejected as incomplete</option>
              </SelectInput>
            </Labelled>
            <Labelled label="What needs to change">
              <TextArea name="notes" rows={3} required maxLength={2000} />
            </Labelled>
          </KForm>
        </Panel>,
      );
    }
    if (v.status === "under_review" && !v.signoff.nutrition && (has("nutrition.approve") || has("nutrition.review"))) {
      actions.push(
        <Panel key="nutrition" title="Nutrition and allergen review" note="Sign-off needs every figure with its source, and every ingredient allergen declared. It applies to this version only.">
          {has("nutrition.approve") ? (
            <KForm action={nutritionAction} submit="Sign off nutrition and allergens" pending="Saving…" label="Nutrition sign-off">
              <input type="hidden" name="id" value={recipeId} />
              <input type="hidden" name="action" value="approve" />
              <Labelled label="Review notes">
                <TextArea name="notes" rows={2} maxLength={2000} />
              </Labelled>
            </KForm>
          ) : null}
          <div className="mt-5">
            <KForm action={nutritionAction} submit="Send back" pending="Saving…" label="Nutrition changes" variant="secondary">
              <input type="hidden" name="id" value={recipeId} />
              <input type="hidden" name="action" value="request-changes" />
              <Labelled label="Decision">
                <SelectInput name="decision" defaultValue="changes_requested">
                  <option value="changes_requested">Changes requested</option>
                  <option value="rejected">Rejected as incomplete</option>
                </SelectInput>
              </Labelled>
              <Labelled label="What needs to change">
                <TextArea name="notes" rows={3} maxLength={2000} />
              </Labelled>
            </KForm>
          </div>
        </Panel>,
      );
    }
    if (v.status === "under_review" && has("recipes.approve")) {
      actions.push(
        <Panel key="approve" title="Final approval" note="Needs a valid culinary approval and a nutrition sign-off. The version then never changes.">
          <KForm action={recipeAction} submit="Approve this version" pending="Approving…" label="Final approval">
            {hidden("approve")}
            <Labelled label="Notes (optional)">
              <TextArea name="notes" rows={2} maxLength={2000} />
            </Labelled>
          </KForm>
        </Panel>,
      );
    }
    if (v.status === "approved" && has("recipes.publish")) {
      actions.push(
        <Panel key="publish" title="Publish for production" note="Used for delivery weeks from the chosen Monday (selection must still be open). Sets the meal's allergens and verified nutrition.">
          <KForm action={recipeAction} submit="Publish" pending="Publishing…" label="Publish">
            {hidden("publish")}
            <Labelled label="First delivery week (a Monday)" hint="Leave blank for the next open week.">
              <TextInput type="date" name="effective_from" />
            </Labelled>
          </KForm>
        </Panel>,
      );
    }
    if (v.status === "published" && has("recipes.publish")) {
      actions.push(
        <Panel key="archive" title="Withdraw" note="Stops this version being used for future plans. Batches already made keep it.">
          <KForm action={recipeAction} submit="Withdraw version" pending="Saving…" label="Withdraw" variant="secondary">
            {hidden("archive")}
            <input type="hidden" name="version" value={v.number} />
            <Labelled label="Reason">
              <TextInput name="reason" required maxLength={500} />
            </Labelled>
          </KForm>
        </Panel>,
      );
    }
    if ((v.status === "approved" || v.status === "published" || v.status === "archived") && canEdit) {
      actions.push(
        <Panel key="revise" title="Change this recipe" note="Starts a new draft version from this one. No review or approval is carried over.">
          <KForm action={recipeAction} submit="Start a revision" pending="Starting…" label="Start a revision" variant="secondary">
            {hidden("revise")}
            <Labelled label="What will change">
              <TextInput name="change_summary" maxLength={500} />
            </Labelled>
          </KForm>
        </Panel>,
      );
    }
  }
  return (
    <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[1fr_26rem]">
      <div className="grid content-start gap-6">
        <Notice tone="warning" title="Before approval" items={v.problems} />
        <Panel title="Sign-offs on this version">
          <ul className="type-body-sm grid gap-2">
            <li>
              <StatusTag status={v.signoff.culinary ? "passed" : "pending"} label={v.signoff.culinary ? "Done" : "Not yet"} /> Culinary review
            </li>
            <li>
              <StatusTag status={v.signoff.nutrition ? "passed" : "pending"} label={v.signoff.nutrition ? "Done" : "Not yet"} /> Nutrition and allergens
            </li>
            <li>
              <StatusTag status={v.signoff.final ? "passed" : "pending"} label={v.signoff.final ? "Done" : "Not yet"} /> Final approval
              {v.approved_by_name ? ` by ${v.approved_by_name}` : ""}
            </li>
          </ul>
        </Panel>
        <Panel title="Review history" note="Reviews given before a later edit are marked as no longer counting.">
          <Timeline
            empty="No reviews yet."
            items={v.reviews.map((r) => ({
              at: r.created_at,
              title: `${STAGE_LABEL[r.stage] ?? r.stage}: ${humanize(r.decision)}${r.reviewer_name ? ` · ${r.reviewer_name}` : ""}${r.invalidated_at ? " (no longer counts)" : ""}`,
              detail: r.notes ? <p className="whitespace-pre-line">{r.notes}</p> : undefined,
            }))}
          />
        </Panel>
      </div>
      <div className="grid content-start gap-6">{actions.length ? actions : <Panel title="Next step">Nothing for you to do on this version.</Panel>}</div>
    </div>
  );
}

async function CostingView({ token, recipeId, n }: { token: string; recipeId: string; n: number }) {
  const c = await api.kitchen.costing(token, recipeId, n);
  return (
    <Panel title={`Estimated cost, version ${c.version}`} note={c.note}>
      <p className="type-body-sm mb-3">
        <StatusTag status={c.complete ? "passed" : "pending"} label={c.complete ? "Complete estimate" : "Incomplete estimate"} />{" "}
        {c.complete ? "" : "Some lines or the packaging have no reference cost, so totals are lower bounds, not costs."}
      </p>
      <DataTable head={["Ingredient", "Quantity", "Estimated cost", "Source"]} minWidth="32rem">
        {c.lines.map((l, i) => (
          <tr key={i}>
            <td className="p-3">{l.ingredient_name}</td>
            <td className="p-3">
              {l.quantity} {l.unit}
            </td>
            <td className="p-3">{l.cost_minor != null ? money(l.cost_minor, c.currency) : <span className="text-muted">{l.status === "unconvertible" ? "Unit cannot be converted" : "No reference cost"}</span>}</td>
            <td className="p-3">{l.source ?? "—"}</td>
          </tr>
        ))}
      </DataTable>
      <dl className="type-body-sm mt-4 grid gap-1">
        <div>
          Ingredients ({c.yield_servings} servings): <strong>{money(c.ingredients_minor, c.currency)}</strong>
        </div>
        <div>Per serving: {money(c.per_serving_minor, c.currency)}</div>
        <div>Packaging per kit: {c.packaging_minor != null ? money(c.packaging_minor, c.currency) : "no reference"}</div>
      </dl>
    </Panel>
  );
}
