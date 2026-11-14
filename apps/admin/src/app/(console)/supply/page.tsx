import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { Panel } from "@/components/commerce-ui";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Supply chain" };

/** What needs someone in purchasing and stores today, and what is not set up yet. */
export default async function SupplyOverviewPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "procurement.read") && !can(admin, "inventory.read")) notFound();
  const proc = can(admin, "procurement.read");
  const inv = can(admin, "inventory.read");
  const [runs, pending, deliveries, settings, orders, stock, locations, checklist] = await Promise.all([
    proc ? api.supply.runs(token, "open") : Promise.resolve(null),
    proc ? api.supply.pendingPlans(token) : Promise.resolve(null),
    proc ? api.supply.deliveries(token) : Promise.resolve(null),
    proc ? api.supply.procurementSettings(token) : Promise.resolve(null),
    can(admin, "receiving.read") ? api.supply.purchaseOrders(token, {}) : Promise.resolve(null),
    inv ? api.supply.stock(token) : Promise.resolve(null),
    api.supply.locations(token).catch(() => null),
    can(admin, "receiving.read") ? api.supply.checklist(token) : Promise.resolve(null),
  ]);
  const today = new Date().toISOString().slice(0, 10);
  const open = (orders?.items ?? []).filter((p) => ["approved", "issued", "partially_received"].includes(p.status));
  const overdue = open.filter((p) => p.expected_delivery && p.expected_delivery < today);
  const waiting = (orders?.items ?? []).filter((p) => p.status === "pending_approval");
  const review = (stock?.items ?? []).filter((x) => Number(x.expiry_review) > 0 || Number(x.expired) > 0);
  const setup = [
    ...(settings?.notes ?? []),
    ...(locations && locations.items.filter((l) => l.active).length === 0 ? ["No storage locations are configured: nothing can be received."] : []),
    ...(checklist && checklist.items.filter((c) => c.active).length === 0 ? ["No receiving checklist is configured: deliveries can be quarantined or rejected but not accepted."] : []),
    "No approved safety stock is configured: purchasing buffers are zero.",
  ];
  return (
    <>
      <PageHeader title="Supply chain" description="Purchasing, receiving and stores at a glance. Every list links to where the work is done." />
      <div className="grid gap-6 [&>*]:min-w-0">
        <Notice tone="warning" title="Setup the business still needs to decide" items={setup} />
        <div className="grid gap-6 [&>*]:min-w-0 lg:grid-cols-2">
          {proc ? (
            <Panel title="Purchasing decisions" note="Runs waiting for decisions or approval.">
              {pending?.items.length ? (
                <p className="type-body-sm mb-3 text-error">
                  {pending.items.length} released plan(s) have no run yet.{" "}
                  <Link href="/procurement" className="underline underline-offset-4">
                    Create them
                  </Link>
                </p>
              ) : null}
              {runs?.items.length ? (
                <ul className="type-body-sm grid gap-2">
                  {runs.items.map((r) => (
                    <li key={r.id}>
                      <Link href={`/procurement/${r.id}`} className="font-semibold underline underline-offset-4">
                        {r.number}
                      </Link>{" "}
                      · plan {r.plan_number} · needed by {formatDay(r.required_by)} · {r.shortages} short · {r.pending} undecided
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="type-body-sm text-muted">No open runs.</p>
              )}
            </Panel>
          ) : null}
          {orders ? (
            <Panel title="Orders" note="Waiting for approval, and deliveries overdue.">
              <ul className="type-body-sm grid gap-2">
                {waiting.map((p) => (
                  <li key={p.id}>
                    <Link href={`/purchase-orders/${p.id}`} className="font-semibold underline underline-offset-4">
                      {p.number}
                    </Link>{" "}
                    · {p.supplier_name} · <StatusTag status={p.status} />
                  </li>
                ))}
                {overdue.map((p) => (
                  <li key={p.id}>
                    <Link href={`/purchase-orders/${p.id}`} className="font-semibold underline underline-offset-4">
                      {p.number}
                    </Link>{" "}
                    · {p.supplier_name} · expected {formatDay(p.expected_delivery!)} <StatusTag status="failed" label="Overdue" />
                  </li>
                ))}
                {waiting.length + overdue.length === 0 ? <li className="text-muted">Nothing waiting. {open.length} order(s) open.</li> : null}
              </ul>
            </Panel>
          ) : null}
          {stock ? (
            <Panel title="Stock needing attention" note="Expired stock, and lots with no expiry recorded (not used until reviewed).">
              {review.length ? (
                <ul className="type-body-sm grid gap-2">
                  {review.map((x) => (
                    <li key={x.ingredient_id}>
                      <Link href={`/inventory/lots?ingredient_id=${x.ingredient_id}`} className="font-semibold underline underline-offset-4">
                        {x.ingredient_name}
                      </Link>
                      {Number(x.expired) ? ` · ${x.expired} ${x.unit} expired` : ""}
                      {Number(x.expiry_review) ? ` · ${x.expiry_review} ${x.unit} awaiting an expiry review` : ""}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="type-body-sm text-muted">Nothing expired or waiting for review.</p>
              )}
            </Panel>
          ) : null}
          {deliveries ? (
            <Panel title="Production events" note="Released plans reach purchasing through the event queue.">
              <p className="type-body-sm">
                Last event handed over: #{deliveries.last_event_id}, {formatDateTime(deliveries.updated_at)} · {deliveries.waiting} waiting
              </p>
              {deliveries.failed.length ? (
                <Notice tone="error" title="Failed deliveries (retried, then stopped)" items={deliveries.failed.map((f) => `Job ${f.job_id} after ${f.attempts} attempts: ${f.error}`)} />
              ) : (
                <p className="type-caption mt-2 text-muted">No failed deliveries.</p>
              )}
            </Panel>
          ) : null}
        </div>
      </div>
    </>
  );
}
