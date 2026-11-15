import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { refreshAnalytics, runDataQuality } from "../../intelligence-actions";

export const metadata: Metadata = { title: "Data quality" };


/** Whether the numbers can be trusted: consistency checks, snapshot freshness and the event consumer. */
export default async function DataQualityPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "analytics_executive.read")) notFound();
  const [checks, status] = await Promise.all([api.intelligence.dataQuality(token), api.intelligence.status(token)]);
  const run = can(admin, "analytics.export");
  const failing = checks.items.filter((c) => c.status === "failed");
  return (
    <>
      <PageHeader
        title="Data quality and freshness"
        back={{ href: "/intelligence", label: "Insights" }}
        description="Checks that the records analytics reads agree with each other. A failed check names how many records are affected; open the records to correct them in their own module."
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        {status.stale ? (
          <Notice tone="warning" title="Snapshots are stale" items={[`No successful refresh within ${status.stale_after}. Dashboards still calculate live; stored daily series may be behind.`]} />
        ) : null}
        {failing.length ? <Notice tone="error" title={`${failing.length} check(s) failed`} items={failing.map((c) => `${c.name}: ${c.detail}`)} /> : null}
        <Panel title="Checks" note={checks.items[0] ? `Last run ${formatDateTime(checks.items[0].checked_at)}.` : "Not run yet."}>
          {checks.items.length ? (
            <DataTable head={["Check", "Result", "Records", "Detail"]} minWidth="40rem">
              {checks.items.map((c) => (
                <tr key={c.id}>
                  <td className="p-3">
                    <span className="font-semibold">{c.name}</span>
                    <span className="type-caption block text-muted">{c.description}</span>
                  </td>
                  <td className="p-3">
                    <StatusTag status={c.status} label={c.status === "ok" ? "OK" : c.status === "warning" ? "Warning" : "Failed"} />
                  </td>
                  <td className="type-figures p-3">{c.count}</td>
                  <td className="type-body-sm p-3">{c.detail}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <Empty>No checks have run yet.</Empty>
          )}
          {run ? (
            <div className="mt-4">
              <KForm action={runDataQuality} submit="Run checks now" pending="Running…" label="Run data-quality checks" variant="secondary" inline />
            </div>
          ) : null}
        </Panel>
        <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
          <Panel title="Snapshots" note={`${status.snapshots} stored daily values. Last success: ${status.last_success ? formatDateTime(status.last_success) : "never"}.`}>
            {status.runs.length ? (
              <DataTable head={["Started", "Reason", "Range", "Result"]} minWidth="30rem">
                {status.runs.map((r) => (
                  <tr key={r.id}>
                    <td className="p-3">{formatDateTime(r.started_at)}</td>
                    <td className="p-3">{r.reason}</td>
                    <td className="p-3">
                      {formatDay(r.from)} – {formatDay(r.to)}
                    </td>
                    <td className="p-3">{r.status === "failed" ? `Failed: ${r.error ?? ""}` : r.status === "succeeded" ? `${r.values_written} values` : "Running"}</td>
                  </tr>
                ))}
              </DataTable>
            ) : (
              <Empty>No refresh has run yet.</Empty>
            )}
            {run ? (
              <div className="mt-4">
                <KForm action={refreshAnalytics} submit="Refresh or backfill" pending="Queuing…" label="Refresh analytics snapshots" variant="secondary">
                  <div className="flex flex-wrap gap-3">
                    <Labelled label="From (optional)">
                      <TextInput type="date" name="from" />
                    </Labelled>
                    <Labelled label="To (optional)">
                      <TextInput type="date" name="to" />
                    </Labelled>
                  </div>
                </KForm>
              </div>
            ) : null}
          </Panel>
          <Panel title="Event consumer" note="Domain events refresh the day they happened. Replaying them changes nothing.">
            {status.consumer ? (
              <ul className="type-body-sm grid gap-1">
                <li>Last event processed: #{status.consumer.last_event_id}</li>
                <li>Waiting: {status.consumer.waiting}</li>
                <li>Failed jobs: {status.consumer.failed.length}</li>
              </ul>
            ) : (
              <Empty>Consumer status unavailable.</Empty>
            )}
          </Panel>
        </div>
      </div>
    </>
  );
}
