import type { Report } from "@mise/api-client";
import type { ReactNode } from "react";

import { FilterBar, MetricTile, WeeklyBars } from "@/components/admin-ui";
import { BarList } from "@/components/charts";
import { Labelled, TextInput } from "@/components/commerce-ui";
import { Panel } from "@/components/panel";
import { formatDay, humanize } from "@/lib/format";

export const SUPPLIER_TYPES = ["farmer", "cooperative", "producer", "distributor", "packaging", "other"];
export const CHANNELS = ["phone", "whatsapp", "email", "in_person", "other"];
export const CONTACT_CHANNELS = ["phone", "whatsapp", "email", "in_person", "document", "other"];
export const AVAILABILITY = ["available_now", "expected_harvest", "limited", "unavailable", "seasonal"];
export const CONFIDENCE = ["confirmed_by_supplier", "supplier_estimate", "staff_estimate"];
export const LOCATION_TYPES = ["receiving", "dry", "chilled", "frozen", "preparation", "quarantine", "other"];
export const DISCREPANCIES = ["none", "short", "over", "damaged", "wrong_item", "specification", "expiry", "temperature", "documentation", "other"];
export const ADJUST_CATEGORIES = ["count_discrepancy", "damage", "spoilage", "measurement_correction", "other"];

/** Today and a range, as YYYY-MM-DD, for report filters. */
export const iso = (d: Date) => d.toISOString().slice(0, 10);
export const validDay = (s: string | undefined) => (s && /^\d{4}-\d{2}-\d{2}$/.test(s) ? s : undefined);

/** A whole supply-chain report: totals, series and breakdowns, each defined. */
export function ReportView({ r, note }: { r: Report; note?: ReactNode }) {
  return (
    <>
      <p className="type-body-sm mb-4 max-w-[56rem] text-muted">
        {formatDay(r.from)} – {formatDay(r.to)}, {humanize(r.time_zone.split("/").pop() ?? r.time_zone)} time. {note}
      </p>
      <FilterBar>
        <Labelled label="From">
          <TextInput type="date" name="from" defaultValue={r.from} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={r.to} />
        </Labelled>
      </FilterBar>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {(r.totals ?? []).map((m) => (
          <MetricTile key={m.key} label={m.label} value={m.value} kind={m.kind} currency={m.currency ?? undefined} definition={m.definition} />
        ))}
      </div>
      <div className="mt-6 grid gap-4 [&>*]:min-w-0 lg:grid-cols-2">
        {(r.series ?? []).map((s) => (
          <Panel key={s.key} title={s.label} note={s.definition}>
            <WeeklyBars label={s.label} points={s.points} kind={s.kind} currency={s.kind === "money" ? "TTD" : undefined} daily={r.interval === "day"} />
          </Panel>
        ))}
        {Object.entries(r.breakdowns ?? {}).map(([key, rows]) => (
          <Panel key={key} title={humanize(key)} note={r.definitions[key]}>
            <BarList rows={rows.map((x) => ({ label: x.label, count: x.value }))} total={rows.reduce((t, x) => t + x.value, 0)} empty="Nothing in this range." />
          </Panel>
        ))}
      </div>
    </>
  );
}

/** The definition of each figure, as a compact list. */
export function Definitions({ defs, keys }: { defs: Record<string, string>; keys?: string[] }) {
  const list = (keys ?? Object.keys(defs)).filter((k) => defs[k]);
  return (
    <details className="type-caption mt-3">
      <summary className="cursor-pointer font-semibold underline underline-offset-4">What each figure means</summary>
      <dl className="mt-2 grid gap-1 sm:grid-cols-[12rem_1fr]">
        {list.map((k) => (
          <div key={k} className="contents">
            <dt className="font-semibold">{humanize(k)}</dt>
            <dd className="text-muted">{defs[k]}</dd>
          </div>
        ))}
      </dl>
    </details>
  );
}
