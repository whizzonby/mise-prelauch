import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, SelectInput } from "@/components/commerce-ui";
import { ACTION_LABELS, automationTabs, EXEC_LABELS, PathTabs } from "@/components/intelligence-ui";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Automation executions" };

/** Every time a rule acted: what triggered it, for which record, and what happened. */
export default async function ExecutionsPage({ searchParams }: { searchParams: Promise<{ status?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "automation.read")) notFound();
  const { status = "all" } = await searchParams;
  const valid = status === "all" || status in EXEC_LABELS ? status : "all";
  const { items } = await api.intelligence.executions(token, { status: valid });
  return (
    <>
      <PageHeader title="Automation" description="Executions are kept as history. Failures stay visible until retried." />
      <PathTabs tabs={automationTabs(true)} current="/automation/executions" />
      <FilterBar>
        <Labelled label="Status">
          <SelectInput name="status" defaultValue={valid}>
            <option value="all">All</option>
            {Object.entries(EXEC_LABELS).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </SelectInput>
        </Labelled>
      </FilterBar>
      {items.length ? (
        <DataTable head={["When", "Rule", "Record", "Action", "Trigger", "Status"]} minWidth="48rem">
          {items.map((x) => (
            <tr key={x.id}>
              <td className="p-3">
                <Link className="font-semibold underline" href={`/automation/executions/${x.id}`}>
                  {formatDateTime(x.created_at)}
                </Link>
              </td>
              <td className="p-3">
                {x.rule_name}
                <span className="type-caption block text-muted">version {x.rule_version}</span>
              </td>
              <td className="p-3">{x.subject_label}</td>
              <td className="p-3">{ACTION_LABELS[x.action] ?? x.action}</td>
              <td className="p-3">{x.trigger}</td>
              <td className="p-3">
                <StatusTag status={x.status} label={EXEC_LABELS[x.status]} />
                {x.attempts > 1 ? <span className="type-caption block text-muted">{x.attempts} attempts</span> : null}
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>No executions{valid !== "all" ? ` with status “${EXEC_LABELS[valid]}”` : ""}. Rules run only once they are approved and switched on.</Empty>
      )}
    </>
  );
}
