import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, Pager, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { CATEGORY_OPTIONS, Field, Options } from "@/components/kitchen-ui";
import { humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { saveIngredient } from "../kitchen-actions";

export const metadata: Metadata = { title: "Ingredients" };

type Search = { q?: string; category?: string; active?: string; page?: string };

/** The shared ingredient catalogue: one entry per ingredient, found by name or alias. */
export default async function IngredientsPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "ingredients.read")) notFound();
  const sp = await searchParams;
  const page = Math.max(Number(sp.page) || 1, 1);
  const [list, units] = await Promise.all([api.kitchen.ingredients(token, { ...sp, page, page_size: 50 }), api.kitchen.units(token)]);
  return (
    <>
      <PageHeader
        title="Ingredients"
        description="The one ingredient catalogue recipes, production and (from Sprint 6) procurement share. Search finds aliases too, so look before adding: a near-duplicate name is refused."
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <div>
          <FilterBar>
            <Labelled label="Name or alias">
              <TextInput name="q" defaultValue={sp.q} maxLength={100} className="w-56" />
            </Labelled>
            <Labelled label="Category">
              <SelectInput name="category" defaultValue={sp.category ?? ""}>
                <option value="">Any</option>
                <Options values={CATEGORY_OPTIONS} />
              </SelectInput>
            </Labelled>
            <Labelled label="Status">
              <SelectInput name="active" defaultValue={sp.active ?? ""}>
                <option value="">Any</option>
                <option value="true">Active</option>
                <option value="false">Inactive</option>
              </SelectInput>
            </Labelled>
          </FilterBar>
          {list.items.length ? (
            <DataTable head={["Ingredient", "Category", "Counted in", "Allergens", "Used in", "Status"]} minWidth="40rem">
              {list.items.map((i) => (
                <tr key={i.id}>
                  <td className="p-3">
                    <Link href={`/ingredients/${i.id}`} className="font-semibold underline underline-offset-4">
                      {i.name}
                    </Link>
                    {i.aliases.length ? <span className="type-caption block text-muted">Also: {i.aliases.join(", ")}</span> : null}
                  </td>
                  <td className="p-3">{humanize(i.category)}</td>
                  <td className="p-3">{i.base_unit}</td>
                  <td className="p-3">{i.allergens.length ? i.allergens.map(humanize).join(", ") : <span className="text-muted">None recorded</span>}</td>
                  <td className="p-3">{i.used_in} recipes</td>
                  <td className="p-3">
                    <StatusTag status={i.active ? "active" : "retired"} label={i.active ? "Active" : "Inactive"} />
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>No ingredients match. Real ingredients are added by the culinary team; none are pre-loaded.</EmptyState>
          )}
          <Pager page={page} size={50} total={list.total} params={{ q: sp.q, category: sp.category, active: sp.active }} />
        </div>
        {can(admin, "ingredients.manage") ? (
          <Panel title="Add an ingredient" note="Requirements are counted in the base unit. Volume and pieces convert to it only through a conversion you record on the ingredient.">
            <KForm action={saveIngredient} submit="Add ingredient" pending="Adding…" label="Add an ingredient">
              <Labelled label="Name">
                <TextInput name="name" required maxLength={100} />
              </Labelled>
              <Field>
                <Labelled label="Category">
                  <SelectInput name="category" defaultValue="produce">
                    <Options values={CATEGORY_OPTIONS} />
                  </SelectInput>
                </Labelled>
                <Labelled label="Base unit">
                  <SelectInput name="base_unit" defaultValue="g">
                    {units.items.map((u) => (
                      <option key={u.code} value={u.code}>
                        {u.name} ({u.code})
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
              </Field>
              <Labelled label="Aliases" hint="Other names, separated by commas.">
                <TextInput name="aliases" maxLength={600} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
