import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Definitions } from "@/components/supply-ui";
import { formatDay, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { issueStock, returnStock } from "../../../supply-actions";

export const metadata: Metadata = { title: "Batch ingredients" };

/**
 * A batch's ingredients from stock: issue (reservations first), return what
 * was not used, and reconcile against the plan. The kitchen's waste records
 * are shown inside consumption, never as a second stock movement.
 */
export default async function BatchStockPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "inventory.read")) notFound();
  const { id } = await params;
  const r = await api.supply.reconciliation(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const b = r.batch;
  const issue = can(admin, "inventory.issue") && (b.status === "released" || b.status === "in_progress");
  return (
    <>
      <PageHeader
        back={{ href: `/production/batches/${b.id}`, label: `Batch ${b.code}` }}
        title={`${b.meal_name}: ingredients`}
        description={
          <>
            {b.code} · <StatusTag status={b.status} /> · plan {b.plan_number} · production {formatDay(b.production_date)} · recipe {b.recipe_name} v{b.recipe_version}
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <Panel title="Planned and used" note="All in each ingredient's base unit.">
          <DataTable head={["Ingredient", "Planned", "Issued", "Returned", "Consumed", "Variance", "Kitchen waste (inside consumed)"]} minWidth="56rem">
            {r.lines.map((l) => (
              <tr key={l.ingredient_id}>
                <td className="p-3 font-semibold">
                  {l.ingredient_name}
                  {l.notes.length ? <span className="type-caption block font-normal text-muted">{l.notes.join(" ")}</span> : null}
                </td>
                <td className="p-3">{quantity(l.planned, l.unit)}</td>
                <td className="p-3">{quantity(l.issued, l.unit)}</td>
                <td className="p-3">{quantity(l.returned, l.unit)}</td>
                <td className="p-3 font-semibold">{quantity(l.consumed, l.unit)}</td>
                <td className="p-3">{l.variance == null ? "—" : `${Number(l.variance) > 0 ? "+" : ""}${l.variance} ${l.unit}`}</td>
                <td className="p-3">{quantity(l.recorded_waste, l.unit)}</td>
              </tr>
            ))}
          </DataTable>
          <Definitions defs={r.definitions} />
        </Panel>
        {issue ? (
          <div className="grid gap-6 [&>*]:min-w-0 lg:grid-cols-2">
            <Panel title="Issue from stock" note="The plan's reservations are used first, in the allocation order. All or nothing: if there is not enough usable stock, nothing is issued.">
              <KForm action={issueStock} submit="Issue" pending="Issuing…" label="Issue stock" size="lg">
                <input type="hidden" name="batch_id" value={b.id} />
                <Labelled label="Ingredient">
                  <SelectInput name="ingredient_id" required defaultValue="">
                    <option value="" disabled>
                      Choose
                    </option>
                    {r.lines.map((l) => (
                      <option key={l.ingredient_id} value={l.ingredient_id}>
                        {l.ingredient_name} ({l.unit})
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Quantity (base unit)">
                  <TextInput name="quantity" inputMode="decimal" required className="text-[1.25rem]" />
                </Labelled>
              </KForm>
            </Panel>
            {r.lots.length ? (
              <Panel title="Return unused stock" note="Up to what this batch was issued from the lot.">
                <KForm action={returnStock} submit="Return to the store" pending="Saving…" label="Return stock">
                  <input type="hidden" name="batch_id" value={b.id} />
                  <Labelled label="Lot">
                    <SelectInput name="lot_id" required defaultValue="">
                      <option value="" disabled>
                        Choose
                      </option>
                      {r.lots.map((l) => (
                        <option key={l.lot_id} value={l.lot_id}>
                          {l.lot_code} · {l.ingredient_name} (still out: {l.consumed} {l.unit})
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <div className="grid grid-cols-2 gap-3">
                    <Labelled label="Quantity">
                      <TextInput name="quantity" inputMode="decimal" required />
                    </Labelled>
                    <Labelled label="Why (optional)">
                      <TextInput name="reason" maxLength={500} />
                    </Labelled>
                  </div>
                </KForm>
              </Panel>
            ) : null}
          </div>
        ) : null}
        <Panel title="Lots used" note="Batch-level traceability: which lots went into this batch.">
          {r.lots.length ? (
            <DataTable head={["Lot", "Ingredient", "Issued", "Returned", "Consumed"]} minWidth="36rem">
              {r.lots.map((l) => (
                <tr key={l.lot_id}>
                  <td className="p-3">
                    <Link href={`/inventory/lots/${l.lot_id}`} className="font-semibold underline underline-offset-4">
                      {l.lot_code}
                    </Link>
                  </td>
                  <td className="p-3">{l.ingredient_name}</td>
                  <td className="p-3">{quantity(l.issued, l.unit)}</td>
                  <td className="p-3">{quantity(l.returned, l.unit)}</td>
                  <td className="p-3">{quantity(l.consumed, l.unit)}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <p className="type-body-sm text-muted">Nothing issued from stock yet.</p>
          )}
        </Panel>
      </div>
    </>
  );
}
