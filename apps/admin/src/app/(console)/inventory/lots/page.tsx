import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, SelectInput, TextInput } from "@/components/commerce-ui";
import { formatDate, formatDay, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Ingredient lots" };

export default async function LotsPage({ searchParams }: { searchParams: Promise<{ q?: string; status?: string; ingredient_id?: string; held?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "inventory.read")) notFound();
  const sp = await searchParams;
  const held = sp.held !== "false";
  const lots = await api.supply.lots(token, { q: sp.q, status: sp.status, ingredient_id: sp.ingredient_id, held });
  return (
    <>
      <PageHeader
        back={{ href: "/inventory", label: "Inventory" }}
        title="Ingredient lots"
        description="One lot per accepted, quarantined or rejected part of a delivery. Lot codes are Mise's own; the supplier's lot reference is kept beside them."
      />
      <FilterBar>
        <Labelled label="Lot, ingredient or supplier reference">
          <TextInput name="q" defaultValue={sp.q} maxLength={100} className="w-56" />
        </Labelled>
        <Labelled label="Status">
          <SelectInput name="status" defaultValue={sp.status ?? ""}>
            <option value="">Any</option>
            <option value="available">Available</option>
            <option value="quarantined">Quarantined</option>
            <option value="on_hold">On hold</option>
            <option value="rejected">Rejected</option>
          </SelectInput>
        </Labelled>
        <Labelled label="Show">
          <SelectInput name="held" defaultValue={held ? "true" : "false"}>
            <option value="true">With stock</option>
            <option value="false">All, including empty</option>
          </SelectInput>
        </Labelled>
      </FilterBar>
      {lots.items.length ? (
        <DataTable head={["Lot", "Ingredient", "Supplier", "On hand", "Reserved", "Expiry", "Where", "Status"]} minWidth="60rem">
          {lots.items.map((l) => (
            <tr key={l.id}>
              <td className="p-3">
                <Link href={`/inventory/lots/${l.id}`} className="font-semibold underline underline-offset-4">
                  {l.code}
                </Link>
                <span className="type-caption block text-muted">
                  received {formatDay(l.received_at.slice(0, 10))}
                  {l.supplier_lot_ref ? ` · ${l.supplier_lot_ref}` : ""}
                </span>
              </td>
              <td className="p-3">{l.ingredient_name}</td>
              <td className="p-3">{l.supplier_name}</td>
              <td className="p-3">{quantity(l.on_hand, l.unit)}</td>
              <td className="p-3">{Number(l.reserved) ? quantity(l.reserved, l.unit) : "—"}</td>
              <td className="p-3">
                {l.expiry_date ? formatDate(l.expiry_date) : l.expiry_status === "none_applies" ? "None applies" : <StatusTag status="not_recorded" label="Not recorded" />}
                {l.expired ? <StatusTag status="expired" label="Expired" /> : null}
              </td>
              <td className="p-3">{l.location_name}</td>
              <td className="p-3">
                <StatusTag status={l.status} />
                {!l.usable && l.status === "available" ? <span className="type-caption block text-muted">Not usable</span> : null}
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No lots match.</EmptyState>
      )}
    </>
  );
}
