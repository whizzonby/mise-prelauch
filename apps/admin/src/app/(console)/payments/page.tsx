import { buttonClasses, Heading, Tag, Text } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { DataTable, Empty, Panel } from "@/components/commerce-ui";
import { commerceTone, formatDateTime, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Payments" };

/**
 * Payments as the provider reported them, payments that need a person to
 * look at them, webhooks received, and background jobs that gave up. There
 * is deliberately no way to set a payment's status by hand.
 */
export default async function PaymentsPage({ searchParams }: { searchParams: Promise<{ attention?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "payments.read")) notFound();
  const { attention } = await searchParams;
  const [payments, webhooks, jobs] = await Promise.all([
    api.admin.payments(token, { attention: attention === "true" ? true : undefined }),
    api.admin.webhooks(token),
    api.admin.failedJobs(token),
  ]);
  return (
    <>
      <Heading as="h1" size="h2">
        Payments
      </Heading>
      <Text className="mt-3 max-w-[48rem]" tone="muted">
        Payments as each provider reported them. Payments marked test moved no real money. Which providers are offered, and in which
        mode, is set on the payment providers page.
      </Text>
      <div className="mt-6 flex gap-3">
        <Link href="/payments" className={buttonClasses(attention ? "secondary" : "primary")}>
          All
        </Link>
        <Link href="/payments?attention=true" className={buttonClasses(attention ? "primary" : "secondary")}>
          Needs attention
        </Link>
      </div>
      <div className="mt-6">
        {payments.items.length ? (
          <DataTable head={["Payment", "Status", "Kind", "Amount", "Refunded", "When"]}>
            {payments.items.map((p) => (
              <tr key={p.id}>
                <td className="p-3">
                  <Link href={`/payments/${p.id}`} className="font-mono text-[0.8125rem] underline underline-offset-4">
                    {p.id.slice(0, 8)}
                  </Link>
                  {p.test ? <span className="type-caption ml-2 text-muted">test</span> : null}
                  {p.needs_attention ? (
                    <span className="ml-2">
                      <Tag tone="error">Needs attention</Tag>
                    </span>
                  ) : null}
                </td>
                <td className="p-3">
                  <Tag tone={commerceTone(p.status)}>{humanize(p.status)}</Tag>
                </td>
                <td className="p-3">{p.kind === "checkout" ? "Checkout" : "Automatic"}</td>
                <td className="p-3">{money(p.amount_minor, p.currency)}</td>
                <td className="p-3">{p.refunded_minor ? money(p.refunded_minor, p.currency) : ""}</td>
                <td className="p-3">{formatDateTime(p.created_at)}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <Empty>No payments.</Empty>
        )}
      </div>

      <Panel title="Webhooks received" className="mt-10">
        {webhooks.items.length ? (
          <DataTable head={["Received", "Provider", "Type", "Status", "Attempts"]}>
            {webhooks.items.slice(0, 30).map((w) => (
              <tr key={w.id}>
                <td className="p-3">{formatDateTime(w.received_at)}</td>
                <td className="p-3">{w.provider}</td>
                <td className="p-3">{w.event_type}</td>
                <td className="p-3">
                  <Tag tone={commerceTone(w.status)}>{humanize(w.status)}</Tag>
                  {w.error ? <span className="type-caption block text-error">{w.error}</span> : null}
                </td>
                <td className="p-3">{w.attempts}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <Empty>None yet.</Empty>
        )}
      </Panel>

      <Panel title="Failed or retrying jobs" className="mt-8">
        {jobs.items.length ? (
          <DataTable head={["Job", "Status", "Attempts", "Last error"]}>
            {jobs.items.map((j) => (
              <tr key={j.id}>
                <td className="p-3">{j.kind}</td>
                <td className="p-3">{humanize(j.status)}</td>
                <td className="p-3">{j.attempts}</td>
                <td className="p-3">{j.last_error}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <Empty>Nothing has failed.</Empty>
        )}
      </Panel>
    </>
  );
}
