import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Options, WASTE_CATEGORIES } from "@/components/kitchen-ui";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { recordWaste } from "../../kitchen-actions";

export const metadata: Metadata = { title: "Waste and yield" };

type Search = { from?: string; to?: string };

/** Waste as weighed or counted, and links to the yield report. Nothing is estimated. */
export default async function WastePage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.read")) notFound();
  const sp = await searchParams;
  const [waste, units, ingredients, batches] = await Promise.all([
    api.kitchen.waste(token, { from: sp.from, to: sp.to }),
    api.kitchen.units(token),
    can(admin, "ingredients.read") ? api.kitchen.ingredients(token, { active: "true", page_size: 200 }) : Promise.resolve({ items: [] }),
    api.kitchen.batches(token, { open: true }),
  ]);
  return (
    <>
      <PageHeader
        title="Waste and yield"
        description={
          <>
            Record waste as it happens. Planned against accepted kits, rejected and reworked kits are in the{" "}
            {can(admin, "production_reports.read") ? (
              <Link href="/reports/production" className="underline underline-offset-4">
                production report
              </Link>
            ) : (
              "production report"
            )}
            .
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[1fr_24rem]">
        <div>
          <FilterBar>
            <Labelled label="From">
              <TextInput type="date" name="from" defaultValue={sp.from} />
            </Labelled>
            <Labelled label="To">
              <TextInput type="date" name="to" defaultValue={sp.to} />
            </Labelled>
          </FilterBar>
          {waste.items.length ? (
            <DataTable head={["When", "What", "Quantity", "Category", "Reason", "Batch", "By"]} minWidth="48rem">
              {waste.items.map((w) => (
                <tr key={w.id}>
                  <td className="p-3">{formatDateTime(w.created_at)}</td>
                  <td className="p-3">{w.ingredient_name ?? w.item}</td>
                  <td className="p-3">
                    {w.quantity} {w.unit}
                  </td>
                  <td className="p-3">{humanize(w.category)}</td>
                  <td className="p-3">{w.reason}</td>
                  <td className="p-3">
                    {w.batch_id ? (
                      <Link href={`/production/batches/${w.batch_id}`} className="underline underline-offset-4">
                        {w.batch_code}
                      </Link>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td className="p-3">{w.recorded_by_name}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>No waste recorded in this period.</EmptyState>
          )}
        </div>
        {can(admin, "waste.record") ? (
          <Panel title="Record waste">
            <KForm action={recordWaste} submit="Record" pending="Saving…" label="Record waste" size="lg" reset>
              <Labelled label="Ingredient">
                <SelectInput name="ingredient_id" defaultValue="">
                  <option value="">Not a catalogued ingredient</option>
                  {ingredients.items.map((i) => (
                    <option key={i.id} value={i.id}>
                      {i.name}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Or what was wasted" hint="A finished kit, packaging…">
                <TextInput name="item" maxLength={100} />
              </Labelled>
              <div className="grid grid-cols-[1fr_6rem] gap-3">
                <Labelled label="Quantity">
                  <TextInput name="quantity" inputMode="decimal" required className="text-[1.25rem]" />
                </Labelled>
                <Labelled label="Unit">
                  <SelectInput name="unit" defaultValue="g">
                    {units.items.map((u) => (
                      <option key={u.code}>{u.code}</option>
                    ))}
                  </SelectInput>
                </Labelled>
              </div>
              <Labelled label="Category">
                <SelectInput name="category" defaultValue="preparation_loss">
                  <Options values={WASTE_CATEGORIES} />
                </SelectInput>
              </Labelled>
              <Labelled label="Reason">
                <TextInput name="reason" required maxLength={500} />
              </Labelled>
              <Labelled label="Batch (optional)">
                <SelectInput name="batch_id" defaultValue="">
                  <option value="">None</option>
                  {batches.items.map((b) => (
                    <option key={b.id} value={b.id}>
                      {b.code} {b.meal_name}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
