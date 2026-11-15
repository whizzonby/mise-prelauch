import type { Metadata } from "next";
import Link from "next/link";

import { FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { Empty, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { automationTabs, PathTabs } from "@/components/intelligence-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { closeTask } from "../../intelligence-actions";

export const metadata: Metadata = { title: "Tasks" };

/** Tasks automation created for the teams this person belongs to. */
export default async function TasksPage({ searchParams }: { searchParams: Promise<{ status?: string }> }) {
  const { token, admin } = await requireAdmin();
  const { status = "open" } = await searchParams;
  const valid = ["open", "done", "dismissed", "all"].includes(status) ? status : "open";
  const { items } = await api.intelligence.tasks(token, valid);
  return (
    <>
      <PageHeader title="Automation" description="Follow-ups raised by approved rules for your role. Close a task once the record itself is dealt with in its own module." />
      <PathTabs tabs={automationTabs(can(admin, "automation.read"))} current="/automation/tasks" />
      <FilterBar>
        <Labelled label="Show">
          <SelectInput name="status" defaultValue={valid}>
            <option value="open">Open</option>
            <option value="done">Done</option>
            <option value="dismissed">Dismissed</option>
            <option value="all">All</option>
          </SelectInput>
        </Labelled>
      </FilterBar>
      {items.length ? (
        <div className="grid gap-4 [&>*]:min-w-0">
          {items.map((t) => (
            <Panel key={t.id} title={t.title} note={`Raised ${formatDateTime(t.created_at)} · for ${t.permission}`}>
              <p className="type-body-sm">{t.detail}</p>
              <p className="type-body-sm mt-2 flex flex-wrap gap-3">
                <StatusTag status={t.status} />
                {t.link ? (
                  <Link className="underline" href={t.link}>
                    Open the record
                  </Link>
                ) : null}
                {t.execution_id && can(admin, "automation.read") ? (
                  <Link className="underline" href={`/automation/executions/${t.execution_id}`}>
                    Why this task exists
                  </Link>
                ) : null}
              </p>
              {t.close_note ? <p className="type-caption mt-2 text-muted">Note: {t.close_note}</p> : null}
              {t.status === "open" ? (
                <div className="mt-4">
                  <KForm action={closeTask} submit="Mark done" pending="Saving…" label={`Close ${t.title}`} inline secondary={{ name: "status", value: "dismissed", label: "Dismiss" }}>
                    <input type="hidden" name="id" value={t.id} />
                    <Labelled label="Note (optional)">
                      <TextInput name="note" maxLength={1000} />
                    </Labelled>
                  </KForm>
                </div>
              ) : null}
            </Panel>
          ))}
        </div>
      ) : (
        <Empty>No {valid === "all" ? "" : `${valid} `}tasks for your role.</Empty>
      )}
    </>
  );
}
