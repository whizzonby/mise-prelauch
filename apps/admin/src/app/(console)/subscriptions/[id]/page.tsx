import { ApiError } from "@mise/api-client";
import { Heading, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { DataTable, Panel } from "@/components/commerce-ui";
import { commerceTone, formatDateTime, formatDay, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Subscription" };

export default async function SubscriptionPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "subscriptions.read")) notFound();
  const { id } = await params;
  const s = await api.admin.subscription(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  return (
    <>
      <Link href="/subscriptions" className="type-body-sm underline underline-offset-4">
        All subscriptions
      </Link>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Heading as="h1" size="h2">
          {s.customer_name || "Customer"}
        </Heading>
        <Tag tone={commerceTone(s.status)}>{s.status_label}</Tag>
      </div>
      <p className="type-body-sm mt-2 text-muted">
        {s.customer_email} · {s.plan.name}, {s.plan.meals_per_week} meals, {s.price_formatted} a week · created {formatDateTime(s.created_at)}
      </p>
      {s.pause ? <p className="type-body-sm mt-2">Paused {s.pause.label}.</p> : null}
      {s.cancel ? <p className="type-body-sm mt-2">{s.cancel.label}.</p> : null}

      <Panel title="Weeks" className="mt-8">
        <DataTable head={["Week", "State", "Plan", "Billing", "Meals", "Order"]}>
          {s.all_cycles.map((c) => (
            <tr key={c.id}>
              <td className="p-3">{c.week_label}</td>
              <td className="p-3">
                <Tag tone={commerceTone(c.state)}>{c.state_label}</Tag>
              </td>
              <td className="p-3">
                {c.plan.name} ({c.plan.meals_per_week})
              </td>
              <td className="p-3">
                {c.billing ? (
                  <>
                    {humanize(c.billing.status)} · {money(c.billing.total_minor, c.billing.currency)}
                    {c.billing.invoice_number ? <span className="type-caption block text-muted">{c.billing.invoice_number}</span> : null}
                  </>
                ) : (
                  <span className="text-muted">Not billed</span>
                )}
              </td>
              <td className="p-3">{c.selection_status ? `${humanize(c.selection_status)} (${c.selected_count})` : "None"}</td>
              <td className="p-3">
                {c.order_id && can(admin, "orders.read") ? (
                  <Link href={`/orders/${c.order_id}`} className="underline underline-offset-4">
                    Order
                  </Link>
                ) : null}
              </td>
            </tr>
          ))}
        </DataTable>
      </Panel>

      <Panel title="History" className="mt-8">
        <ol className="type-body-sm grid gap-2">
          {s.history.map((h, i) => (
            <li key={i} className="grid gap-1 border-b border-border pb-2 sm:grid-cols-[12rem_1fr]">
              <span className="text-muted">{formatDateTime(h.created_at)}</span>
              <span>
                <strong>{humanize(h.type)}</strong>
                {h.from_status && h.to_status ? ` · ${humanize(h.from_status)} → ${humanize(h.to_status)}` : ""}
                {h.effective_week ? ` · from week of ${formatDay(h.effective_week)}` : ""}
              </span>
            </li>
          ))}
        </ol>
      </Panel>
    </>
  );
}
