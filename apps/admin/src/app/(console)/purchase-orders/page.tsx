import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Options } from "@/components/kitchen-ui";
import { businessDay, formatDay, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createPO } from "../supply-actions";

export const metadata: Metadata = { title: "Purchase orders" };

const STATUSES = ["draft", "pending_approval", "approved", "issued", "partially_received", "received", "closed", "cancelled"];

export default async function PurchaseOrdersPage({ searchParams }: { searchParams: Promise<{ status?: string; q?: string; run_id?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "receiving.read") && !can(admin, "purchase_orders.read")) notFound();
  const sp = await searchParams;
  const create = can(admin, "purchase_orders.create") && can(admin, "suppliers.read");
  const [list, suppliers, locations] = await Promise.all([
    api.supply.purchaseOrders(token, { status: sp.status, q: sp.q, run_id: sp.run_id }),
    create ? api.supply.suppliers(token, { status: "approved", page_size: 200 }) : Promise.resolve(null),
    create ? api.supply.locations(token) : Promise.resolve(null),
  ]);
  const prices = can(admin, "purchase_orders.read");
  const today = businessDay();
  return (
    <>
      <PageHeader
        title="Purchase orders"
        description="Prepared by a buyer, approved by someone else (and above the configured limit, by someone allowed to), then issued to the supplier by phone, WhatsApp, email or on paper. Mise sends nothing itself."
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <div>
          <FilterBar>
            <Labelled label="Number or supplier">
              <TextInput name="q" defaultValue={sp.q} maxLength={60} className="w-48" />
            </Labelled>
            <Labelled label="Status">
              <SelectInput name="status" defaultValue={sp.status ?? ""}>
                <option value="">Any</option>
                <Options values={STATUSES} />
              </SelectInput>
            </Labelled>
          </FilterBar>
          {list.items.length ? (
            <DataTable head={prices ? ["Order", "Supplier", "Expected", "Total", "Status"] : ["Order", "Supplier", "Expected", "Status"]} minWidth="36rem">
              {list.items.map((po) => (
                <tr key={po.id}>
                  <td className="p-3">
                    <Link href={`/purchase-orders/${po.id}`} className="font-semibold underline underline-offset-4">
                      {po.number}
                    </Link>
                    <span className="type-caption block text-muted">
                      {po.run_number ? `for ${po.run_number} · ` : ""}revision {po.revision}
                    </span>
                  </td>
                  <td className="p-3">{po.supplier_name}</td>
                  <td className="p-3">{po.expected_delivery ? formatDay(po.expected_delivery) : "—"}</td>
                  {prices ? <td className="p-3">{money(po.total_minor, po.currency)}</td> : null}
                  <td className="p-3">
                    <StatusTag status={po.status} />
                    {po.status !== "received" && po.status !== "closed" && po.status !== "cancelled" && po.expected_delivery && po.expected_delivery < today && ["approved", "issued", "partially_received"].includes(po.status) ? (
                      <StatusTag status="failed" label="Overdue" />
                    ) : null}
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>No purchase orders match.</EmptyState>
          )}
        </div>
        {create && suppliers && locations ? (
          <Panel title="New purchase order" note="Usually drafted from a procurement run. Only approved suppliers are listed.">
            <KForm action={createPO} submit="Start order" pending="Starting…" label="New purchase order">
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
              <Labelled label="Expected delivery">
                <TextInput type="date" name="expected_delivery" />
              </Labelled>
              <Labelled label="Deliver to">
                <SelectInput name="location_id" defaultValue="">
                  <option value="">Decide later</option>
                  {locations.items
                    .filter((l) => l.active && l.type !== "quarantine")
                    .map((l) => (
                      <option key={l.id} value={l.id}>
                        {l.name}
                      </option>
                    ))}
                </SelectInput>
              </Labelled>
              <input type="hidden" name="currency" value="TTD" />
            </KForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
