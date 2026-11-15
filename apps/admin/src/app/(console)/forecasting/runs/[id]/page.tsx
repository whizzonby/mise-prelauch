import { ApiError, type ForecastIngredients, type ForecastReadiness } from "@mise/api-client";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, Panel, SelectInput } from "@/components/commerce-ui";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay, humanize, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Forecast run" };

const n2 = (v: number | null | undefined) => (v === null || v === undefined ? "Not defined" : v.toLocaleString("en-GB", { maximumFractionDigits: 2 }));

/** One run: its accuracy against history, the expected kits, and (on request) the ingredients they would need. */
export default async function ForecastRunPage({ params, searchParams }: { params: Promise<{ id: string }>; searchParams: Promise<{ week?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "forecasting.read")) notFound();
  const { id } = await params;
  const { week } = await searchParams;
  const run = await api.intelligence.forecastRun(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const readiness = run.readiness as ForecastReadiness;
  const weeks = [...new Set((run.values ?? []).map((v) => v.week))];
  const chosen = week && weeks.includes(week) ? week : weeks[0];
  let ingredients: ForecastIngredients | null = null;
  if (run.status === "completed" && chosen) ingredients = await api.intelligence.forecastIngredients(token, run.id, chosen);
  return (
    <>
      <PageHeader
        title={`Forecast of ${formatDateTime(run.created_at)}`}
        back={{ href: "/forecasting", label: "Forecasting" }}
        description={
          run.status === "completed"
            ? `${run.method} (version ${run.method_version}) from verified history ${formatDay(run.input_from!)} to ${formatDay(run.input_to!)}, ${run.horizon_weeks} weeks ahead.`
            : "This run was refused. Nothing was forecast."
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <p>
          <StatusTag status={run.status} />
        </p>
        {run.status === "refused" ? <Notice tone="warning" title={readiness.message} items={readiness.reasons} /> : null}
        {run.notes ? <p className="type-body-sm text-muted">{run.notes}</p> : null}
        {run.status === "completed" ? (
          <>
            <Notice
              tone="info"
              title="Advisory only"
              items={["These are expected kits, not orders. They never change a production plan, procurement run or purchase order."]}
            />
            <Panel title="Accuracy against history" note="Rolling-origin backtest: each fold forecasts only from earlier weeks. A method is chosen only if it beats the naive forecast by the approved margin.">
              <DataTable head={["Method", "Folds", "MAE (kits)", "WAPE", "Bias (kits)", "MASE", ""]} minWidth="40rem">
                {(run.backtests ?? []).map((b) => (
                  <tr key={b.method}>
                    <td className="p-3 font-semibold">{b.method}</td>
                    <td className="type-figures p-3">{b.folds}</td>
                    <td className="type-figures p-3">{n2(b.mae)}</td>
                    <td className="type-figures p-3">{b.wape === null ? "Not defined" : `${(b.wape * 100).toFixed(1)}%`}</td>
                    <td className="type-figures p-3">{n2(b.bias)}</td>
                    <td className="type-figures p-3">{n2(b.mase)}</td>
                    <td className="p-3">{b.selected ? <StatusTag status="approved" label="Selected" /> : null}</td>
                  </tr>
                ))}
              </DataTable>
              <p className="type-caption mt-2 text-muted">
                MAE: average absolute error. WAPE: total error over total demand (not defined with no demand). Bias: positive means over-forecasting. MASE: error relative to the naive
                forecast&apos;s (not defined on a flat history).
              </p>
            </Panel>
            <Panel title="Expected kits" note="Ranges come from backtest errors and are shown only when there are enough of them.">
              {run.values?.length ? (
                <DataTable head={["Week", "Meal", "Expected", "Range"]} minWidth="32rem">
                  {run.values.map((v) => (
                    <tr key={v.week + v.meal_id}>
                      <td className="p-3">{formatDay(v.week)}</td>
                      <td className="p-3 font-semibold">{v.meal_name}</td>
                      <td className="type-figures p-3">{n2(v.expected)}</td>
                      <td className="type-figures p-3">{v.lower !== null && v.upper !== null ? `${n2(v.lower)} – ${n2(v.upper)}` : "Not estimated"}</td>
                    </tr>
                  ))}
                </DataTable>
              ) : (
                <Empty>No values.</Empty>
              )}
            </Panel>
            {ingredients ? (
              <Panel title="Projected ingredients" note={ingredients.label}>
                <FilterBar>
                  <Labelled label="Week">
                    <SelectInput name="week" defaultValue={chosen}>
                      {weeks.map((w) => (
                        <option key={w} value={w}>
                          {formatDay(w)}
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                </FilterBar>
                {ingredients.meals_without_recipe.length ? (
                  <Notice tone="warning" title="No published recipe for that week" items={ingredients.meals_without_recipe} />
                ) : null}
                {ingredients.needs.length ? (
                  <DataTable head={["Ingredient", "Preparation", "Quantity", "Status"]} minWidth="32rem">
                    {ingredients.needs.map((n) => (
                      <tr key={n.ingredient_id + n.preparation}>
                        <td className="p-3 font-semibold">{n.ingredient_name}</td>
                        <td className="p-3">{humanize(n.preparation)}</td>
                        <td className="type-figures p-3">
                          {n.quantity ? quantity(n.quantity, n.unit) : "—"}
                          {n.approximate ? " (approximate)" : ""}
                        </td>
                        <td className="p-3">
                          <StatusTag status={n.status} />
                          {n.note ? <span className="type-caption block text-muted">{n.note}</span> : null}
                        </td>
                      </tr>
                    ))}
                  </DataTable>
                ) : (
                  <Empty>No ingredients for this week.</Empty>
                )}
              </Panel>
            ) : null}
          </>
        ) : null}
      </div>
    </>
  );
}
