import { ApiError } from "@mise/api-client";
import { Heading, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { DataTable, Panel } from "@/components/commerce-ui";
import { commerceTone, formatDateTime, formatDay, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Order" };

export default async function OrderPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "orders.read")) notFound();
  const { id } = await params;
  const o = await api.admin.order(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const a = o.address;
  return (
    <>
      <Link href={`/orders?week=${o.week}`} className="type-body-sm underline underline-offset-4">
        Orders for the week of {formatDay(o.week)}
      </Link>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Heading as="h1" size="h2">
          {o.number}
        </Heading>
        <Tag tone={commerceTone(o.status)}>{humanize(o.status)}</Tag>
        {o.delivery_outcome?.state === "disputed" ? <Tag tone="error">Delivery outcome disputed</Tag> : null}
      </div>
      {o.delivery_outcome ? (
        <p className="type-body-sm mt-3 max-w-[56rem] border-l-4 border-accent pl-3">
          {o.delivery_outcome.state === "disputed" ? "Delivery corrected the delivered outcome: " : "Delivered outcome confirmed again: "}
          {o.delivery_outcome.reason} The order status and payment history are kept as they were; this note is the commerce record of the correction.
        </p>
      ) : null}
      <p className="type-body-sm mt-2 text-muted">
        {o.plan_name} · {o.meals_count} meals · {money(o.total_minor, o.currency)} ·{" "}
        {can(admin, "subscriptions.read") ? (
          <Link href={`/subscriptions/${o.subscription_id}`} className="underline underline-offset-4">
            subscription
          </Link>
        ) : null}
      </p>
      <div className="mt-8 grid gap-8 xl:grid-cols-[1fr_22rem]">
        <Panel title="Meals (as ordered)">
          <DataTable head={["Meal", "Version", "Qty", "Declared allergens"]} minWidth="30rem">
            {o.items.map((it) => (
              <tr key={it.meal_id}>
                <td className="p-3">{it.name}</td>
                <td className="p-3">v{it.meal_version}</td>
                <td className="p-3 font-semibold">{it.quantity}</td>
                <td className="p-3">{it.allergen_status === "declared" ? it.allergens.join(", ") || "None declared" : "Not confirmed"}</td>
              </tr>
            ))}
          </DataTable>
        </Panel>
        <div className="grid content-start gap-8">
          <Panel title="Deliver to">
            <address className="type-body-sm not-italic">
              {[a.recipient_name, a.phone, a.line1, a.line2, a.area, a.city, a.region_label].filter(Boolean).join(", ")}
            </address>
            {a.instructions ? <p className="type-caption mt-2 text-muted">Instructions: {a.instructions}</p> : null}
          </Panel>
          <Panel title="Status history">
            <ol className="type-body-sm grid gap-2">
              {(o.history ?? []).map((h, i) => (
                <li key={i}>
                  {formatDateTime(h.created_at)}: {humanize(h.to)}
                  {h.reason ? <span className="type-caption block text-muted">{h.reason}</span> : null}
                </li>
              ))}
            </ol>
          </Panel>
        </div>
      </div>
    </>
  );
}
