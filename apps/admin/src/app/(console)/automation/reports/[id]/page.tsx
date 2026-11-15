import { ApiError, type AnalyticsDashboard } from "@mise/api-client";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Panel } from "@/components/commerce-ui";
import { DashboardView } from "@/components/intelligence-ui";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Generated report" };

/** A generated report exactly as it was produced (stored content; figures are not recalculated). */
export default async function ReportRunPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "automation.read")) notFound();
  const { id } = await params;
  const r = await api.intelligence.reportRun(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const boards = ((r.content as { dashboards?: AnalyticsDashboard[] } | undefined)?.dashboards ?? []) as AnalyticsDashboard[];
  return (
    <>
      <PageHeader
        title={r.name}
        back={{ href: "/automation/reports", label: "Scheduled reports" }}
        description={`${formatDay(r.period_start)} to ${formatDay(r.period_end)} · generated ${r.generated_at ? formatDateTime(r.generated_at) : "—"} (${r.trigger}). Stored as generated.`}
        actions={
          r.status === "succeeded" && can(admin, "analytics.export") ? (
            <a
              className="type-body-sm inline-flex min-h-11 items-center rounded-md border border-foreground px-4 font-semibold hover:bg-foreground hover:text-surface"
              href={`/automation/reports/${r.id}/csv`}
            >
              Download CSV
            </a>
          ) : undefined
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        {r.status === "failed" ? <Notice tone="error" title="Generation failed" items={[r.error ?? "No error was recorded."]} /> : null}
        <Panel title="Email delivery" note="As reported by the mail service. Skipped means it was not sent.">
          {r.deliveries.length ? (
            <DataTable head={["Recipient", "Outcome", "Reason"]} minWidth="30rem">
              {r.deliveries.map((d) => (
                <tr key={d.email}>
                  <td className="p-3">{d.email}</td>
                  <td className="p-3">
                    <StatusTag status={d.status} />
                  </td>
                  <td className="type-body-sm p-3">{d.reason ?? "—"}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <p className="type-body-sm text-muted">No recipients: the report is kept here only.</p>
          )}
        </Panel>
        {boards.map((b) => (
          <Panel key={b.kind} title={b.title}>
            <DashboardView d={b} />
          </Panel>
        ))}
      </div>
    </>
  );
}
