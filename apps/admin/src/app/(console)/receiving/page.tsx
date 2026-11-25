import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Panel } from "@/components/commerce-ui";
import { businessDay, formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Goods receiving" };

/**
 * Deliveries expected (approved and issued orders) and deliveries received.
 * Receiving is laid out for a phone or tablet at the door.
 */
export default async function ReceivingPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "receiving.read")) notFound();
  const [expected, receipts] = await Promise.all([api.supply.purchaseOrders(token, { receivable: true }), api.supply.receipts(token)]);
  const manage = can(admin, "receiving.manage") && can(admin, "receiving.inspect");
  const today = businessDay();
  return (
    <>
      <PageHeader
        title="Goods receiving"
        description="Check each delivery against its order and the receiving checklist. Accepted stock becomes a lot you can use; quarantined and rejected stock is kept apart and never used."
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6">
        <Panel title="Expected deliveries">
          {expected.items.length ? (
            <ul className="grid gap-3">
              {expected.items.map((po) => (
                <li key={po.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
                  <span className="type-body">
                    <strong>{po.supplier_name}</strong>
                    <span className="type-body-sm block text-muted">
                      {po.number} · {po.expected_delivery ? `expected ${formatDay(po.expected_delivery)}` : "no date"} <StatusTag status={po.status} />
                      {po.expected_delivery && po.expected_delivery < today ? <StatusTag status="failed" label="Overdue" /> : null}
                    </span>
                  </span>
                  {manage ? (
                    <Link
                      href={`/receiving/new?po=${po.id}`}
                      className="inline-flex min-h-12 items-center rounded-md bg-primary px-5 font-semibold text-primary-foreground hover:bg-primary/90"
                    >
                      Receive
                    </Link>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState>No deliveries expected.</EmptyState>
          )}
        </Panel>
        <Panel title="Received">
          {receipts.items.length ? (
            <DataTable head={["Receipt", "Order", "Supplier", "Received"]} minWidth="34rem">
              {receipts.items.map((g) => (
                <tr key={g.id}>
                  <td className="p-3">
                    <Link href={`/receiving/${g.id}`} className="font-semibold underline underline-offset-4">
                      {g.number}
                    </Link>
                  </td>
                  <td className="p-3">{g.po_number}</td>
                  <td className="p-3">{g.supplier_name}</td>
                  <td className="p-3">
                    {formatDateTime(g.received_at)}
                    <span className="type-caption block text-muted">{g.received_by_name}</span>
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>Nothing received yet.</EmptyState>
          )}
        </Panel>
      </div>
    </>
  );
}
