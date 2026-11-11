import { buttonClasses, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { TestModeNotice } from "@/components/commerce-bits";
import { Form, FormMessage, SubmitButton } from "@/components/forms";
import { EmptyState } from "@/components/meal-card";
import { deadline, formatMoney, shortDate } from "@/lib/commerce";
import { api, requireCustomer } from "@/lib/session";

import { payWeek } from "../subscribe/actions";

export const metadata: Metadata = { title: "Billing" };

const statusLabel: Record<string, string> = { paid: "Paid", due: "Due", failed: "Failed", void: "Not charged", pending: "Pending" };
const statusTone = (s: string) => (s === "paid" ? "success" : s === "failed" ? "error" : s === "due" ? "accent" : "neutral") as "success" | "error" | "accent" | "neutral";

export default async function BillingPage() {
  const { token } = await requireCustomer();
  const billing = await api.commerce.billing(token);
  return (
    <>
      <h1 className="type-h2">Billing</h1>
      {billing.test_payments ? <TestModeNotice className="mt-6 max-w-[44rem]" /> : null}
      <section aria-labelledby="method" className="mt-8 max-w-[44rem]">
        <h2 id="method" className="type-h4">
          Payment method
        </h2>
        <p className="type-body-sm mt-2 text-muted">
          {billing.payment_method
            ? `${billing.payment_method}${billing.auto_renew ? ". Each week is charged automatically before its menu closes." : "."}`
            : "None saved. Each week can be paid from your subscription page."}{" "}
          Card details are held by the payment provider, never by Mise.
        </p>
      </section>

      {billing.outstanding?.billing ? (
        <aside className="mt-8 max-w-[44rem] border border-error bg-surface p-5">
          <h2 className="type-h4">Payment needed for {billing.outstanding.week_label}</h2>
          <p className="type-body-sm mt-1 text-muted">
            {formatMoney(billing.outstanding.billing.total_minor, billing.outstanding.billing.currency)}, by {deadline(billing.outstanding.cutoff_at)}.
          </p>
          <Form action={payWeek} className="mt-4">
            <input type="hidden" name="billing_cycle_id" value={billing.outstanding.billing.id} />
            <FormMessage />
            <SubmitButton pending="Opening checkout…">Pay now</SubmitButton>
          </Form>
        </aside>
      ) : null}

      <section aria-labelledby="history" className="mt-10">
        <h2 id="history" className="type-h3">
          History
        </h2>
        {billing.history.length ? (
          <div className="mt-4 overflow-x-auto">
            <table className="type-body-sm type-figures w-full min-w-[36rem]">
              <thead>
                <tr className="border-b border-foreground text-left">
                  <th scope="col" className="py-2 font-semibold">Week</th>
                  <th scope="col" className="py-2 font-semibold">Status</th>
                  <th scope="col" className="py-2 text-right font-semibold">Amount</th>
                  <th scope="col" className="py-2 text-right font-semibold">Invoice</th>
                </tr>
              </thead>
              <tbody>
                {billing.history.map((b) => (
                  <tr key={b.id} className="border-b border-border">
                    <td className="py-3">{shortDate(b.period_start)}</td>
                    <td className="py-3">
                      <Tag tone={statusTone(b.status)}>{statusLabel[b.status] ?? b.status}</Tag>
                    </td>
                    <td className="py-3 text-right">{formatMoney(b.total_minor, b.currency)}</td>
                    <td className="py-3 text-right">
                      {b.invoice_id ? (
                        <Link href={`/billing/invoices/${b.invoice_id}`} className="font-semibold underline underline-offset-4">
                          {b.invoice_number}
                        </Link>
                      ) : (
                        <span className="text-muted">None</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState
            title="No payments yet."
            action={
              <Link href="/plans" className={buttonClasses("primary")}>
                See plans
              </Link>
            }
          >
            Your payments and invoices will be listed here.
          </EmptyState>
        )}
      </section>
      {billing.terms ? <p className="type-caption mt-8 max-w-[44rem] text-muted">{billing.terms}</p> : null}
    </>
  );
}
