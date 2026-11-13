import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, Pager, StatusTag, Tabs } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createRecipe } from "../kitchen-actions";

export const metadata: Metadata = { title: "Recipes" };

const STATUSES = ["draft", "submitted", "under_review", "changes_requested", "approved", "published", "archived"];
type Search = { q?: string; status?: string; tab?: string; page?: string };

/**
 * The chef workspace: "My recipes" first for chefs, every recipe for the
 * culinary team. A recipe is the operational source of a catalogue meal.
 */
export default async function RecipesPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "recipes.read")) notFound();
  const sp = await searchParams;
  const chefOnly = !can(admin, "recipes.review") && can(admin, "recipes.create");
  const tab = sp.tab ?? (chefOnly ? "mine" : "all");
  const page = Math.max(Number(sp.page) || 1, 1);
  const [list, meals] = await Promise.all([
    api.kitchen.recipes(token, { q: sp.q, status: sp.status, mine: tab === "mine", page }),
    can(admin, "catalog.read") && can(admin, "recipes.create") ? api.admin.catalogMeals(token, {}) : Promise.resolve(null),
  ]);
  return (
    <>
      <PageHeader
        title={tab === "mine" ? "My recipes" : "Recipes"}
        description="Each recipe has versions. Drafts are edited step by step, then reviewed by the culinary team and a nutritionist. Approved versions never change; a change is a new revision."
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <div>
          <Tabs
            base={`/recipes${sp.q || sp.status ? `?${new URLSearchParams({ ...(sp.q ? { q: sp.q } : {}), ...(sp.status ? { status: sp.status } : {}) })}` : ""}`}
            current={tab}
            tabs={chefOnly ? [{ key: "mine", label: "Mine" }, { key: "all", label: "All recipes" }] : [{ key: "all", label: "All recipes" }, { key: "mine", label: "Mine" }]}
          />
          <div className="mt-4">
            <FilterBar>
              <input type="hidden" name="tab" value={tab} />
              <Labelled label="Name or code">
                <TextInput name="q" defaultValue={sp.q} maxLength={100} className="w-56" />
              </Labelled>
              <Labelled label="Latest version">
                <SelectInput name="status" defaultValue={sp.status ?? ""}>
                  <option value="">Any</option>
                  {STATUSES.map((s) => (
                    <option key={s} value={s}>
                      {humanize(s)}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
            </FilterBar>
          </div>
          {list.items.length ? (
            <DataTable head={["Recipe", "Meal", "Chef", "Latest version", "Updated"]} minWidth="40rem">
              {list.items.map((r) => (
                <tr key={r.id}>
                  <td className="p-3">
                    <Link href={`/recipes/${r.id}`} className="font-semibold underline underline-offset-4">
                      {r.name}
                    </Link>
                    <span className="type-caption block text-muted">{r.code}</span>
                  </td>
                  <td className="p-3">{r.meal_name ?? <span className="text-muted">Not linked</span>}</td>
                  <td className="p-3">{r.chef_name ?? "—"}</td>
                  <td className="p-3">
                    v{r.latest.number} <StatusTag status={r.latest.status} />
                  </td>
                  <td className="p-3">{formatDateTime(r.latest.updated_at)}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>{tab === "mine" ? "You have no recipes yet. Start one on the right." : "No recipes match."}</EmptyState>
          )}
          <Pager page={page} size={50} total={list.total} params={{ q: sp.q, status: sp.status, tab }} />
        </div>
        {can(admin, "recipes.create") ? (
          <Panel title="Start a recipe" note="Quantities, steps and nutrition come next, one step at a time.">
            <KForm action={createRecipe} submit="Start recipe" pending="Starting…" label="Start a recipe">
              <Labelled label="Name">
                <TextInput name="name" required maxLength={100} />
              </Labelled>
              <Labelled label="Category" hint="Optional, for example Caribbean classics.">
                <TextInput name="category" maxLength={60} />
              </Labelled>
              {meals ? (
                <Labelled label="Catalogue meal" hint="The meal this recipe produces. Can be linked later.">
                  <SelectInput name="meal_id" defaultValue="">
                    <option value="">Not yet</option>
                    {meals.items.map((m) => (
                      <option key={m.id} value={m.id}>
                        {m.name}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
              ) : null}
            </KForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
