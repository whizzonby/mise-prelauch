import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, Pager, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, SelectInput, TextInput } from "@/components/commerce-ui";
import { formatDate, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Customers" };

const SUB_STATUSES = ["active", "pause_scheduled", "paused", "past_due", "cancel_scheduled", "cancelled", "expired", "pending_payment", "none"];
const FLAG_LABELS: Record<string, string> = { payment_failed: "Payment failed", open_case: "Open case", payment_review: "Payment review" };

type Search = { q?: string; status?: string; subscription_status?: string; plan_id?: string; segment_id?: string; sort?: string; page?: string };

/** Every customer, filtered and paged on the server. */
export default async function CustomersPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "customers.read")) notFound();
  const sp = await searchParams;
  const page = Math.max(Number(sp.page) || 1, 1);
  const [list, plans, segments] = await Promise.all([
    api.admin.customers(token, { ...sp, page, page_size: 25 }),
    can(admin, "subscriptions.read") ? api.admin.plans(token) : Promise.resolve({ items: [] }),
    can(admin, "campaigns.manage") ? api.admin.segments(token) : Promise.resolve({ items: [] }),
  ]);
  return (
    <>
      <PageHeader title="Customers" description="Customer accounts and their current subscription. Sensitive details (allergies, addresses) are in each profile, for staff allowed to see them." />
      <FilterBar>
        <Labelled label="Name or email">
          <TextInput name="q" defaultValue={sp.q} maxLength={100} className="w-56" />
        </Labelled>
        <Labelled label="Account">
          <SelectInput name="status" defaultValue={sp.status ?? ""}>
            <option value="">Any</option>
            <option value="verified">Verified</option>
            <option value="unverified">Email not confirmed</option>
            <option value="disabled">Disabled</option>
          </SelectInput>
        </Labelled>
        <Labelled label="Subscription">
          <SelectInput name="subscription_status" defaultValue={sp.subscription_status ?? ""}>
            <option value="">Any</option>
            {SUB_STATUSES.map((s) => (
              <option key={s} value={s}>
                {s === "none" ? "Never subscribed" : humanize(s)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        {plans.items.length ? (
          <Labelled label="Plan">
            <SelectInput name="plan_id" defaultValue={sp.plan_id ?? ""}>
              <option value="">Any</option>
              {plans.items.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </SelectInput>
          </Labelled>
        ) : null}
        {segments.items.length ? (
          <Labelled label="Segment">
            <SelectInput name="segment_id" defaultValue={sp.segment_id ?? ""}>
              <option value="">None</option>
              {segments.items.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </SelectInput>
          </Labelled>
        ) : null}
        <Labelled label="Sort">
          <SelectInput name="sort" defaultValue={sp.sort ?? ""}>
            <option value="">Newest first</option>
            <option value="name">Name</option>
            <option value="last_login">Last sign-in</option>
          </SelectInput>
        </Labelled>
      </FilterBar>

      {list.items.length ? (
        <DataTable head={["Customer", "Account", "Subscription", "Plan", "Source", "Joined", "Last sign-in", "Flags"]} minWidth="60rem">
          {list.items.map((c) => (
            <tr key={c.id}>
              <td className="p-3">
                <Link href={`/customers/${c.id}`} className="font-semibold underline underline-offset-4">
                  {c.name || c.email}
                </Link>
                <span className="type-caption block text-muted">{c.email}</span>
              </td>
              <td className="p-3">
                <StatusTag status={c.status === "verified" ? "active" : c.status} label={humanize(c.status)} />
              </td>
              <td className="p-3">{c.subscription_status ? <StatusTag status={c.subscription_status} /> : <span className="text-muted">None</span>}</td>
              <td className="p-3">{c.plan_name ?? ""}</td>
              <td className="p-3">{humanize(c.source)}</td>
              <td className="p-3">{formatDate(c.joined_at)}</td>
              <td className="p-3">{c.last_login_at ? formatDate(c.last_login_at) : "Never"}</td>
              <td className="p-3">
                <span className="flex flex-wrap gap-1">
                  {c.flags.map((f) => (
                    <Tag key={f} tone={f === "open_case" ? "warning" : "error"}>
                      {FLAG_LABELS[f] ?? f}
                    </Tag>
                  ))}
                </span>
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No customers match.</EmptyState>
      )}
      <Pager page={list.page} size={list.page_size} total={list.total} params={{ ...sp, page: undefined }} />
    </>
  );
}
