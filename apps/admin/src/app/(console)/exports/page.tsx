import type { Permission } from "@mise/api-client";
import type { Metadata } from "next";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { CatalogForm } from "@/components/catalog-form";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { formatDateTime, humanize, number } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { requestExport } from "../crm-actions";

export const metadata: Metadata = { title: "Exports" };

const KINDS: { kind: string; label: string; perm: Permission; columns: string }[] = [
  { kind: "customers", label: "Customers", perm: "customers.export", columns: "customer id, name, email, account status, joined, subscription status, plan, region, source" },
  { kind: "subscriptions", label: "Subscriptions", perm: "customers.export", columns: "subscription id, customer id, status, plan, weekly price, first week, started, cancelled" },
  { kind: "orders", label: "Orders", perm: "customers.export", columns: "order number, customer id, week, status, plan, meals, total, created" },
  { kind: "payments", label: "Payments", perm: "payments.read", columns: "payment id, customer id, date, status, kind, amount, refunded, provider and reference; never card details" },
  { kind: "leads", label: "Waitlist leads", perm: "leads.export", columns: "lead id, name, email, status, location, source, campaign, signed up" },
];

const tone = (s: string) => (s === "ready" ? "paid" : s === "failed" ? "failed" : s === "expired" ? "expired" : "pending");

/**
 * Exports run in the background and are kept for 24 hours. Each file is
 * private to the person who asked for it, and every download is audited.
 * Allergies, addresses, phone numbers and notes are never exported.
 */
export default async function ExportsPage() {
  const { token, admin } = await requireAdmin();
  const allowed = KINDS.filter((k) => can(admin, k.perm));
  const { items, limit } = await api.admin.exports(token);
  return (
    <>
      <PageHeader
        title="Exports"
        description={`CSV files of business records, up to ${number.format(limit)} rows each. Files are yours alone and are deleted after 24 hours. Health details, addresses, phone numbers and notes are never included.`}
      />
      <div className="grid gap-6 xl:grid-cols-[24rem_1fr]">
        <Panel title="New export">
          {allowed.length ? (
            <CatalogForm action={requestExport} submit="Request export" pending="Requesting…" label="New export">
              <Labelled label="What to export">
                <SelectInput name="kind">
                  {allowed.map((k) => (
                    <option key={k.kind} value={k.kind}>
                      {k.label}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <div className="grid grid-cols-2 gap-3">
                <Labelled label="From">
                  <TextInput type="date" name="from" />
                </Labelled>
                <Labelled label="To">
                  <TextInput type="date" name="to" />
                </Labelled>
              </div>
              <ul className="type-caption grid gap-1 text-muted">
                {allowed.map((k) => (
                  <li key={k.kind}>
                    <strong>{k.label}:</strong> {k.columns}
                  </li>
                ))}
              </ul>
            </CatalogForm>
          ) : (
            <p className="type-body-sm text-muted">Your role does not include any exports.</p>
          )}
        </Panel>
        <Panel title="Your exports" note="Refresh to see progress.">
          {items.length ? (
            <DataTable head={["Requested", "Kind", "Status", "Rows", "Expires", ""]} minWidth="40rem">
              {items.map((e) => (
                <tr key={e.id}>
                  <td className="p-3">{formatDateTime(e.created_at)}</td>
                  <td className="p-3">{humanize(e.kind)}</td>
                  <td className="p-3">
                    <StatusTag status={tone(e.status)} label={humanize(e.status)} />
                    {e.error ? <span className="type-caption block text-error">{e.error}</span> : null}
                  </td>
                  <td className="p-3">
                    {e.row_count === null ? "" : number.format(e.row_count)}
                    {e.truncated ? <span className="type-caption block text-warning">Cut at the row limit</span> : null}
                  </td>
                  <td className="p-3">{formatDateTime(e.expires_at)}</td>
                  <td className="p-3">
                    {e.status === "ready" ? (
                      <a href={`/exports/${e.id}`} className="font-semibold underline underline-offset-4">
                        Download
                      </a>
                    ) : null}
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>You have not requested any exports.</EmptyState>
          )}
        </Panel>
      </div>
    </>
  );
}
