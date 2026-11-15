import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { automationTabs, PathTabs } from "@/components/intelligence-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { runReport, saveSchedule } from "../../intelligence-actions";

export const metadata: Metadata = { title: "Scheduled reports" };

/** Reports generated from the same metric definitions as the dashboards, once per period. */
export default async function ReportsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "automation.read")) notFound();
  const [schedules, runs] = await Promise.all([api.intelligence.schedules(token), api.intelligence.reportRuns(token)]);
  const manage = can(admin, "automation.manage");
  const execute = can(admin, "automation.execute");
  return (
    <>
      <PageHeader
        title="Automation"
        description="Each schedule ships off, with no recipients. A report runs once per period; email outcomes are what the mail service reported, and without one they are recorded as skipped."
      />
      <PathTabs tabs={automationTabs(true)} current="/automation/reports" />
      <div className="grid gap-6 [&>*]:min-w-0">
        <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
          {schedules.items.map((s) => (
            <Panel key={s.id} title={s.name} note={`From the ${s.dashboards.join(" and ")} dashboards · recipients need ${s.permission} · next period ${s.next_period}`}>
              <div className="mb-3 flex flex-wrap items-center gap-3">
                <StatusTag status={s.enabled ? "active" : "draft"} label={s.enabled ? "On" : "Off"} />
                <span className="type-caption text-muted">Last generated: {s.last_run ? formatDateTime(s.last_run) : "never"}</span>
              </div>
              {s.missing.length ? <Notice tone="warning" title="Not set up" items={s.missing} /> : null}
              <div className="mt-4">
                <KForm action={saveSchedule} submit="Save schedule" pending="Saving…" label={`Configure ${s.name}`}>
                  <input type="hidden" name="id" value={s.id} />
                  <Labelled label="Recipients (staff emails)" hint="Separate with commas. Each must be an active staff account allowed to see this report.">
                    <TextInput name="recipients" defaultValue={s.recipient_emails.join(", ")} disabled={!manage} />
                  </Labelled>
                  <Labelled label="Approval reference">
                    <TextInput name="approval_ref" defaultValue={s.approval_ref} maxLength={200} disabled={!manage} />
                  </Labelled>
                  <label className="flex min-h-11 items-center gap-3">
                    <input type="checkbox" name="enabled" defaultChecked={s.enabled} disabled={!manage} className="size-5" />
                    Schedule on
                  </label>
                </KForm>
              </div>
              {execute ? (
                <div className="mt-3">
                  <KForm action={runReport} submit="Generate now" pending="Generating…" label={`Generate ${s.name} now`} variant="secondary" inline>
                    <input type="hidden" name="id" value={s.id} />
                  </KForm>
                </div>
              ) : null}
            </Panel>
          ))}
        </div>
        <Panel title="Generated reports">
          {runs.items.length ? (
            <DataTable head={["Report", "Period", "Generated", "Trigger", "Status", "Emails"]} minWidth="44rem">
              {runs.items.map((r) => (
                <tr key={r.id}>
                  <td className="p-3">
                    <Link className="font-semibold underline" href={`/automation/reports/${r.id}`}>
                      {r.name}
                    </Link>
                  </td>
                  <td className="p-3">
                    {formatDay(r.period_start)} – {formatDay(r.period_end)}
                  </td>
                  <td className="p-3">{r.generated_at ? formatDateTime(r.generated_at) : "—"}</td>
                  <td className="p-3">{r.trigger}</td>
                  <td className="p-3">
                    <StatusTag status={r.status} />
                  </td>
                  <td className="p-3">{r.deliveries.length ? r.deliveries.map((d) => `${d.status}`).join(", ") : "None"}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <Empty>No report has been generated yet.</Empty>
          )}
        </Panel>
      </div>
    </>
  );
}
