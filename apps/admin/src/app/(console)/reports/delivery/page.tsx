import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { FilterBar, PageHeader } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { Definitions, iso, validDay } from "@/components/supply-ui";
import { formatDay, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Delivery reports" };

const pct = (v: unknown) => (typeof v === "number" ? `${Math.round(v * 100)}%` : "Not measurable");

export default async function DeliveryReportPage({ searchParams }: { searchParams: Promise<{ from?: string; to?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_reports.read")) notFound();
  const sp = await searchParams;
  const now = new Date();
  const to = validDay(sp.to) ?? iso(now);
  const from = validDay(sp.from) ?? iso(new Date(now.getTime() - 27 * 864e5));
  const r = await api.delivery.report(token, from, to);
  const t = r.totals as Record<string, number | null>;
  const tiles: [string, string][] = [
    ["Deliveries", String(t.deliveries ?? 0)],
    ["Delivered", String(t.delivered ?? 0)],
    ["First-attempt success", pct(t.first_attempt_rate)],
    ["On time", pct(t.on_time_rate)],
    ["Failed attempts", String(t.failed_attempts ?? 0)],
    ["Needed a reattempt", String(t.reattempts ?? 0)],
    ["Returned", String(t.returned ?? 0)],
    ["Proof completeness", pct(t.proof_completeness)],
    ["Unresolved exceptions", String(t.unresolved_exceptions ?? 0)],
    ["Corrected outcomes", String(t.corrected_outcomes ?? 0)],
    ["Disputed outcomes", String(t.disputed_outcomes ?? 0)],
  ];
  const table = (rows: typeof r.by_zone) =>
    rows.length ? (
      <DataTable head={["", "Deliveries", "Delivered", "First attempt", "Failed attempts", "On time"]} minWidth="36rem">
        {rows.map((b) => (
          <tr key={b.key}>
            <td className="p-3 font-semibold">{b.label}</td>
            <td className="p-3">{b.deliveries}</td>
            <td className="p-3">{b.delivered}</td>
            <td className="p-3">{b.first_attempt_delivered}</td>
            <td className="p-3">{b.failed_attempts}</td>
            <td className="p-3">{b.on_time_measurable ? `${b.on_time}/${b.on_time_measurable}` : "—"}</td>
          </tr>
        ))}
      </DataTable>
    ) : (
      <Empty>No deliveries in this range.</Empty>
    );
  return (
    <>
      <PageHeader title="Delivery reports" description={`Deliveries with a confirmed date from ${formatDay(r.from)} to ${formatDay(r.to)}. Counted from records; nothing is estimated.`} />
      <FilterBar>
        <Labelled label="From">
          <TextInput type="date" name="from" defaultValue={r.from} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={r.to} />
        </Labelled>
      </FilterBar>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5">
        {tiles.map(([label, value]) => (
          <div key={label} className="border border-border bg-surface p-4">
            <span className="type-caption block text-muted">{label}</span>
            <span className="mt-1 block font-display text-[1.75rem] leading-none">{value}</span>
          </div>
        ))}
      </div>
      <Definitions defs={r.definitions} />
      <div className="mt-6 grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
        <Panel title="By partner">{table(r.by_partner)}</Panel>
        <Panel title="By zone">{table(r.by_zone)}</Panel>
        <Panel title="Recorded partner costs" note="Estimated, contracted and actual are shown apart.">
          {r.costs.length ? (
            <ul className="type-body-sm grid gap-1">
              {r.costs.map((c) => (
                <li key={c.kind + c.currency}>
                  {humanize(c.kind)}: {money(c.amount_minor, c.currency)} ({c.records} record{c.records === 1 ? "" : "s"})
                </li>
              ))}
            </ul>
          ) : (
            <p className="type-body-sm text-muted">No costs recorded.</p>
          )}
        </Panel>
      </div>
    </>
  );
}
