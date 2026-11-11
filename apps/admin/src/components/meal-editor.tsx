import type { AdminCategory, AdminChef, AdminMeal, Reference } from "@mise/api-client";
import type { ReactNode } from "react";

import { saveMeal } from "@/app/(console)/meals/actions";

import { CatalogForm } from "./catalog-form";

const input = "min-h-11 w-full rounded-md border border-border-strong bg-surface px-3";

function Label({ text, hint, children }: { text: string; hint?: string; children: ReactNode }) {
  return (
    <label className="grid gap-1.5">
      <span className="type-label">{text}</span>
      {children}
      {hint ? <span className="type-caption text-muted">{hint}</span> : null}
    </label>
  );
}

function Checks({ legend, name, options, selected, hint }: { legend: string; name: string; options: { key: string; label: string }[]; selected: string[]; hint?: string }) {
  return (
    <fieldset>
      <legend className="type-label mb-1.5">{legend}</legend>
      {hint ? <p className="type-caption mb-2 text-muted">{hint}</p> : null}
      <div className="flex flex-wrap gap-x-5 gap-y-2">
        {options.map((o) => (
          <label key={o.key} className="type-body-sm inline-flex min-h-11 items-center gap-2">
            <input type="checkbox" name={name} value={o.key} defaultChecked={selected.includes(o.key)} className="size-5 accent-[var(--color-primary)]" />
            {o.label}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

/**
 * The whole meal on one page. Photographs and ingredients are one per line,
 * fields separated by "|". Saving a published meal changes what customers
 * see at once; unpublish first to rework it privately.
 */
export function MealEditor({
  meal,
  reference,
  categories,
  chefs,
}: {
  meal?: AdminMeal;
  reference: Reference;
  categories: AdminCategory[];
  chefs: AdminChef[];
}) {
  return (
    <CatalogForm action={saveMeal} label={meal ? "Edit meal" : "New meal"} submit={meal ? "Save meal" : "Create draft"} pending="Saving…">
      {meal ? (
        <>
          <input type="hidden" name="id" value={meal.id} />
          <input type="hidden" name="version" value={meal.version} />
        </>
      ) : null}
      <div className="grid gap-5 md:grid-cols-2">
        <Label text="Name">
          <input name="name" required maxLength={100} defaultValue={meal?.name} className={input} />
        </Label>
        <Label text="Web address" hint="Made from the name when left empty. Changing it breaks saved links.">
          <input name="slug" maxLength={80} defaultValue={meal?.slug} className={input} />
        </Label>
      </div>
      <Label text="Summary" hint="One sentence for meal cards, up to 200 characters.">
        <input name="summary" required maxLength={200} defaultValue={meal?.summary} className={input} />
      </Label>
      <Label text="Description">
        <textarea name="description" rows={4} maxLength={3000} defaultValue={meal?.description} className={`${input} py-2`} />
      </Label>
      <Label text="How it is cooked" hint="An overview only. The full method is on the recipe card.">
        <textarea name="cooking_overview" rows={3} maxLength={3000} defaultValue={meal?.cooking_overview} className={`${input} py-2`} />
      </Label>
      <div className="grid gap-5 md:grid-cols-3">
        <Label text="Category">
          <select name="category_id" required defaultValue={meal?.category_id ?? ""} className={input}>
            <option value="">Choose…</option>
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
                {c.is_demo ? " (demo)" : ""}
              </option>
            ))}
          </select>
        </Label>
        <Label text="Chef" hint="Only published profiles are shown to customers.">
          <select name="chef_id" defaultValue={meal?.chef_id ?? ""} className={input}>
            <option value="">None</option>
            {chefs.map((c) => (
              <option key={c.id} value={c.id}>
                {c.display_name} ({c.status})
              </option>
            ))}
          </select>
        </Label>
        <Label text="Main protein">
          <select name="protein" defaultValue={meal?.protein ?? ""} className={input}>
            <option value="">Not set</option>
            {reference.proteins.map((p) => (
              <option key={p.key} value={p.key}>
                {p.label}
              </option>
            ))}
          </select>
        </Label>
      </div>
      <div className="grid gap-5 sm:grid-cols-3">
        <Label text="Preparation (minutes)">
          <input name="prep_minutes" type="number" min={0} max={240} required defaultValue={meal?.prep_minutes ?? 15} className={input} />
        </Label>
        <Label text="Cooking (minutes)">
          <input name="cook_minutes" type="number" min={0} max={480} required defaultValue={meal?.cook_minutes ?? 30} className={input} />
        </Label>
        <Label text="Servings">
          <input name="servings" type="number" min={1} max={12} required defaultValue={meal?.servings ?? 2} className={input} />
        </Label>
      </div>
      <Checks legend="Dietary tags" name="dietary_tags" options={reference.dietary} selected={meal?.dietary_tags ?? []} hint="Customers filter on these. Tag only what the recipe truly is." />

      <fieldset className="border-l-4 border-accent bg-background p-4">
        <legend className="type-label px-1">Allergens</legend>
        <div className="mt-1 flex flex-wrap gap-x-5">
          <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
            <input type="radio" name="allergen_status" value="unconfirmed" defaultChecked={(meal?.allergen_status ?? "unconfirmed") === "unconfirmed"} />
            Not confirmed yet
          </label>
          <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
            <input type="radio" name="allergen_status" value="declared" defaultChecked={meal?.allergen_status === "declared"} />
            Declared by the kitchen
          </label>
        </div>
        <p className="type-caption mb-3 text-muted">
          Declare only after checking every ingredient and its packaging. Customers see what a meal contains, never that it is free of anything.
        </p>
        <Checks legend="The meal contains" name="allergens" options={reference.meal_allergens} selected={meal?.allergens ?? []} />
      </fieldset>

      <Label text="Photographs" hint="One per line: /catalog/file.jpg | description for people who cannot see it | credit. The first is the main photograph.">
        <textarea
          name="images"
          rows={3}
          defaultValue={meal?.images.map((i) => [i.src, i.alt, i.credit ?? ""].join(" | ")).join("\n")}
          className={`${input} py-2 font-mono text-[0.875rem]`}
        />
      </Label>
      <Label text="Ingredients in the kit" hint="One per line: name | quantity">
        <textarea
          name="ingredients"
          rows={6}
          defaultValue={meal?.ingredients.map((i) => [i.name, i.quantity ?? ""].join(" | ")).join("\n")}
          className={`${input} py-2 font-mono text-[0.875rem]`}
        />
      </Label>
      <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
        <input type="checkbox" name="is_demo" defaultChecked={meal?.is_demo} className="size-5" />
        Demo content (never shown or published where demo content is off)
      </label>
    </CatalogForm>
  );
}
