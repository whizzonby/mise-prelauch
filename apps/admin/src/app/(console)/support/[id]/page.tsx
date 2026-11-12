import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag, Timeline } from "@/components/admin-ui";
import { CatalogForm } from "@/components/catalog-form";
import { Labelled, Panel, SelectInput, TextArea } from "@/components/commerce-ui";
import { DetailList } from "@/components/panel";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { caseNote, updateCase } from "../../crm-actions";

export const metadata: Metadata = { title: "Case" };

const NEXT: Record<string, string[]> = {
  open: ["in_progress", "waiting_for_customer", "resolved", "closed"],
  in_progress: ["open", "waiting_for_customer", "resolved", "closed"],
  waiting_for_customer: ["open", "in_progress", "resolved", "closed"],
  resolved: ["open", "in_progress", "closed"],
  closed: [],
};

function describe(e: { type: string; from: string | null; to: string | null }) {
  switch (e.type) {
    case "created":
      return "Opened";
    case "status":
      return `Status: ${humanize(e.from ?? "")} → ${humanize(e.to ?? "")}`;
    case "priority":
      return `Priority: ${humanize(e.from ?? "")} → ${humanize(e.to ?? "")}`;
    case "assignment":
      return e.to ? "Reassigned" : "Unassigned";
    case "resolution":
      return "Resolution recorded";
    default:
      return "Note";
  }
}

export default async function CasePage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "cases.read")) notFound();
  const { id } = await params;
  const c = await api.admin.case(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const staff = can(admin, "users.read") ? (await api.admin.staff(token)).items.filter((m) => m.status === "active") : [];
  const manage = can(admin, "cases.manage") && c.status !== "closed";
  return (
    <>
      <PageHeader
        back={{ href: "/support", label: "All cases" }}
        title={`#${c.number} ${c.subject}`}
        description={
          <>
            <StatusTag status={c.status === "resolved" || c.status === "closed" ? "paid" : "due"} label={humanize(c.status)} /> · {humanize(c.category)} · {humanize(c.priority)} priority
          </>
        }
      />
      <div className="grid gap-6 xl:grid-cols-[1fr_24rem]">
        <Panel title="History">
          <Timeline
            items={(c.events ?? []).map((e) => ({
              at: e.created_at,
              title: `${describe(e)}${e.actor_name ? ` · ${e.actor_name}` : ""}`,
              detail: e.body ? <p className="whitespace-pre-line">{e.body}</p> : undefined,
            }))}
          />
          {manage ? (
            <div className="mt-4">
              <CatalogForm action={caseNote} submit="Add note" pending="Adding…" label="Add a note">
                <input type="hidden" name="id" value={c.id} />
                <Labelled label="Note">
                  <TextArea name="body" rows={3} maxLength={2000} required />
                </Labelled>
              </CatalogForm>
            </div>
          ) : null}
        </Panel>
        <div className="grid content-start gap-6">
          <Panel title="Details">
            <DetailList
              items={[
                ["Customer", c.customer_id ? <Link key="c" href={`/customers/${c.customer_id}`} className="underline underline-offset-4">Open profile</Link> : "Not linked"],
                ["Assigned", c.assignee_name || "Nobody"],
                ["Opened", formatDateTime(c.created_at)],
                ["Resolved", c.resolved_at ? formatDateTime(c.resolved_at) : "Not yet"],
                ["Resolution", c.resolution ?? "None yet"],
              ]}
            />
          </Panel>
          {manage ? (
            <Panel title="Update">
              <CatalogForm action={updateCase} submit="Save" pending="Saving…" label="Update case">
                <input type="hidden" name="id" value={c.id} />
                <Labelled label="Status">
                  <SelectInput name="status" defaultValue={c.status}>
                    <option value={c.status}>{humanize(c.status)}</option>
                    {NEXT[c.status]!.map((s) => (
                      <option key={s} value={s}>
                        {humanize(s)}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Priority">
                  <SelectInput name="priority" defaultValue={c.priority}>
                    {["low", "normal", "high", "urgent"].map((p) => (
                      <option key={p} value={p}>
                        {humanize(p)}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Assigned to">
                  <SelectInput name="assigned_to" defaultValue={c.assigned_to ?? ""}>
                    <option value="">Nobody</option>
                    {!staff.some((m) => m.id === admin.id) ? <option value={admin.id}>Me</option> : null}
                    {staff.map((m) => (
                      <option key={m.id} value={m.id}>
                        {m.name || m.email}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Resolution" hint="Needed to resolve or close.">
                  <TextArea name="resolution" rows={3} maxLength={2000} defaultValue={c.resolution ?? ""} />
                </Labelled>
              </CatalogForm>
            </Panel>
          ) : null}
        </div>
      </div>
    </>
  );
}
