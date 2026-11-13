import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Panel } from "@/components/commerce-ui";
import { BatchCard, Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Production" };

/** The kitchen's day at a glance: what is due, what is stuck, what is missing. */
export default async function ProductionOverviewPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.read")) notFound();
  const o = await api.kitchen.overview(token);
  return (
    <>
      <PageHeader
        title="Production"
        description={`Today is ${formatDay(o.date)} (Trinidad and Tobago time). Mise produces meal kits: ingredients are prepared, portioned, assembled, checked, packed and labelled; customers cook at home.`}
        actions={
          can(admin, "production.execute") ? (
            <Link href="/production/tasks" className="type-body-sm inline-flex min-h-11 items-center rounded-md bg-primary px-5 font-semibold text-primary-foreground">
              Open kitchen tasks
            </Link>
          ) : undefined
        }
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6">
        <Notice tone="warning" title="Setup the business still needs to provide" items={o.setup} />
        <ul className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7">
          {o.counts.map((c) => (
            <li key={c.key}>
              <Link href={c.link} className="block h-full border border-border bg-surface p-4 hover:border-foreground">
                <span className="type-caption block font-semibold uppercase tracking-[0.06em] text-muted">{c.label}</span>
                <span className={`mt-1 block font-display text-[2rem] leading-none ${c.value > 0 && (c.key === "holds" || c.key === "delayed" || c.key === "tasks_blocked") ? "text-error" : ""}`}>
                  {c.value}
                </span>
                <span className="type-caption mt-1 block text-muted">{c.definition}</span>
              </Link>
            </li>
          ))}
        </ul>
        {o.holds.length ? (
          <Panel title="On quality hold" note="Held batches cannot be released for fulfilment.">
            <ul className="grid gap-2">
              {o.holds.map((h) => (
                <li key={h.id} className="type-body-sm">
                  <Link href={`/production/batches/${h.batch_id}`} className="font-semibold underline underline-offset-4">
                    {h.batch_code} {h.meal_name}
                  </Link>{" "}
                  — {h.reason}
                </li>
              ))}
            </ul>
          </Panel>
        ) : null}
        <Panel title="Today's batches">
          {o.today.length ? (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {o.today.map((b) => (
                <BatchCard key={b.id} batch={b} />
              ))}
            </div>
          ) : (
            <EmptyState>No batches are scheduled for today.</EmptyState>
          )}
        </Panel>
        {o.overdue.length ? (
          <Panel title="Past their production date">
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {o.overdue.map((b) => (
                <BatchCard key={b.id} batch={b} />
              ))}
            </div>
          </Panel>
        ) : null}
        <Panel title="Delivery weeks" note="Demand is final once a week's meal selection closes. Plans are made from a snapshot of paid, confirmed orders.">
          <DataTable head={["Delivery week", "Selection closes", "Orders", "Kits", "Plan"]} minWidth="36rem">
            {o.weeks.map((w) => (
              <tr key={w.week}>
                <td className="p-3 font-semibold">Week of {formatDay(w.week)}</td>
                <td className="p-3">
                  {formatDateTime(w.cutoff_at)} {w.final ? <StatusTag status="finalized" label="Closed" /> : <StatusTag status="scheduled" label="Open" />}
                </td>
                <td className="p-3">{w.final ? w.orders : "—"}</td>
                <td className="p-3">{w.final ? w.kits : "—"}</td>
                <td className="p-3">
                  {w.plan_id ? (
                    <Link href={`/production/plans/${w.plan_id}`} className="underline underline-offset-4">
                      {w.plan_number} <StatusTag status={w.plan_status ?? ""} />
                    </Link>
                  ) : w.final && w.orders > 0 ? (
                    <Link href={`/production/plans?week=${w.week}`} className="underline underline-offset-4">
                      Plan this week
                    </Link>
                  ) : (
                    <span className="text-muted">—</span>
                  )}
                </td>
              </tr>
            ))}
          </DataTable>
        </Panel>
      </div>
    </>
  );
}
