import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { CATEGORY_OPTIONS, Checks, Field, Options, PREP_STATES, STORAGE } from "@/components/kitchen-ui";
import { formatDate, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { addCost, saveIngredient, setConversion } from "../../kitchen-actions";

export const metadata: Metadata = { title: "Ingredient" };

export default async function IngredientPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "ingredients.read")) notFound();
  const { id } = await params;
  const [ing, units, reference] = await Promise.all([
    api.kitchen.ingredient(token, id).catch((e) => {
      if (e instanceof ApiError && e.status === 404) notFound();
      throw e;
    }),
    api.kitchen.units(token),
    api.catalog.reference(),
  ]);
  const manage = can(admin, "ingredients.manage");
  const unitName = (c: string) => units.items.find((u) => u.code === c)?.name.toLowerCase() ?? c;
  return (
    <>
      <PageHeader
        back={{ href: "/ingredients", label: "All ingredients" }}
        title={ing.name}
        description={
          <>
            <StatusTag status={ing.active ? "active" : "retired"} label={ing.active ? "Active" : "Inactive"} /> · {humanize(ing.category)} · counted in {unitName(ing.base_unit)} · used in {ing.used_in} recipes
          </>
        }
      />
      <div className="grid gap-6 xl:grid-cols-2">
        <Panel title="Details" note={manage ? "Changes apply to drafts and future plans. Approved recipe versions keep what they were approved with." : undefined}>
          {manage ? (
            <KForm action={saveIngredient} submit="Save ingredient" pending="Saving…" label="Edit ingredient">
              <input type="hidden" name="id" value={ing.id} />
              <input type="hidden" name="allergens_present" value="1" />
              <input type="hidden" name="active_present" value="1" />
              <Labelled label="Name">
                <TextInput name="name" defaultValue={ing.name} required maxLength={100} />
              </Labelled>
              <Labelled label="Aliases" hint="Other names, separated by commas. Each name or alias belongs to one ingredient.">
                <TextInput name="aliases" defaultValue={ing.aliases.join(", ")} />
              </Labelled>
              <Field>
                <Labelled label="Category">
                  <SelectInput name="category" defaultValue={ing.category}>
                    <Options values={CATEGORY_OPTIONS} />
                  </SelectInput>
                </Labelled>
                <Labelled label="Base unit" hint="Fixed once in an approved recipe.">
                  <SelectInput name="base_unit" defaultValue={ing.base_unit}>
                    {units.items.map((u) => (
                      <option key={u.code} value={u.code}>
                        {u.name}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Preparation state">
                  <SelectInput name="preparation_state" defaultValue={ing.preparation_state}>
                    <Options values={PREP_STATES} />
                  </SelectInput>
                </Labelled>
                <Labelled label="Purchasing unit" hint="As bought, for example a 2 kg bag.">
                  <TextInput name="purchasing_unit" defaultValue={ing.purchasing_unit ?? ""} maxLength={60} />
                </Labelled>
                <Labelled label="Storage">
                  <SelectInput name="storage" defaultValue={ing.storage}>
                    <Options values={STORAGE} />
                  </SelectInput>
                </Labelled>
                <Labelled label="Shelf life" hint="Only when verified; leave blank otherwise.">
                  <TextInput name="shelf_life_note" defaultValue={ing.shelf_life_note ?? ""} maxLength={300} />
                </Labelled>
              </Field>
              <Checks
                legend="Contains these allergens"
                name="allergens"
                options={reference.meal_allergens}
                selected={ing.allergens}
                hint="Recipes using it must declare these before approval. No tick never means allergen-free."
              />
              <Labelled label="Storage notes">
                <TextArea name="storage_notes" defaultValue={ing.storage_notes} rows={2} maxLength={500} />
              </Labelled>
              <Labelled label="Description">
                <TextArea name="description" defaultValue={ing.description} rows={2} maxLength={1000} />
              </Labelled>
              <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
                <input type="checkbox" name="active" defaultChecked={ing.active} className="size-5" /> Active (inactive ingredients block plan approval)
              </label>
            </KForm>
          ) : (
            <dl className="type-body-sm grid gap-2">
              <div>Allergens: {ing.allergens.length ? ing.allergens.map(humanize).join(", ") : "none recorded"}</div>
              <div>Storage: {humanize(ing.storage)}</div>
              <div>Shelf life: {ing.shelf_life_note ?? "not recorded"}</div>
            </dl>
          )}
        </Panel>
        <div className="grid content-start gap-6">
          <Panel
            title="Conversions"
            note="Grams to kilograms and millilitres to litres convert exactly without anything here. Volume or pieces become weight only through a figure recorded for this ingredient, with its source."
          >
            {ing.conversions?.length ? (
              <ul className="type-body-sm mb-4 grid gap-2">
                {ing.conversions.map((c) => (
                  <li key={c.id}>
                    1 {c.from_unit} = {c.factor} {c.to_unit} {c.approximate ? <StatusTag status="pending" label="Average" /> : null}
                    <span className="type-caption block text-muted">Source: {c.source}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="type-body-sm mb-4 text-muted">None recorded.</p>
            )}
            {manage ? (
              <KForm action={setConversion} submit="Save conversion" pending="Saving…" label="Record a conversion" reset>
                <input type="hidden" name="id" value={ing.id} />
                <div className="grid grid-cols-[auto_1fr_auto_1fr] items-end gap-2">
                  <span className="type-body-sm pb-3">1</span>
                  <Labelled label="From">
                    <SelectInput name="from_unit" defaultValue="ml">
                      {units.items.map((u) => (
                        <option key={u.code}>{u.code}</option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <span className="type-body-sm pb-3">=</span>
                  <Labelled label="Amount">
                    <TextInput name="factor" inputMode="decimal" required />
                  </Labelled>
                </div>
                <Labelled label="To">
                  <SelectInput name="to_unit" defaultValue={ing.base_unit}>
                    {units.items.map((u) => (
                      <option key={u.code}>{u.code}</option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Source" hint="Supplier specification, measured average, reference table.">
                  <TextInput name="source" required maxLength={300} />
                </Labelled>
                <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
                  <input type="checkbox" name="approximate" className="size-5" /> An average (required for pieces)
                </label>
              </KForm>
            ) : null}
          </Panel>
          <Panel title="Reference costs" note="For estimates only, never a purchase price. Sprint 6 procurement will record actual prices.">
            {ing.costs?.length ? (
              <DataTable head={["From", "Cost", "Source"]} minWidth="24rem">
                {ing.costs.map((c) => (
                  <tr key={c.id}>
                    <td className="p-3">{formatDate(c.effective_from)}</td>
                    <td className="p-3">
                      {money(c.amount_minor, c.currency)} per {c.per_quantity} {c.unit}
                    </td>
                    <td className="p-3">{c.source}</td>
                  </tr>
                ))}
              </DataTable>
            ) : (
              <EmptyState>No reference cost. Recipe estimates using this ingredient show as incomplete.</EmptyState>
            )}
            {manage ? (
              <div className="mt-4">
                <KForm action={addCost} submit="Record cost" pending="Saving…" label="Record a reference cost" reset>
                  <input type="hidden" name="ingredient_id" value={ing.id} />
                  <Field>
                    <Labelled label="Amount (TTD)">
                      <TextInput name="amount" inputMode="decimal" required />
                    </Labelled>
                    <Labelled label="Per">
                      <div className="flex gap-2">
                        <TextInput name="per_quantity" inputMode="decimal" defaultValue="1" required />
                        <SelectInput name="unit" defaultValue={ing.base_unit === "g" ? "kg" : ing.base_unit}>
                          {units.items.map((u) => (
                            <option key={u.code}>{u.code}</option>
                          ))}
                        </SelectInput>
                      </div>
                    </Labelled>
                    <Labelled label="Applies from">
                      <TextInput type="date" name="effective_from" required />
                    </Labelled>
                    <Labelled label="Source">
                      <TextInput name="source" required maxLength={300} />
                    </Labelled>
                  </Field>
                </KForm>
              </div>
            ) : null}
          </Panel>
        </div>
      </div>
    </>
  );
}
