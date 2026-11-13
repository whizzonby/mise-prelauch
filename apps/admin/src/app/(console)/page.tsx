import { cx } from "@mise/ui";
import { AlertTriangle, CircleAlert, Info } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { EmptyState, MetricTile, PageHeader, WeeklyBars } from "@/components/admin-ui";
import { BarList } from "@/components/charts";
import { Panel } from "@/components/panel";
import { formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Overview" };

const severityIcon = { critical: AlertTriangle, warning: CircleAlert, info: Info } as const;

/**
 * The operational overview: every figure comes from the API with its
 * definition, the attention list holds only what this person can act on,
 * and nothing is drawn that the data does not support.
 */
export default async function OverviewPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "reports.read")) {
    // Kitchen staff and chefs start in their own workspace.
    if (can(admin, "production.execute")) redirect("/production/tasks");
    if (can(admin, "production.read")) redirect("/production");
    if (can(admin, "recipes.read")) redirect("/recipes");
    return (
      <>
        <PageHeader title={`Hello, ${admin.name.split(" ")[0]}`} description="Your role does not include the business overview. Use the menu to get to your work." />
      </>
    );
  }
  const d = await api.admin.dashboard(token);
  const group = (keys: string[]) => d.metrics.filter((m) => keys.includes(m.key));
  const sections = [
    { title: "Customers and subscriptions", keys: ["customers", "active_subscriptions", "paused_subscriptions", "new_subscriptions", "cancellations"] },
    { title: "Orders", keys: ["orders_this_week", "upcoming_boxes"] },
    { title: "Money", keys: ["invoiced", "collected", "refunded", "outstanding", "successful_payments", "failed_payments"] },
    { title: "Waitlist", keys: ["new_leads", "lead_conversions"] },
  ];
  const attention = d.attention.filter((a) => a.count > 0);

  return (
    <>
      <PageHeader
        title="Overview"
        description={
          <>
            Period figures cover <strong>{d.period_label.toLowerCase()}</strong> ({formatDay(d.period_from)} – {formatDay(d.period_to)}, Trinidad and
            Tobago time). The current delivery week starts {formatDay(d.current_week)}.
            {d.next_cutoff_at ? <> Next selection cutoff: {formatDateTime(d.next_cutoff_at)}.</> : null}
          </>
        }
      />

      <section aria-labelledby="attention" className="mb-8">
        <h2 id="attention" className="type-h4 mb-3">
          Needs attention
        </h2>
        {attention.length ? (
          <ul className="grid gap-2 md:grid-cols-2">
            {attention.map((a) => {
              const Icon = severityIcon[a.severity];
              return (
                <li key={a.key}>
                  <Link
                    href={a.link}
                    className={cx(
                      "flex items-center gap-3 border bg-surface p-3 hover:border-foreground",
                      a.severity === "critical" ? "border-error" : a.severity === "warning" ? "border-warning" : "border-border",
                    )}
                  >
                    <Icon aria-hidden="true" className={cx("size-5 shrink-0", a.severity === "critical" ? "text-error" : a.severity === "warning" ? "text-warning" : "text-muted")} strokeWidth={1.75} />
                    <span className="type-figures font-display text-[1.5rem] leading-none">{a.count}</span>
                    <span className="type-body-sm">{a.label}</span>
                    <span className="sr-only">({a.severity})</span>
                  </Link>
                </li>
              );
            })}
          </ul>
        ) : (
          <EmptyState>Nothing needs attention right now.</EmptyState>
        )}
      </section>

      {sections.map((s) => (
        <section key={s.title} aria-label={s.title} className="mb-6">
          <h2 className="type-h4 mb-3">{s.title}</h2>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {group(s.keys).map((m) => (
              <MetricTile key={m.key} label={m.label} value={m.value} kind={m.kind} currency={m.currency} definition={m.definition} />
            ))}
          </div>
        </section>
      ))}
      <p className="type-caption -mt-2 mb-8 text-muted">Collected is gross receipts before refunds, not profit. Invoiced is billed, not necessarily paid.</p>

      <div className="grid gap-4 lg:grid-cols-2">
        {d.charts.map((c) => (
          <Panel key={c.key} title={c.label} note={c.definition}>
            <WeeklyBars label={c.label} points={c.points} kind={c.kind} currency="TTD" />
          </Panel>
        ))}
        <Panel title="Live subscriptions by plan" note="Including paused">
          <BarList rows={d.plan_distribution.map((p) => ({ label: p.label, count: p.value }))} total={d.plan_distribution.reduce((s, p) => s + p.value, 0)} empty="No live subscriptions yet." />
        </Panel>
      </div>
    </>
  );
}
