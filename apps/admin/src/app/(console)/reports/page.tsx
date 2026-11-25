import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { FilterBar, MetricTile, PageHeader, Tabs, WeeklyBars } from "@/components/admin-ui";
import { BarList } from "@/components/charts";
import { Labelled, TextInput } from "@/components/commerce-ui";
import { Panel } from "@/components/panel";
import { businessDay, formatDay, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Reports" };

const KINDS = [
  { key: "customers", label: "Customers" },
  { key: "subscriptions", label: "Subscriptions" },
  { key: "orders", label: "Orders" },
  { key: "finance", label: "Finance" },
  { key: "marketing", label: "Marketing" },
];

const iso = (d: Date) => businessDay(d);
const valid = (s: string | undefined) => (s && /^\d{4}-\d{2}-\d{2}$/.test(s) ? s : undefined);

/**
 * Business reports over a date range in Trinidad and Tobago time. Each
 * figure comes with its definition; ranges up to 62 days are drawn by day,
 * longer ones by week (Monday to Sunday).
 */
export default async function ReportsPage({ searchParams }: { searchParams: Promise<{ tab?: string; from?: string; to?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "reports.read")) notFound();
  const sp = await searchParams;
  const kind = KINDS.some((k) => k.key === sp.tab) ? sp.tab! : "customers";
  const today = new Date();
  const to = valid(sp.to) ?? iso(today);
  const from = valid(sp.from) ?? iso(new Date(today.getTime() - 83 * 86400000));
  const r = await api.admin.report(token, kind, from, to);
  const range = `from=${from}&to=${to}`;
  const currency = r.totals.find((m) => m.currency)?.currency;

  return (
    <>
      <PageHeader
        title="Reports"
        description={
          <>
            {formatDay(r.from)} – {formatDay(r.to)}, {humanize(r.time_zone.split("/").pop() ?? r.time_zone)} time, by {r.interval}.{" "}
            {can(admin, "analytics.view") ? (
              <Link href="/reports/waitlist" className="underline underline-offset-4">
                Waitlist statistics
              </Link>
            ) : null}
          </>
        }
      />
      <FilterBar>
        <input type="hidden" name="tab" value={kind} />
        <Labelled label="From">
          <TextInput type="date" name="from" defaultValue={from} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={to} />
        </Labelled>
      </FilterBar>
      <Tabs base={`/reports?${range}`} current={kind} tabs={KINDS} />
      <div className="mt-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {r.totals.map((m) => (
          <MetricTile key={m.key} label={m.label} value={m.value} kind={m.kind} currency={m.currency} definition={m.definition} />
        ))}
      </div>
      <div className="mt-6 grid gap-4 lg:grid-cols-2">
        {r.series.map((s) => (
          <Panel key={s.key} title={s.label} note={s.definition}>
            <WeeklyBars label={s.label} points={s.points} kind={s.kind} currency={currency} daily={r.interval === "day"} />
          </Panel>
        ))}
        {Object.entries(r.breakdowns).map(([key, rows]) => (
          <Panel key={key} title={humanize(key)} note={r.definitions[key]}>
            <BarList rows={rows.map((x) => ({ label: x.label, count: x.value }))} total={rows.reduce((t, x) => t + x.value, 0)} empty="Nothing in this range." />
          </Panel>
        ))}
      </div>
    </>
  );
}
