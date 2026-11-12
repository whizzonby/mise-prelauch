import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, Pager } from "@/components/admin-ui";
import { DataTable, Labelled, TextInput } from "@/components/commerce-ui";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Audit log" };

type Search = { action?: string; target_type?: string; target_id?: string; from?: string; to?: string; page?: string };

/**
 * The audit log: append-only, kept by the database. Metadata never holds
 * passwords, tokens, card details or note text, so it is safe to show.
 */
export default async function AuditPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "audit.read")) notFound();
  const sp = await searchParams;
  const page = Math.max(Number(sp.page) || 1, 1);
  const { items, total } = await api.admin.audit(token, { ...sp, page });
  return (
    <>
      <PageHeader title="Audit log" description="Who did what, and when. Entries cannot be edited or deleted; the only exception is a waitlist lead's own entries when that lead is erased on request." />
      <FilterBar>
        <Labelled label="Action starts with" hint="For example: subscription. or staff.">
          <TextInput name="action" defaultValue={sp.action} maxLength={80} className="w-52" />
        </Labelled>
        <Labelled label="Record type">
          <TextInput name="target_type" defaultValue={sp.target_type} maxLength={40} className="w-40" />
        </Labelled>
        <Labelled label="Record id">
          <TextInput name="target_id" defaultValue={sp.target_id} maxLength={80} className="w-72" />
        </Labelled>
        <Labelled label="From">
          <TextInput type="date" name="from" defaultValue={sp.from} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={sp.to} />
        </Labelled>
      </FilterBar>
      {items.length ? (
        <DataTable head={["When", "Who", "Action", "Record", "Reason", "Details"]} minWidth="64rem">
          {items.map((e) => (
            <tr key={e.id} className="align-top">
              <td className="whitespace-nowrap p-3">{formatDateTime(e.at)}</td>
              <td className="p-3">{e.actor_name ?? (e.actor_id ? "Removed user" : "Customer or system")}</td>
              <td className="p-3">
                <code className="text-[0.8125rem]">{e.action}</code>
              </td>
              <td className="p-3">
                {e.target_type ? (
                  <>
                    {e.target_type}
                    <span className="type-caption block break-all text-muted">{e.target_id}</span>
                  </>
                ) : null}
              </td>
              <td className="p-3">{e.reason ?? ""}</td>
              <td className="p-3">
                {e.metadata && Object.keys(e.metadata).length ? (
                  <details>
                    <summary className="type-caption cursor-pointer underline underline-offset-2">Show</summary>
                    <pre className="mt-1 max-w-[28rem] overflow-x-auto whitespace-pre-wrap text-[0.75rem]">{JSON.stringify(e.metadata, null, 2)}</pre>
                  </details>
                ) : null}
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No entries match.</EmptyState>
      )}
      <Pager page={page} size={50} total={total} params={{ ...sp, page: undefined }} />
    </>
  );
}
