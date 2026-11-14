import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader } from "@/components/admin-ui";
import { DataTable, Labelled, SelectInput, TextInput } from "@/components/commerce-ui";
import { Options } from "@/components/kitchen-ui";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Stock movements" };

const TYPES = ["receipt", "reserve", "release", "issue", "return", "transfer", "adjustment", "waste", "supplier_return", "status"];

/** The append-only stock ledger: every change to every lot. */
export default async function MovementsPage({ searchParams }: { searchParams: Promise<{ type?: string; from?: string; to?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "inventory.read")) notFound();
  const sp = await searchParams;
  const list = await api.supply.movements(token, { type: sp.type, from: sp.from, to: sp.to });
  return (
    <>
      <PageHeader
        back={{ href: "/inventory", label: "Inventory" }}
        title="Stock movements"
        description="Every change to every lot, with who made it and why. Nothing here can be edited or deleted; a correction is a new movement."
      />
      <FilterBar>
        <Labelled label="Movement">
          <SelectInput name="type" defaultValue={sp.type ?? ""}>
            <option value="">Any</option>
            <Options values={TYPES} />
          </SelectInput>
        </Labelled>
        <Labelled label="From">
          <TextInput type="date" name="from" defaultValue={sp.from} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={sp.to} />
        </Labelled>
      </FilterBar>
      {list.items.length ? (
        <DataTable head={["When", "Lot", "Movement", "On hand", "Reserved", "Detail"]} minWidth="56rem">
          {list.items.map((m) => (
            <tr key={m.id}>
              <td className="p-3">
                {formatDateTime(m.created_at)}
                <span className="type-caption block text-muted">{m.actor_name}</span>
              </td>
              <td className="p-3">
                <Link href={`/inventory/lots/${m.lot_id}`} className="font-semibold underline underline-offset-4">
                  {m.lot_code}
                </Link>
                <span className="type-caption block text-muted">{m.ingredient_name}</span>
              </td>
              <td className="p-3">{humanize(m.type)}</td>
              <td className="p-3">
                {Number(m.on_hand_delta) ? `${Number(m.on_hand_delta) > 0 ? "+" : ""}${m.on_hand_delta} ${m.unit}` : "—"}
                <span className="type-caption block text-muted">after: {m.on_hand_after}</span>
              </td>
              <td className="p-3">{Number(m.reserved_delta) ? `${Number(m.reserved_delta) > 0 ? "+" : ""}${m.reserved_delta}` : "—"}</td>
              <td className="type-caption p-3">
                {m.reason}
                {m.batch_code ? ` · batch ${m.batch_code}` : ""}
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No movements match.</EmptyState>
      )}
    </>
  );
}
