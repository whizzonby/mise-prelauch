import { buttonClasses } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, Pager, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, SelectInput } from "@/components/commerce-ui";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Support cases" };

const STATUSES = ["open", "in_progress", "waiting_for_customer", "resolved", "closed"];

export default async function SupportPage({ searchParams }: { searchParams: Promise<{ status?: string; open?: string; assigned_to?: string; priority?: string; page?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "cases.read")) notFound();
  const sp = await searchParams;
  const page = Math.max(Number(sp.page) || 1, 1);
  const list = await api.admin.cases(token, { status: sp.status, open: sp.open === "true" || undefined, assigned_to: sp.assigned_to, priority: sp.priority, page });
  return (
    <>
      <PageHeader
        title="Support cases"
        description="Internal case list. Customers are contacted outside the console; record what was said as notes."
        actions={
          can(admin, "cases.manage") ? (
            <Link href="/support/new" className={buttonClasses("primary")}>
              New case
            </Link>
          ) : null
        }
      />
      <FilterBar>
        <Labelled label="Status">
          <SelectInput name="status" defaultValue={sp.status ?? ""}>
            <option value="">Any</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {humanize(s)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <Labelled label="Show">
          <SelectInput name="open" defaultValue={sp.open ?? ""}>
            <option value="">All cases</option>
            <option value="true">Not yet resolved</option>
          </SelectInput>
        </Labelled>
        <Labelled label="Assigned">
          <SelectInput name="assigned_to" defaultValue={sp.assigned_to ?? ""}>
            <option value="">Anyone</option>
            <option value="me">Me</option>
          </SelectInput>
        </Labelled>
        <Labelled label="Priority">
          <SelectInput name="priority" defaultValue={sp.priority ?? ""}>
            <option value="">Any</option>
            {["urgent", "high", "normal", "low"].map((p) => (
              <option key={p} value={p}>
                {humanize(p)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
      </FilterBar>
      {list.items.length ? (
        <DataTable head={["Case", "Subject", "Category", "Priority", "Status", "Assigned", "Updated"]} minWidth="56rem">
          {list.items.map((c) => (
            <tr key={c.id}>
              <td className="p-3">
                <Link href={`/support/${c.id}`} className="font-semibold underline underline-offset-4">
                  #{c.number}
                </Link>
              </td>
              <td className="p-3">{c.subject}</td>
              <td className="p-3">{humanize(c.category)}</td>
              <td className="p-3">
                <StatusTag status={c.priority === "urgent" || c.priority === "high" ? "failed" : "neutral"} label={humanize(c.priority)} />
              </td>
              <td className="p-3">
                <StatusTag status={c.status === "resolved" || c.status === "closed" ? "paid" : c.status === "open" ? "due" : "pending"} label={humanize(c.status)} />
              </td>
              <td className="p-3">{c.assignee_name || "Nobody"}</td>
              <td className="p-3">{formatDateTime(c.updated_at)}</td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No cases match.</EmptyState>
      )}
      <Pager page={list.page} size={50} total={list.total} params={{ ...sp, page: undefined }} />
    </>
  );
}
