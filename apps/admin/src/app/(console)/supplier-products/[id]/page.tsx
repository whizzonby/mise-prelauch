import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Field, Options } from "@/components/kitchen-ui";
import { businessDay, formatDate, formatDateTime, humanize, money, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { addPrice, reviewMapping, updateProduct } from "../../supply-actions";

export const metadata: Metadata = { title: "Supplier product" };

export default async function SupplierProductPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "supplier_products.read")) notFound();
  const { id } = await params;
  const p = await api.supply.product(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const manage = can(admin, "supplier_products.manage");
  const today = businessDay();
  return (
    <>
      <PageHeader
        back={{ href: "/supplier-products", label: "All supplier products" }}
        title={p.name}
        description={
          <>
            <Link href={`/suppliers/${p.supplier_id}`} className="underline underline-offset-4">
              {p.supplier_name}
            </Link>{" "}
            · {p.ingredient_name ? <>maps to the ingredient “{p.ingredient_name}”</> : "packaging item"} · mapping <StatusTag status={p.mapping_status} />
            {!p.supplier_qualified ? " · supplier not approved" : ""}
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[minmax(0,1fr)_26rem]">
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          <Panel title="Pack and terms" note="Changing the ingredient or the pack sends the mapping back for review.">
            {manage ? (
              <KForm action={updateProduct} submit="Save product" pending="Saving…" label="Product details">
                <input type="hidden" name="id" value={p.id} />
                <Field>
                  <Labelled label="Name">
                    <TextInput name="name" defaultValue={p.name} required maxLength={160} />
                  </Labelled>
                  <Labelled label="SKU">
                    <TextInput name="sku" defaultValue={p.sku} maxLength={60} />
                  </Labelled>
                  <Labelled label="Grade">
                    <TextInput name="grade" defaultValue={p.grade} maxLength={120} />
                  </Labelled>
                  <Labelled label="Pack size">
                    <TextInput name="pack_quantity" inputMode="decimal" defaultValue={p.pack_quantity} />
                  </Labelled>
                  <Labelled label="Pack unit">
                    <TextInput name="pack_unit" defaultValue={p.pack_unit} />
                  </Labelled>
                  <Labelled label="Pack description">
                    <TextInput name="pack_description" defaultValue={p.pack_description} maxLength={120} />
                  </Labelled>
                  <Labelled label="Minimum order (packs)">
                    <TextInput name="min_order_packs" inputMode="decimal" defaultValue={p.min_order_packs} />
                  </Labelled>
                  <Labelled label="Lead time (days)">
                    <TextInput type="number" name="lead_time_days" min={0} max={365} defaultValue={p.lead_time_days ?? ""} />
                  </Labelled>
                  <Labelled label="Availability">
                    <SelectInput name="availability" defaultValue={p.availability}>
                      <Options values={["unknown", "available", "limited", "seasonal", "unavailable"]} />
                    </SelectInput>
                  </Labelled>
                </Field>
                <Labelled label="Specification">
                  <TextArea name="specification" rows={2} defaultValue={p.specification} maxLength={1000} />
                </Labelled>
                <Labelled label="Delivery constraints">
                  <TextInput name="delivery_constraints" defaultValue={p.delivery_constraints} maxLength={500} />
                </Labelled>
              </KForm>
            ) : (
              <dl className="type-body-sm grid gap-2">
                <div>
                  Pack: {p.pack_quantity} {p.pack_unit} {p.pack_description ? `(${p.pack_description})` : ""}
                </div>
                <div>Minimum order: {p.min_order_packs} packs · lead time {p.lead_time_days ?? "—"} days · {humanize(p.availability)}</div>
              </dl>
            )}
            {p.pack_base && p.base_unit ? (
              <p className="type-caption mt-3 text-muted">
                One pack is {quantity(p.pack_base, p.base_unit)} of {p.ingredient_name}
                {p.pack_approximate ? ", through an average conversion: weigh on receipt" : ""}.
              </p>
            ) : null}
          </Panel>
          <Panel title="Price history" note="Per pack. A new price never replaces an old one: orders and receipts keep the price they used.">
            <DataTable head={["Price per pack", "Valid", "Source", "Recorded"]} minWidth="34rem">
              {(p.prices ?? []).map((x) => (
                <tr key={x.id}>
                  <td className="p-3 font-semibold">
                    {money(x.amount_minor, x.currency)}
                    {p.price?.id === x.id ? <StatusTag status="active" label="In force" /> : null}
                  </td>
                  <td className="p-3">
                    {formatDate(x.valid_from)}
                    {x.valid_until ? ` – ${formatDate(x.valid_until)}` : " onward"}
                  </td>
                  <td className="p-3">
                    {humanize(x.origin)}
                    {x.note ? <span className="type-caption block text-muted">{x.note}</span> : null}
                  </td>
                  <td className="p-3">
                    {formatDateTime(x.created_at)}
                    <span className="type-caption block text-muted">{x.recorded_by_name}</span>
                  </td>
                </tr>
              ))}
            </DataTable>
          </Panel>
        </div>
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          {manage && p.kind === "ingredient" ? (
            <Panel
              title="Ingredient mapping"
              note={
                p.mapping_reviewer_name
                  ? `${humanize(p.mapping_status)} by ${p.mapping_reviewer_name}${p.mapping_reviewed_at ? `, ${formatDateTime(p.mapping_reviewed_at)}` : ""}${p.mapping_note ? `: ${p.mapping_note}` : ""}`
                  : "Confirm only when this product is the same ingredient the recipes use, not merely similar."
              }
            >
              <KForm action={reviewMapping} submit="Confirm mapping" pending="Saving…" label="Review mapping" secondary={{ name: "decision", value: "rejected", label: "Reject" }}>
                <input type="hidden" name="id" value={p.id} />
                <Labelled label="Note" hint="Required to reject.">
                  <TextInput name="note" maxLength={500} />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}
          {manage ? (
            <Panel title="Record a price" note="As the supplier gave it. A quotation recorded under Quotations adds its prices here automatically.">
              <KForm action={addPrice} submit="Record price" pending="Saving…" label="Record a price" reset>
                <input type="hidden" name="id" value={p.id} />
                <div className="grid grid-cols-2 gap-3">
                  <Labelled label="Price per pack ($)">
                    <TextInput name="amount" inputMode="decimal" required />
                  </Labelled>
                  <Labelled label="Currency">
                    <TextInput name="currency" defaultValue="TTD" maxLength={3} />
                  </Labelled>
                  <Labelled label="Valid from">
                    <TextInput type="date" name="valid_from" defaultValue={today} required />
                  </Labelled>
                  <Labelled label="Valid until">
                    <TextInput type="date" name="valid_until" />
                  </Labelled>
                </div>
                <Labelled label="Source">
                  <SelectInput name="origin" defaultValue="price_list">
                    <option value="price_list">Price list</option>
                    <option value="verbal">Told to us</option>
                    <option value="other">Other</option>
                  </SelectInput>
                </Labelled>
                <Labelled label="Note">
                  <TextInput name="note" maxLength={300} />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}
        </div>
      </div>
    </>
  );
}
