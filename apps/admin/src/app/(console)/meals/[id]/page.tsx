import { ApiError } from "@mise/api-client";
import { buttonClasses, Heading, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { CatalogForm } from "@/components/catalog-form";
import { MealEditor } from "@/components/meal-editor";
import { Panel } from "@/components/panel";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { saveNutrition, setStatus } from "../actions";

export const metadata: Metadata = { title: "Meal" };

const problems: Record<string, string> = {
  image_required: "Add at least one photograph before publishing.",
  demo_content: "This is demo content. It can be published only where demo content is enabled.",
  forbidden: "Your role does not allow publishing.",
};

type Search = Promise<{ created?: string; published?: string; unpublished?: string; problem?: string }>;

export default async function MealPage({ params, searchParams }: { params: Promise<{ id: string }>; searchParams: Search }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "catalog.read")) notFound();
  const { id } = await params;
  const flags = await searchParams;
  const meal = await api.admin.catalogMeal(token, id).catch((error) => {
    if (error instanceof ApiError && error.status === 404) notFound();
    throw error;
  });
  const [reference, categories, chefs] = await Promise.all([
    api.catalog.reference(),
    api.admin.catalogCategories(token),
    api.admin.catalogChefs(token),
  ]);
  const n = meal.nutrition;
  const input = "min-h-11 w-full rounded-md border border-border-strong bg-surface px-3";

  return (
    <>
      <Link href="/meals" className="type-body-sm underline underline-offset-4">
        All meals
      </Link>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Heading as="h1" size="h2">
          {meal.name}
        </Heading>
        <Tag tone={meal.status === "published" ? "success" : meal.status === "draft" ? "neutral" : "warning"}>{meal.status}</Tag>
        {meal.is_demo ? <Tag tone="warning">Demo</Tag> : null}
      </div>
      <p className="type-caption mt-2 text-muted">
        Version {meal.version}, changed {formatDateTime(meal.updated_at)}
        {meal.published_at ? `. First published ${formatDateTime(meal.published_at)}` : ""}.
      </p>

      <div aria-live="polite" className="mt-4">
        {flags.created ? <p className="type-body-sm border-l-2 border-success pl-3">Draft created.</p> : null}
        {flags.published ? <p className="type-body-sm border-l-2 border-success pl-3">Published. Customers can see it now.</p> : null}
        {flags.unpublished ? <p className="type-body-sm border-l-2 border-success pl-3">Unpublished. Customers no longer see it.</p> : null}
        {flags.problem ? (
          <p role="alert" className="type-body-sm border-l-2 border-error pl-3 font-semibold text-error">
            {problems[flags.problem] ?? "That did not work. Try again."}
          </p>
        ) : null}
      </div>

      {can(admin, "catalog.publish") ? (
        <form action={setStatus} className="mt-5">
          <input type="hidden" name="id" value={meal.id} />
          {meal.status === "published" ? (
            <button type="submit" name="action" value="unpublish" className={buttonClasses("secondary")}>
              Unpublish
            </button>
          ) : (
            <button type="submit" name="action" value="publish" className={buttonClasses("primary")}>
              Publish
            </button>
          )}
        </form>
      ) : null}

      <div className="mt-8 grid gap-8 xl:grid-cols-[1fr_20rem]">
        <div className="border border-border bg-surface p-5 sm:p-6">
          {can(admin, "catalog.manage") ? (
            <MealEditor meal={meal} reference={reference} categories={categories.items} chefs={chefs.items} />
          ) : (
            <p className="type-body-sm text-muted">Your role can read this meal but not change it.</p>
          )}
        </div>

        <Panel title="Nutrition per serving" note={n ? (n.status === "verified" ? "Verified" : "Pending") : "None"}>
          {can(admin, "nutrition.verify") ? (
            <CatalogForm action={saveNutrition} label="Nutrition" submit="Save nutrition" pending="Saving…">
              <input type="hidden" name="id" value={meal.id} />
              {(
                [
                  ["calories", "Energy (kcal)", n?.calories],
                  ["protein_g", "Protein (g)", n?.protein_g],
                  ["carbs_g", "Carbohydrate (g)", n?.carbs_g],
                  ["fat_g", "Fat (g)", n?.fat_g],
                  ["fibre_g", "Fibre (g), optional", n?.fibre_g],
                  ["sodium_mg", "Sodium (mg), optional", n?.sodium_mg],
                ] as const
              ).map(([name, label, value]) => (
                <label key={name} className="grid gap-1.5">
                  <span className="type-label">{label}</span>
                  <input name={name} type="number" step="0.1" min={0} defaultValue={value ?? ""} className={input} />
                </label>
              ))}
              <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
                <input type="checkbox" name="verified" defaultChecked={n?.status === "verified"} className="size-5" />
                I have checked these against the final recipe. Show them to customers.
              </label>
            </CatalogForm>
          ) : (
            <p className="type-body-sm text-muted">
              {n
                ? `${n.calories} kcal, ${n.protein_g} g protein (${n.status}).`
                : "No figures yet."}{" "}
              Only a nutritionist can enter or verify nutrition.
            </p>
          )}
        </Panel>
      </div>
    </>
  );
}
