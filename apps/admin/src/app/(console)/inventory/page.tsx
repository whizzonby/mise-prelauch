import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader } from "@/components/admin-ui";
import { DataTable, Labelled, TextInput } from "@/components/commerce-ui";
import { Definitions } from "@/components/supply-ui";
import { quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Inventory" };

/**
 * Stock per ingredient, every figure apart and defined. Expected deliveries
 * are shown as incoming, never as stock.
 */
export default async function InventoryPage({ searchParams }: { searchParams: Promise<{ q?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "inventory.read")) notFound();
  const sp = await searchParams;
  const v = await api.supply.stock(token, sp.q);
  return (
    <>
      <PageHeader
        title="Inventory"
        description="Built from the stock ledger: every figure is the sum of recorded movements. Quarantined, rejected, expired and unreviewed stock is never allocated."
        actions={
          <div className="flex flex-wrap gap-4">
            <Link href="/inventory/lots" className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Lots
            </Link>
            <Link href="/inventory/movements" className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Ledger
            </Link>
          </div>
        }
      />
      <FilterBar>
        <Labelled label="Ingredient">
          <TextInput name="q" defaultValue={sp.q} maxLength={100} className="w-56" />
        </Labelled>
      </FilterBar>
      {v.items.length ? (
        <DataTable head={["Ingredient", "On hand", "Usable", "Reserved", "Available", "Quarantined", "Rejected", "Expired", "Needs review", "Incoming"]} minWidth="72rem">
          {v.items.map((x) => (
            <tr key={x.ingredient_id}>
              <td className="p-3">
                <Link href={`/inventory/lots?ingredient_id=${x.ingredient_id}`} className="font-semibold underline underline-offset-4">
                  {x.ingredient_name}
                </Link>
                <span className="type-caption block text-muted">
                  {x.lots} lot{x.lots === 1 ? "" : "s"} · {x.method.toUpperCase()}
                </span>
              </td>
              <td className="p-3">{quantity(x.on_hand, x.unit)}</td>
              <td className="p-3">{quantity(x.usable, x.unit)}</td>
              <td className="p-3">{quantity(x.reserved, x.unit)}</td>
              <td className="p-3 font-semibold">{quantity(x.available, x.unit)}</td>
              <td className="p-3">{Number(x.quarantined) ? quantity(x.quarantined, x.unit) : "—"}</td>
              <td className="p-3">{Number(x.rejected) ? quantity(x.rejected, x.unit) : "—"}</td>
              <td className="p-3">{Number(x.expired) ? <strong className="text-error">{quantity(x.expired, x.unit)}</strong> : "—"}</td>
              <td className="p-3">{Number(x.expiry_review) ? quantity(x.expiry_review, x.unit) : "—"}</td>
              <td className="p-3 text-muted">{Number(x.incoming) ? quantity(x.incoming, x.unit) : "—"}</td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No stock. Lots appear when deliveries are received.</EmptyState>
      )}
      <Definitions defs={v.definitions} keys={["on_hand", "usable", "reserved", "available", "quarantined", "rejected", "expired", "expiry_review", "incoming"]} />
    </>
  );
}
