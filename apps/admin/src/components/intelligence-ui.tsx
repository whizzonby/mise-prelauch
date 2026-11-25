import type { AnalyticsDashboard, AnalyticsMetricValue, Permission } from "@mise/api-client";
import Link from "next/link";

import { DataTable, Empty, Panel } from "@/components/commerce-ui";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay, money, number } from "@/lib/format";

/** Dashboards and the permission each needs (the API checks it again). */
export const DASHBOARDS: { kind: string; label: string; perm: Permission }[] = [
  { kind: "overview", label: "Overview", perm: "analytics_executive.read" },
  { kind: "revenue", label: "Revenue", perm: "analytics_revenue.read" },
  { kind: "customers", label: "Customers", perm: "analytics_customers.read" },
  { kind: "meals", label: "Meals", perm: "analytics_production.read" },
  { kind: "production", label: "Production", perm: "analytics_production.read" },
  { kind: "procurement", label: "Procurement", perm: "analytics_procurement.read" },
  { kind: "inventory", label: "Inventory", perm: "analytics_inventory.read" },
  { kind: "delivery", label: "Delivery", perm: "analytics_delivery.read" },
  { kind: "support", label: "Support chat", perm: "support_chat.analytics" },
];

/** A figure in its unit. Null means it cannot be calculated: never shown as zero. */
export function formatMetric(m: Pick<AnalyticsMetricValue, "kind" | "currency">, v: number | null | undefined): string {
  if (v === null || v === undefined) return "Not available";
  switch (m.kind) {
    case "money":
      // The catalogue's currency is a rule in words; only an ISO code is used to format.
      return money(Math.round(v), /^[A-Z]{3}$/.test(m.currency) ? m.currency : "TTD");
    case "ratio":
      return `${(v * 100).toLocaleString("en-GB", { maximumFractionDigits: 1 })}%`;
    case "hours":
      return `${v.toLocaleString("en-GB", { maximumFractionDigits: 1 })} h`;
    case "days":
      return `${v.toLocaleString("en-GB", { maximumFractionDigits: 1 })} days`;
    case "number":
      return v.toLocaleString("en-GB", { maximumFractionDigits: 2 });
    default:
      return number.format(v);
  }
}

function Change({ m }: { m: AnalyticsMetricValue }) {
  if (m.previous === null) return <span className="type-caption text-muted">No comparison</span>;
  if (m.change === null) return <span className="type-caption text-muted">Previous: {formatMetric(m, m.previous)}</span>;
  const up = m.change > 0;
  const pct = `${up ? "+" : ""}${(m.change * 100).toLocaleString("en-GB", { maximumFractionDigits: 1 })}%`;
  // Direction only: whether up is good depends on the metric, so no colour judgement.
  return (
    <span className="type-caption text-muted">
      <span aria-hidden="true">{m.change === 0 ? "→" : up ? "↑" : "↓"}</span> {pct} vs {formatMetric(m, m.previous)}
    </span>
  );
}

export function MetricCard({ m }: { m: AnalyticsMetricValue }) {
  return (
    <div className="flex flex-col border border-border bg-surface p-4">
      <p className="type-caption font-semibold uppercase tracking-[0.06em] text-muted">{m.name}</p>
      <p className={`type-figures mt-1 font-display leading-tight ${m.value === null ? "text-[1.1rem] text-muted" : "text-[1.75rem]"}`}>{formatMetric(m, m.value)}</p>
      <Change m={m} />
      <details className="mt-2">
        <summary className="type-caption cursor-pointer text-muted underline decoration-dotted underline-offset-2">Definition</summary>
        <p className="type-caption mt-1">{m.definition}</p>
        {m.limitations ? <p className="type-caption mt-1 text-muted">Limits: {m.limitations}</p> : null}
        <p className="type-caption mt-1 text-muted">
          <Link className="underline" href={`/intelligence/metrics#${encodeURIComponent(m.id)}`}>
            {m.id}
          </Link>
        </p>
      </details>
    </div>
  );
}

/** The one renderer for every dashboard: notices, sections of figures, then tables. */
export function DashboardView({ d }: { d: AnalyticsDashboard }) {
  return (
    // minmax(0, 1fr) tracks: a wide table scrolls inside its frame instead of widening the page.
    <div className="grid grid-cols-[minmax(0,1fr)] gap-6">
      <p className="type-body-sm text-muted">
        {formatDay(d.from)} to {formatDay(d.to)}, compared with {formatDay(d.previous_from)} to {formatDay(d.previous_to)}. {d.time_zone}.{" "}
        {d.last_refresh ? `Snapshots last refreshed ${formatDateTime(d.last_refresh)}; figures here are calculated live.` : "Figures are calculated live."}
      </p>
      {d.notices.length ? <Notice tone="warning" title="Read these figures with care" items={d.notices} /> : null}
      {d.sections.map((s) => (
        <section key={s.title} aria-labelledby={`sec-${slug(s.title)}`} className="grid grid-cols-[minmax(0,1fr)] gap-3">
          <h2 id={`sec-${slug(s.title)}`} className="type-h3">
            {s.title}
          </h2>
          {s.note ? <p className="type-body-sm text-muted">{s.note}</p> : null}
          {s.metrics.length ? (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
              {s.metrics.map((m) => (
                <MetricCard key={m.id} m={m} />
              ))}
            </div>
          ) : null}
          {s.tables.map((t) => (
            <Panel key={t.title} title={t.title} note={t.note || undefined}>
              {t.rows.length ? (
                <DataTable head={t.columns} minWidth={t.columns.length > 4 ? "40rem" : "24rem"}>
                  {t.rows.map((r, i) => (
                    <tr key={i}>
                      {r.map((c, j) => (
                        <td key={j} className={`p-3 ${j === 0 ? "font-semibold" : "type-figures"}`}>
                          {c}
                        </td>
                      ))}
                    </tr>
                  ))}
                </DataTable>
              ) : (
                <Empty>No records in this range.</Empty>
              )}
              {t.link ? (
                <p className="type-caption mt-2">
                  <Link className="underline" href={t.link}>
                    Open the records
                  </Link>
                </p>
              ) : null}
            </Panel>
          ))}
        </section>
      ))}
    </div>
  );
}

/** Links between pages of one area (each tab is its own path). */
export function PathTabs({ tabs, current }: { tabs: { href: string; label: string }[]; current: string }) {
  return (
    <nav aria-label="Sections" className="-mx-1 mb-5 overflow-x-auto px-1">
      <ul className="flex gap-1 border-b border-border">
        {tabs.map((t) => (
          <li key={t.href} className="shrink-0">
            <Link
              href={t.href}
              aria-current={t.href === current ? "page" : undefined}
              className={`type-body-sm flex min-h-11 items-center border-b-2 px-3 font-semibold ${
                t.href === current ? "border-accent text-foreground" : "border-transparent text-muted hover:text-foreground"
              }`}
            >
              {t.label}
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}

const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-");

export const EXEC_LABELS: Record<string, string> = {
  pending: "Pending",
  succeeded: "Done",
  failed: "Failed",
  awaiting_approval: "Awaiting approval",
  approved: "Approved",
  rejected: "Rejected",
};

export const RULE_LABELS: Record<string, string> = {
  procurement_shortage: "Procurement shortage",
  overdue_purchase_order: "Overdue purchase order",
  production_hold_unresolved: "Unresolved quality hold",
  delivery_failure_unresolved: "Failed delivery without a next step",
  run_unreconciled: "Dispatch run not reconciled",
  payment_failed: "Failed payment",
};

export const ACTION_LABELS: Record<string, string> = { alert: "Raise an alert", task: "Create a task", request_approval: "Request an approval" };

/** The automation area's tabs. Tasks are open to all staff (scoped by permission). */
export function automationTabs(read: boolean) {
  return [
    ...(read ? [{ href: "/automation", label: "Rules" }, { href: "/automation/executions", label: "Executions" }] : []),
    { href: "/automation/tasks", label: "My tasks" },
    ...(read ? [{ href: "/automation/reports", label: "Scheduled reports" }] : []),
  ];
}

export const FORECAST_TABS = [
  { href: "/forecasting", label: "Readiness and runs" },
  { href: "/forecasting/settings", label: "Activation criteria" },
];
