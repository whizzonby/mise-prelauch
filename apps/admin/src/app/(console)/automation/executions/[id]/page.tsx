import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag, Timeline } from "@/components/admin-ui";
import { Labelled, Panel, TextArea } from "@/components/commerce-ui";
import { ACTION_LABELS, EXEC_LABELS } from "@/components/intelligence-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { DetailList } from "@/components/panel";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { decideExecution, retryExecution } from "../../../intelligence-actions";

export const metadata: Metadata = { title: "Automation execution" };

export default async function ExecutionPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "automation.read")) notFound();
  const { id } = await params;
  const x = await api.intelligence.execution(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  return (
    <>
      <PageHeader title={`${x.rule_name}: ${x.subject_label}`} back={{ href: "/automation/executions", label: "Executions" }} />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[2fr_1fr]">
        <div className="grid gap-6 [&>*]:min-w-0">
          {x.status === "failed" ? <Notice tone="error" title="This action failed" items={[x.last_error ?? "No error was recorded."]} /> : null}
          <Panel title="What happened">
            <DetailList
              items={[
                ["Status", <StatusTag key="s" status={x.status} label={EXEC_LABELS[x.status]} />],
                ["Action", ACTION_LABELS[x.action] ?? x.action],
                ["Record", x.link ? <Link key="l" className="underline" href={x.link}>{x.subject_label}</Link> : x.subject_label],
                ["Why", x.detail || "—"],
                ["Trigger", `${humanize(x.trigger)}${x.trigger_ref ? ` (${x.trigger_ref})` : ""}`],
                ["Rule version", String(x.rule_version)],
                ["Attempts", String(x.attempts)],
                ["Created", formatDateTime(x.created_at)],
                ["Finished", x.finished_at ? formatDateTime(x.finished_at) : "—"],
                ...(x.decided_at ? ([["Decided", `${formatDateTime(x.decided_at)}: ${x.decision_note ?? ""}`]] as [string, string][]) : []),
              ]}
            />
          </Panel>
          <Panel title="History">
            <Timeline items={(x.events ?? []).map((e) => ({ at: e.at, title: humanize(e.type), detail: [e.actor_name, e.body].filter(Boolean).join(" · ") || undefined }))} />
          </Panel>
        </div>
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          {x.status === "failed" && can(admin, "automation.execute") ? (
            <Panel title="Retry" note="Safe to repeat: the action is done at most once.">
              <KForm action={retryExecution} submit="Retry now" pending="Queuing…" label="Retry the execution">
                <input type="hidden" name="id" value={x.id} />
              </KForm>
            </Panel>
          ) : null}
          {x.status === "awaiting_approval" && can(admin, "automation.approve") ? (
            <Panel title="Decide" note="Approving records your decision and tells the owning team. It does not refund, charge, approve an order or change any record.">
              <KForm action={decideExecution} submit="Approve" pending="Saving…" label="Decide the approval" secondary={{ name: "decision", value: "reject", label: "Reject" }}>
                <input type="hidden" name="id" value={x.id} />
                <Labelled label="Why (required)">
                  <TextArea name="note" rows={3} required minLength={3} maxLength={1000} />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}
        </div>
      </div>
    </>
  );
}
