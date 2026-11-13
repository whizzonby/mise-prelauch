import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { FilterBar, MetricTile, PageHeader, WeeklyBars } from "@/components/admin-ui";
import { BarList } from "@/components/charts";
import { Labelled, TextInput } from "@/components/commerce-ui";
import { Panel } from "@/components/panel";
import { formatDay, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Production report" };

const iso = (d: Date) => d.toISOString().slice(0, 10);
const valid = (s: string | undefined) => (s && /^\d{4}-\d{2}-\d{2}$/.test(s) ? s : undefined);

/**
 * Planned against completed production by production date, from batches,
 * yield records, holds and waste. Every figure is defined; no forecasts.
 */
export default async function ProductionReportPage({ searchParams }: { searchParams: Promise<{ from?: string; to?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production_reports.read")) notFound();
  const sp = await searchParams;
  const today = new Date();
  const to = valid(sp.to) ?? iso(new Date(today.getTime() + 7 * 86400000));
  const from = valid(sp.from) ?? iso(new Date(today.getTime() - 27 * 86400000));
  const r = await api.kitchen.productionReport(token, from, to);
  return (
    <>
      <PageHeader
        title="Production report"
        description={`${formatDay(r.from)} – ${formatDay(r.to)} by production date, ${humanize(r.time_zone.split("/").pop() ?? r.time_zone)} time. Costs are not shown: without procurement prices (Sprint 6) any margin would be invented.`}
      />
      <FilterBar>
        <Labelled label="From">
          <TextInput type="date" name="from" defaultValue={from} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={to} />
        </Labelled>
      </FilterBar>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {r.totals.map((m) => (
          <MetricTile key={m.key} label={m.label} value={m.value} kind={m.kind} definition={m.definition} />
        ))}
      </div>
      <div className="mt-6 grid gap-4 lg:grid-cols-2">
        {r.series.map((s) => (
          <Panel key={s.key} title={s.label} note={s.definition}>
            <WeeklyBars label={s.label} points={s.points} kind={s.kind} daily={r.interval === "day"} />
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
