import type { Subscription, SubscriptionCycle } from "@mise/api-client";
import { buttonClasses, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { PaymentChoice, TestModeNotice } from "@/components/commerce-bits";
import { Form, FormMessage, SubmitButton } from "@/components/forms";
import { EmptyState } from "@/components/meal-card";
import { cycleTone, deadline, formatMoney, selectHref, statusTone } from "@/lib/commerce";
import { api, requireCustomer } from "@/lib/session";

import { payWeek } from "../subscribe/actions";
import { resume } from "./actions";

export const metadata: Metadata = { title: "Your subscription" };

export default async function SubscriptionPage({ searchParams }: { searchParams: Promise<{ updated?: string }> }) {
  const { token } = await requireCustomer();
  const [{ subscription: sub }, { updated }] = await Promise.all([api.commerce.subscription(token), searchParams]);

  if (!sub) {
    return (
      <EmptyState
        title="You don't have a subscription yet."
        action={
          <Link href="/plans" className={buttonClasses("primary")}>
            See plans
          </Link>
        }
      >
        Choose how many meals you want each week, then pick them from the weekly menu.
      </EmptyState>
    );
  }

  return (
    <>
      {updated ? (
        <p role="status" className="type-body-sm mb-6 border-l-2 border-success pl-3">
          Your subscription was updated.
        </p>
      ) : null}
      <div className="flex flex-wrap items-start justify-between gap-6">
        <div>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="type-h2">{sub.plan.name} plan</h1>
            <Tag tone={statusTone(sub.status)}>{sub.status_label}</Tag>
          </div>
          <p className="type-body-lg mt-3 text-muted">
            {sub.plan.meals_per_week} meal kits a week · {sub.price_formatted} a week
          </p>
        </div>
        {sub.actions.change_plan ? (
          <Link href="/subscription/change-plan" className={buttonClasses("secondary")}>
            Change plan
          </Link>
        ) : null}
      </div>
      {sub.test_payments ? <TestModeNotice className="mt-6 max-w-[44rem]" /> : null}

      <StatusNotice sub={sub} />

      {sub.outstanding ? <PayNow week={sub.outstanding} pending={sub.status === "pending_payment"} /> : null}

      <section aria-labelledby="weeks" className="mt-12">
        <h2 id="weeks" className="type-h3">
          Your weeks
        </h2>
        {sub.cycles.length ? (
          <ul className="mt-4 divide-y divide-border border-y border-border">
            {sub.cycles.map((c) => (
              <WeekRow key={c.id} c={c} />
            ))}
          </ul>
        ) : (
          <p className="type-body mt-3 text-muted">Your first week appears here once your payment is confirmed.</p>
        )}
      </section>

      <section aria-labelledby="details" className="mt-12 grid gap-8 md:grid-cols-2">
        <div>
          <h2 id="details" className="type-h4">
            Delivery address
          </h2>
          {sub.address ? (
            <address className="type-body-sm mt-2 not-italic text-muted">
              {[sub.address.recipient_name, sub.address.line1, sub.address.line2, sub.address.area, sub.address.city, sub.address.region_label]
                .filter(Boolean)
                .join(", ")}
            </address>
          ) : (
            <p className="type-body-sm mt-2 font-semibold text-error">No delivery address. Choose one so we can deliver.</p>
          )}
          {sub.actions.change_address ? (
            <Link href="/subscription/manage" className="type-body-sm mt-2 inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Change address
            </Link>
          ) : null}
        </div>
        <div>
          <h2 className="type-h4">Payment</h2>
          <p className="type-body-sm mt-2 text-muted">
            {sub.payment_method
              ? `${sub.payment_method}${sub.auto_renew ? ", charged automatically each week." : "."}`
              : "No saved payment method. You'll pay each week from this page."}
          </p>
          <Link href="/billing" className="type-body-sm mt-2 inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
            Billing and invoices
          </Link>
        </div>
      </section>

      <section aria-labelledby="terms" className="mt-12 max-w-[44rem] border-t border-border pt-6">
        <h2 id="terms" className="type-h4">
          How billing works
        </h2>
        <p className="type-body-sm mt-2 text-muted">{sub.terms}</p>
        <div className="mt-6 flex flex-wrap gap-x-6 gap-y-2">
          {sub.actions.pause || sub.actions.resume ? (
            <Link href="/subscription/pause" className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              {sub.actions.resume ? "Resume or change pause" : "Pause deliveries"}
            </Link>
          ) : null}
          {sub.actions.cancel ? (
            <Link href="/subscription/cancel" className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Cancel subscription
            </Link>
          ) : null}
        </div>
      </section>
    </>
  );
}

function StatusNotice({ sub }: { sub: Subscription }) {
  let body: React.ReactNode = null;
  if (sub.status === "pending_payment") body = "Your subscription starts once your first payment is confirmed.";
  if (sub.status === "past_due") body = "A weekly payment didn't go through. Pay before that week's menu closes to have it delivered.";
  if (sub.pause) body = `Paused ${sub.pause.label}. No deliveries or charges in those weeks.`;
  if (sub.cancel) body = `Cancelled. ${sub.cancel.label}. Weeks you've already paid for are still delivered.`;
  if (!body) return null;
  return (
    <div className="mt-6 flex max-w-[44rem] flex-col gap-3 border-l-4 border-accent bg-surface p-4 sm:flex-row sm:items-center sm:justify-between">
      <p className="type-body-sm">{body}</p>
      {sub.actions.resume || sub.actions.undo_cancel ? (
        <Form action={resume} className="shrink-0">
          <input type="hidden" name="subscription_id" value={sub.id} />
          <FormMessage />
          <SubmitButton pending="Updating…" variant="secondary">
            {sub.actions.undo_cancel ? "Keep my subscription" : "Resume now"}
          </SubmitButton>
        </Form>
      ) : null}
    </div>
  );
}

async function PayNow({ week, pending }: { week: SubscriptionCycle; pending: boolean }) {
  if (!week.billing) return null;
  const options = (await api.commerce.paymentOptions(week.billing.currency).catch(() => null))?.items ?? [];
  return (
    <aside aria-labelledby="pay-now" className="mt-8 max-w-[44rem] border border-error bg-surface p-5">
      <h2 id="pay-now" className="type-h4">
        {pending ? "Finish your first payment" : `Payment needed for ${week.week_label}`}
      </h2>
      <p className="type-body-sm mt-1 text-muted">
        {formatMoney(week.billing.total_minor, week.billing.currency)}, by {deadline(week.cutoff_at)}.
        {week.billing.status === "failed" ? " The last attempt didn't go through." : ""}
      </p>
      <Form action={payWeek} className="mt-4">
        <input type="hidden" name="billing_cycle_id" value={week.billing.id} />
        <PaymentChoice options={options} className="mb-4" />
        <FormMessage />
        <SubmitButton pending="Opening checkout…">Pay now</SubmitButton>
      </Form>
    </aside>
  );
}

function WeekRow({ c }: { c: SubscriptionCycle }) {
  return (
    <li className="grid gap-3 py-4 sm:grid-cols-[1fr_auto] sm:items-center">
      <div>
        <p className="type-h4 flex flex-wrap items-center gap-2">
          {c.week_label}
          <Tag tone={cycleTone(c)}>{c.state_label}</Tag>
        </p>
        <p className="type-caption mt-1 text-muted">
          {c.plan.name} · {c.plan.meals_per_week} meals ·{" "}
          {c.state === "scheduled" ? `choose by ${deadline(c.cutoff_at)}` : formatMoney(c.amount_minor, c.currency)}
          {c.selection_status === "confirmed" ? " · meals confirmed" : c.selected_count ? ` · ${c.selected_count} chosen, not confirmed` : ""}
        </p>
      </div>
      <div className="flex gap-4">
        {c.selection_open ? (
          <Link href={selectHref(c.week)} className={buttonClasses(c.selection_status === "confirmed" ? "secondary" : "primary")}>
            {c.selection_status === "confirmed" ? "Change meals" : "Choose meals"}
          </Link>
        ) : c.state === "scheduled" && !c.menu_published ? (
          <span className="type-caption text-muted">Menu coming soon</span>
        ) : null}
        {c.billing?.invoice_id ? (
          <Link href={`/billing/invoices/${c.billing.invoice_id}`} className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
            Invoice
          </Link>
        ) : null}
      </div>
    </li>
  );
}
