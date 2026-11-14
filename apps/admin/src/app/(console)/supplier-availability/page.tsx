import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Options } from "@/components/kitchen-ui";
import { AVAILABILITY, CONFIDENCE } from "@/components/supply-ui";
import { formatDate, formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { recordAvailability } from "../supply-actions";

export const metadata: Metadata = { title: "Produce availability" };

/**
 * What farmers and suppliers say they could supply, recorded by staff. It is
 * never stock and never a promise: an expected harvest is not inventory.
 */
export default async function AvailabilityPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "supplier_products.read")) notFound();
  const manage = can(admin, "supplier_products.manage");
  const [list, suppliers, ingredients, units] = await Promise.all([
    api.supply.availability(token, { current: true }),
    manage && can(admin, "suppliers.read") ? api.supply.suppliers(token, { page_size: 200 }) : Promise.resolve(null),
    manage ? api.kitchen.ingredients(token, { active: "true", page_size: 200 }) : Promise.resolve(null),
    manage ? api.kitchen.units(token) : Promise.resolve(null),
  ]);
  return (
    <>
      <PageHeader
        title="Produce availability"
        description="The latest word from each supplier on each ingredient, with how sure it is. Estimates are never counted as stock or as confirmed supply."
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <Panel title="Current">
          {list.items.length ? (
            <DataTable head={["Ingredient", "Supplier", "Status", "Estimate", "When", "Confidence"]} minWidth="48rem">
              {list.items.map((a) => (
                <tr key={a.id}>
                  <td className="p-3 font-semibold">{a.ingredient_name}</td>
                  <td className="p-3">
                    <Link href={`/suppliers/${a.supplier_id}`} className="underline underline-offset-4">
                      {a.supplier_name}
                    </Link>
                  </td>
                  <td className="p-3">
                    <StatusTag status={a.status === "unavailable" ? "failed" : a.status === "available_now" ? "active" : "pending"} label={humanize(a.status)} />
                  </td>
                  <td className="p-3">{a.estimated_quantity ? `${a.estimated_quantity} ${a.unit}` : "—"}</td>
                  <td className="p-3">
                    {a.available_from ? formatDate(a.available_from) : "—"}
                    {a.available_until ? ` – ${formatDate(a.available_until)}` : ""}
                  </td>
                  <td className="p-3">
                    {humanize(a.confidence)}
                    <span className="type-caption block text-muted">
                      {formatDateTime(a.confirmed_at)} · {a.recorded_by_name}
                      {a.source ? ` · ${a.source}` : ""}
                    </span>
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>Nothing recorded.</EmptyState>
          )}
        </Panel>
        {manage && suppliers && ingredients && units ? (
          <Panel title="Record availability" note="What the supplier said, as they said it.">
            <KForm action={recordAvailability} submit="Record" pending="Saving…" label="Record availability" reset>
              <Labelled label="Supplier">
                <SelectInput name="supplier_id" required defaultValue="">
                  <option value="" disabled>
                    Choose
                  </option>
                  {suppliers.items.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Ingredient">
                <SelectInput name="ingredient_id" required defaultValue="">
                  <option value="" disabled>
                    Choose
                  </option>
                  {ingredients.items.map((i) => (
                    <option key={i.id} value={i.id}>
                      {i.name}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Availability">
                <SelectInput name="status" defaultValue="available_now">
                  <Options values={AVAILABILITY} />
                </SelectInput>
              </Labelled>
              <div className="grid grid-cols-2 gap-3">
                <Labelled label="Estimated quantity">
                  <TextInput name="estimated_quantity" inputMode="decimal" />
                </Labelled>
                <Labelled label="Unit">
                  <SelectInput name="unit" defaultValue="">
                    <option value="">—</option>
                    {units.items.map((u) => (
                      <option key={u.code}>{u.code}</option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="From">
                  <TextInput type="date" name="available_from" />
                </Labelled>
                <Labelled label="Until">
                  <TextInput type="date" name="available_until" />
                </Labelled>
              </div>
              <Labelled label="How sure">
                <SelectInput name="confidence" defaultValue="supplier_estimate">
                  <Options values={CONFIDENCE} />
                </SelectInput>
              </Labelled>
              <Labelled label="Source" hint="For example, a WhatsApp message on 12 November.">
                <TextInput name="source" maxLength={300} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
