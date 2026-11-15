import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { FORECAST_TABS, PathTabs } from "@/components/intelligence-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { saveForecastSettings } from "../../intelligence-actions";

export const metadata: Metadata = { title: "Forecast criteria" };

/** The activation criteria. They ship unset: until the business decides them, no forecast is produced. */
export default async function ForecastSettingsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "forecasting.read")) notFound();
  const s = await api.intelligence.forecastSettings(token);
  const edit = can(admin, "forecasting.configure");
  return (
    <>
      <PageHeader title="Demand forecasting" description="Thresholds the business approves before any forecast is shown. Changes are audited." />
      <PathTabs tabs={FORECAST_TABS} current="/forecasting/settings" />
      <Panel title="Activation criteria" note={`Last changed ${formatDateTime(s.updated_at)}. Leave a field empty to keep it undecided.`}>
        <KForm action={saveForecastSettings} submit="Save criteria" pending="Saving…" label="Forecast activation criteria">
          <div className="grid gap-4 sm:grid-cols-2">
            <Labelled label="Minimum weeks of verified history" hint="4 to 520">
              <TextInput type="number" name="min_weeks" min={4} max={520} defaultValue={s.min_weeks ?? ""} disabled={!edit} />
            </Labelled>
            <Labelled label="Largest share of missing weeks" hint="0 to 1, e.g. 0.2 for 20%">
              <TextInput type="number" step="0.01" name="max_missing_share" min={0} max={1} defaultValue={s.max_missing_share ?? ""} disabled={!edit} />
            </Labelled>
            <Labelled label="Required improvement over the naive forecast" hint="0 to 1; a method must beat naive by this share">
              <TextInput type="number" step="0.01" name="min_improvement" min={0} max={1} defaultValue={s.min_improvement ?? ""} disabled={!edit} />
            </Labelled>
            <Labelled label="Backtest errors needed for a range" hint="5 to 1,000; empty shows no range">
              <TextInput type="number" name="min_interval_residuals" min={5} max={1000} defaultValue={s.min_interval_residuals ?? ""} disabled={!edit} />
            </Labelled>
            <Labelled label="Season length in weeks (optional)" hint="2 to 52; empty means no seasonal method">
              <TextInput type="number" name="season_weeks" min={2} max={52} defaultValue={s.season_weeks ?? ""} disabled={!edit} />
            </Labelled>
            <Labelled label="Weeks ahead to forecast" hint="1 to 12">
              <TextInput type="number" name="horizon_weeks" min={1} max={12} defaultValue={s.horizon_weeks} disabled={!edit} />
            </Labelled>
          </div>
          <Labelled label="Approval reference" hint="The decision that approved these criteria (required once any is set)">
            <TextInput name="approval_ref" defaultValue={s.approval_ref} maxLength={200} disabled={!edit} />
          </Labelled>
        </KForm>
      </Panel>
    </>
  );
}
