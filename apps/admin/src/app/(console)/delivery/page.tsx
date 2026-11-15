import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { DataTable, Panel } from "@/components/commerce-ui";
import { CountTile, DeliveryStatus } from "@/components/delivery-ui";
import { Notice } from "@/components/kitchen-ui";
import { formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Delivery overview" };

/** What needs someone in fulfillment and dispatch today, from real records only. */
export default async function DeliveryOverviewPage({ searchParams }: { searchParams: Promise<{ date?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_reports.read")) notFound();
  const { date } = await searchParams;
  const o = await api.delivery.overview(token, date);
  const [due, exceptions] = await Promise.all([
    can(admin, "delivery.read") ? api.delivery.deliveries(token, { date: o.date, status: "all" }) : Promise.resolve(null),
    can(admin, "delivery_exceptions.read") ? api.delivery.exceptions(token, {}) : Promise.resolve(null),
  ]);
  return (
    <>
      <PageHeader
        title="Delivery & fulfillment"
        description={`Today is ${formatDay(o.date)} (Trinidad and Tobago). Counts come from recorded deliveries; nothing here is estimated or live-tracked.`}
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <Notice tone="warning" title="Not configured yet: these block live delivery" items={o.blockers} />
        {o.consumer_failed_jobs > 0 ? (
          <Notice tone="error" title="Production readiness" items={[`${o.consumer_failed_jobs} readiness event(s) failed to process. Open the fulfillment queue to retry.`]} />
        ) : null}
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-6">
          <CountTile label="Due today" value={o.due_today} href={`/delivery/planning?date=${o.date}`} />
          <CountTile label="To verify" value={o.awaiting_verification} href="/fulfillment" tone="warn" />
          <CountTile label="Verified, not released" value={o.ready_for_dispatch} href="/fulfillment?status=verified" tone="warn" />
          <CountTile label="No date yet" value={o.unscheduled} href="/delivery/planning" tone="warn" />
          <CountTile label="Scheduled, no run" value={o.unassigned} href="/delivery/planning" tone="warn" />
          <CountTile label="Out now" value={o.in_transit} href="/delivery/runs" />
          <CountTile label="Delivered today" value={o.delivered_today} />
          <CountTile label="Failed, open" value={o.failed_open} href="/delivery/exceptions" tone="bad" />
          <CountTile label="Delayed (date passed)" value={o.delayed} tone="bad" />
          <CountTile label="Delivered, no proof" value={o.missing_proof} tone="bad" />
          <CountTile label="Open exceptions" value={o.open_exceptions} href="/delivery/exceptions" tone="bad" />
          <CountTile label="Returns to review" value={o.returns_to_review} href="/delivery/returns" tone="warn" />
        </div>
        <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
          {due ? (
            <Panel title={`Deliveries on ${formatDay(o.date)}`}>
              {due.items.length ? (
                <DataTable head={["Delivery", "Order", "Region", "Run", "Status"]} minWidth="32rem">
                  {due.items.map((d) => (
                    <tr key={d.id}>
                      <td className="p-3">
                        <Link href={`/delivery/deliveries/${d.id}`} className="font-semibold underline underline-offset-4">
                          {d.number}
                        </Link>
                      </td>
                      <td className="p-3">{d.order_number}</td>
                      <td className="p-3">{d.address.region_label}</td>
                      <td className="p-3">{d.run_number ?? "—"}</td>
                      <td className="p-3">
                        <DeliveryStatus status={d.status} />
                      </td>
                    </tr>
                  ))}
                </DataTable>
              ) : (
                <p className="type-body-sm text-muted">No deliveries are scheduled for this date.</p>
              )}
            </Panel>
          ) : null}
          {exceptions ? (
            <Panel title="Exceptions needing attention" note="Most severe first.">
              {exceptions.items.length ? (
                <ul className="type-body-sm grid gap-2">
                  {exceptions.items.slice(0, 8).map((e) => (
                    <li key={e.id}>
                      <Link href={`/delivery/exceptions/${e.id}`} className="font-semibold underline underline-offset-4">
                        {e.number}
                      </Link>{" "}
                      · {e.severity} · {e.summary}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="type-body-sm text-muted">No open exceptions.</p>
              )}
            </Panel>
          ) : null}
        </div>
      </div>
    </>
  );
}
