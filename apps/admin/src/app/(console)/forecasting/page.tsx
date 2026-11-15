import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Panel } from "@/components/commerce-ui";
import { CountTile } from "@/components/delivery-ui";
import { FORECAST_TABS, PathTabs } from "@/components/intelligence-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay, number } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { runForecast } from "../intelligence-actions";

export const metadata: Metadata = { title: "Forecasting" };

/**
 * Whether there is enough verified history to forecast, and every run so
 * far. Test-provider payments, unpaid weeks and demo meals are counted
 * apart and never used. Forecasts are advisory: they never change a plan,
 * a procurement run or a purchase order.
 */
export default async function ForecastingPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "forecasting.read")) notFound();
  const [r, runs] = await Promise.all([api.intelligence.readiness(token), api.intelligence.forecastRuns(token)]);
  return (
    <>
      <PageHeader title="Demand forecasting" description="Baseline forecasts of meal-kit demand, tested against history before they are shown. Advisory only." />
      <PathTabs tabs={FORECAST_TABS} current="/forecasting" />
      <div className="grid gap-6 [&>*]:min-w-0">
        {r.ready ? (
          <Notice tone="info" title={r.message} items={[`History ${r.history_from ? formatDay(r.history_from) : ""} to ${r.history_to ? formatDay(r.history_to) : ""}.`]} />
        ) : (
          <Notice tone="warning" title={r.message} items={r.reasons} />
        )}
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <CountTile label="Verified weeks" value={r.verified_weeks} />
          <CountTile label="Verified kits" value={r.verified_kits} />
          <CountTile label="Test-payment kits (excluded)" value={r.test_paid_kits} tone={r.test_paid_kits ? "warn" : undefined} />
          <CountTile label="Demo-meal kits (excluded)" value={r.demo_meal_kits} tone={r.demo_meal_kits ? "warn" : undefined} />
        </div>
        <Panel title="Readiness" note="Verified demand: confirmed orders paid through a real payment provider, for meals that are not demo content.">
          <DataTable head={["Criterion", "Required", "Now", "Met"]} minWidth="32rem">
            {r.criteria.map((c) => (
              <tr key={c.name}>
                <td className="p-3 font-semibold">{c.name}</td>
                <td className="p-3">{c.required ?? "Not decided"}</td>
                <td className="p-3">{c.actual}</td>
                <td className="p-3">{c.required === null ? "—" : c.met ? "Yes" : "No"}</td>
              </tr>
            ))}
          </DataTable>
          <ul className="type-body-sm mt-4 grid gap-1 text-muted">
            <li>
              Weeks spanned: {r.span_weeks}
              {r.missing_share !== null ? ` · missing ${Math.round(r.missing_share * 100)}%` : ""} · meals with history: {r.meals_with_history}
            </li>
            <li>
              Orders confirmed: {number.format(r.confirmed_orders)} · cancelled: {number.format(r.cancelled_orders)} · unpaid (free) kits excluded: {number.format(r.unpaid_kits)}
            </li>
            <li>
              Unusual weeks (beyond 3 interquartile ranges): {r.outlier_weeks} · meals new in the last 8 weeks: {r.new_meals_last_8_weeks}
              {r.seasonal_coverage ? ` · seasonal coverage: ${r.seasonal_coverage}` : ""}
            </li>
          </ul>
          {can(admin, "forecasting.run") ? (
            <div className="mt-4">
              <KForm action={runForecast} submit="Run forecast" pending="Running…" label="Run a forecast" variant={r.ready ? "primary" : "secondary"} inline />
              {!r.ready ? <p className="type-caption mt-2 text-muted">A run now is recorded as refused, with the reasons above.</p> : null}
            </div>
          ) : null}
        </Panel>
        <Panel title="Runs" note="Kept unchanged: each run records its inputs, method and readiness.">
          {runs.items.length ? (
            <DataTable head={["When", "By", "Result", "Method", "History", "Horizon"]} minWidth="40rem">
              {runs.items.map((x) => (
                <tr key={x.id}>
                  <td className="p-3">
                    <Link className="font-semibold underline" href={`/forecasting/runs/${x.id}`}>
                      {formatDateTime(x.created_at)}
                    </Link>
                  </td>
                  <td className="p-3">{x.actor_name ?? x.trigger}</td>
                  <td className="p-3">
                    <StatusTag status={x.status} />
                  </td>
                  <td className="p-3">{x.method ?? "—"}</td>
                  <td className="p-3">{x.input_from && x.input_to ? `${formatDay(x.input_from)} – ${formatDay(x.input_to)}` : "—"}</td>
                  <td className="p-3">{x.horizon_weeks} weeks</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <Empty>No forecast has been run.</Empty>
          )}
        </Panel>
      </div>
    </>
  );
}
