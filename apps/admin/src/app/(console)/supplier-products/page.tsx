import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, SelectInput, TextInput } from "@/components/commerce-ui";
import { formatDate, money, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Supplier products" };

/**
 * Every supplier's products, mapped to the one ingredient catalogue. The
 * same ingredient can come from several suppliers, each with its own SKU,
 * pack, grade and price.
 */
export default async function SupplierProductsPage({ searchParams }: { searchParams: Promise<{ q?: string; mapping?: string; ingredient_id?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "supplier_products.read")) notFound();
  const sp = await searchParams;
  const list = await api.supply.products(token, { q: sp.q, mapping: sp.mapping, ingredient_id: sp.ingredient_id });
  return (
    <>
      <PageHeader
        title="Supplier products"
        description="A product is a purchasing option only when its supplier is approved and someone has confirmed it really is the catalogue ingredient (materially different products are never merged)."
      />
      <FilterBar>
        <Labelled label="Product, SKU, ingredient or supplier">
          <TextInput name="q" defaultValue={sp.q} maxLength={100} className="w-64" />
        </Labelled>
        <Labelled label="Mapping">
          <SelectInput name="mapping" defaultValue={sp.mapping ?? ""}>
            <option value="">Any</option>
            <option value="proposed">Waiting for review</option>
            <option value="confirmed">Confirmed</option>
            <option value="rejected">Rejected</option>
          </SelectInput>
        </Labelled>
      </FilterBar>
      {list.items.length ? (
        <DataTable head={["Ingredient", "Product", "Supplier", "Pack", "Price per pack", "Lead time", "Mapping"]} minWidth="56rem">
          {list.items.map((p) => (
            <tr key={p.id}>
              <td className="p-3 font-semibold">{p.ingredient_name ?? "Packaging"}</td>
              <td className="p-3">
                <Link href={`/supplier-products/${p.id}`} className="underline underline-offset-4">
                  {p.name}
                </Link>
                <span className="type-caption block text-muted">{[p.sku, p.grade].filter(Boolean).join(" · ") || "—"}</span>
              </td>
              <td className="p-3">
                <Link href={`/suppliers/${p.supplier_id}`} className="underline underline-offset-4">
                  {p.supplier_name}
                </Link>
                {!p.supplier_qualified ? <span className="type-caption block text-muted">Not approved</span> : null}
              </td>
              <td className="p-3">
                {p.pack_quantity} {p.pack_unit}
                {p.pack_base && p.base_unit ? <span className="type-caption block text-muted">= {quantity(p.pack_base, p.base_unit)}</span> : null}
              </td>
              <td className="p-3">
                {p.price ? (
                  <>
                    {money(p.price.amount_minor, p.price.currency)}
                    <span className="type-caption block text-muted">from {formatDate(p.price.valid_from)}</span>
                  </>
                ) : (
                  <span className="text-muted">No price in force</span>
                )}
              </td>
              <td className="p-3">{p.lead_time_days != null ? `${p.lead_time_days} days` : "—"}</td>
              <td className="p-3">
                <StatusTag status={p.mapping_status} />
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No products match. Add products from a supplier’s page.</EmptyState>
      )}
    </>
  );
}
