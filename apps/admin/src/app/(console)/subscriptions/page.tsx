import { buttonClasses, Heading, Tag, Text } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { DataTable, Empty, SelectInput } from "@/components/commerce-ui";
import { commerceTone, formatDate, formatDay, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Subscriptions" };

const STATUSES = ["pending_payment", "active", "pause_scheduled", "paused", "past_due", "cancel_scheduled", "cancelled", "expired"];

/** Read-only: staff inspect subscriptions; customers change their own. */
export default async function SubscriptionsPage({ searchParams }: { searchParams: Promise<{ status?: string; page?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "subscriptions.read")) notFound();
  const { status, page } = await searchParams;
  const valid = STATUSES.find((s) => s === status);
  const pageNo = Math.max(Number(page) || 1, 1);
  const { items } = await api.admin.subscriptions(token, { status: valid, page: pageNo });
  return (
    <>
      <Heading as="h1" size="h2">
        Subscriptions
      </Heading>
      <Text className="mt-3 max-w-[48rem]" tone="muted">
        Read-only. Customers pause, cancel and change plan from their account.
      </Text>
      <form className="mt-6 flex flex-wrap items-end gap-3" role="search">
        <label className="grid gap-1.5">
          <span className="type-label">Status</span>
          <SelectInput name="status" defaultValue={valid ?? ""}>
            <option value="">Any</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {humanize(s)}
              </option>
            ))}
          </SelectInput>
        </label>
        <button type="submit" className={buttonClasses("secondary")}>
          Filter
        </button>
      </form>
      <div className="mt-6">
        {items.length ? (
          <DataTable head={["Subscription", "Status", "Plan", "Price", "First week", "Started"]}>
            {items.map((s) => (
              <tr key={s.id}>
                <td className="p-3">
                  <Link href={`/subscriptions/${s.id}`} className="font-mono text-[0.8125rem] underline underline-offset-4">
                    {s.id.slice(0, 8)}
                  </Link>
                </td>
                <td className="p-3">
                  <Tag tone={commerceTone(s.status)}>{humanize(s.status)}</Tag>
                </td>
                <td className="p-3">{s.plan_name}</td>
                <td className="p-3">{money(s.amount_minor, s.currency)}</td>
                <td className="p-3">{formatDay(s.first_week)}</td>
                <td className="p-3">{s.activated_at ? formatDate(s.activated_at) : "Not yet"}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <Empty>No subscriptions match.</Empty>
        )}
      </div>
      <div className="mt-4 flex gap-4">
        {pageNo > 1 ? (
          <Link href={`?${new URLSearchParams({ ...(valid ? { status: valid } : {}), page: String(pageNo - 1) })}`} className="underline">
            Previous
          </Link>
        ) : null}
        {items.length === 50 ? (
          <Link href={`?${new URLSearchParams({ ...(valid ? { status: valid } : {}), page: String(pageNo + 1) })}`} className="underline">
            Next
          </Link>
        ) : null}
      </div>
    </>
  );
}
