import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader } from "@/components/admin-ui";
import { DataTable, Panel } from "@/components/commerce-ui";
import { formatDate, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Retention" };

/**
 * Customers showing a sign that they may leave, each from a stated rule.
 * No churn score: the rules are listed so anyone can check a row.
 */
export default async function RetentionPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "customers.read")) notFound();
  const { items, rules } = await api.admin.retention(token);
  return (
    <>
      <PageHeader title="Retention" description="Customers who may need a follow-up. Each row comes from one of the rules below; there is no predictive score." />
      {items.length ? (
        <DataTable head={["Customer", "Signal", "Detail", "Since"]} minWidth="48rem">
          {items.map((s, i) => (
            <tr key={`${s.customer_id}-${s.signal}-${i}`}>
              <td className="p-3">
                <Link href={`/customers/${s.customer_id}`} className="font-semibold underline underline-offset-4">
                  {s.name || s.email}
                </Link>
                <span className="type-caption block text-muted">{s.email}</span>
              </td>
              <td className="p-3">
                <Tag tone={s.signal === "failed_renewal" ? "error" : "warning"}>{humanize(s.signal)}</Tag>
              </td>
              <td className="p-3">{s.detail}</td>
              <td className="p-3">{formatDate(s.since)}</td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No customers match a retention rule right now.</EmptyState>
      )}
      <Panel title="The rules" className="mt-6">
        <dl className="type-body-sm grid gap-3">
          {Object.entries(rules).map(([k, v]) => (
            <div key={k}>
              <dt className="font-semibold">{humanize(k)}</dt>
              <dd className="text-muted">{v}</dd>
            </div>
          ))}
        </dl>
      </Panel>
    </>
  );
}
