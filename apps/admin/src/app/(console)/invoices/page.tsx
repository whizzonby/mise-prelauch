import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, Pager, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, SelectInput, TextInput } from "@/components/commerce-ui";
import { formatDate, formatDay, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Invoices" };

export default async function InvoicesPage({ searchParams }: { searchParams: Promise<{ status?: string; q?: string; page?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "payments.read")) notFound();
  const sp = await searchParams;
  const page = Math.max(Number(sp.page) || 1, 1);
  const list = await api.admin.invoices(token, { status: sp.status, q: sp.q, page });
  return (
    <>
      <PageHeader title="Invoices" description="Invoices issued for paid weeks. Amounts are what was billed; tax and delivery fees are not configured yet, so both are zero." />
      <FilterBar>
        <Labelled label="Invoice number or customer email">
          <TextInput name="q" defaultValue={sp.q} maxLength={100} className="w-64" />
        </Labelled>
        <Labelled label="Status">
          <SelectInput name="status" defaultValue={sp.status ?? ""}>
            <option value="">Any</option>
            {["open", "paid", "void"].map((s) => (
              <option key={s} value={s}>
                {humanize(s)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
      </FilterBar>
      {list.items.length ? (
        <DataTable head={["Invoice", "Customer", "Week", "Status", "Total", "Issued"]}>
          {list.items.map((i) => (
            <tr key={i.id}>
              <td className="p-3 font-semibold">{i.number}</td>
              <td className="p-3">
                {can(admin, "customers.read") ? (
                  <Link href={`/customers/${i.customer_id}?tab=billing`} className="underline underline-offset-4">
                    {i.customer}
                  </Link>
                ) : (
                  i.customer
                )}
              </td>
              <td className="p-3">{formatDay(i.period_start)}</td>
              <td className="p-3">
                <StatusTag status={i.status} />
              </td>
              <td className="p-3">{money(i.total_minor, i.currency)}</td>
              <td className="p-3">{formatDate(i.issued_at)}</td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No invoices match.</EmptyState>
      )}
      <Pager page={page} size={50} total={list.total} params={{ status: sp.status, q: sp.q }} />
    </>
  );
}
