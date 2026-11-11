import { ApiError, type Quote } from "@mise/api-client";
import { buttonClasses, Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { AmountTable, Steps, TestModeNotice } from "@/components/commerce-bits";
import { Form, FormMessage, SubmitButton } from "@/components/forms";
import { deadline, formatMoney } from "@/lib/commerce";
import { api, requireCustomer } from "@/lib/session";

import { startSubscription } from "../actions";

export const metadata: Metadata = { title: "Review your subscription" };

/**
 * Step 2: everything that will be charged, before any payment. Every amount
 * is the API's quote; this page does no arithmetic of its own.
 */
export default async function ReviewPage({ searchParams }: { searchParams: Promise<{ plan?: string; address?: string; code?: string }> }) {
  const { token } = await requireCustomer();
  const { plan: slug, address, code } = await searchParams;
  if (!slug || !address) redirect(slug ? `/subscribe?plan=${encodeURIComponent(slug)}` : "/plans");
  const [plan, addresses] = await Promise.all([api.shop.plan(slug).catch(() => null), api.account.addresses(token)]);
  if (!plan) redirect("/plans");
  const addr = addresses.items.find((a) => a.id === address);
  if (!addr) redirect(`/subscribe?plan=${encodeURIComponent(slug)}`);

  let quote: Quote | null = null;
  let problem: string | null = null;
  try {
    quote = await api.commerce.quote(token, plan.id, code?.trim() || undefined);
  } catch (e) {
    if (e instanceof ApiError) problem = e.fields.find((f) => f.field === "promotion_code")?.message ?? e.message;
    else throw e;
  }
  const back = `/subscribe?plan=${encodeURIComponent(slug)}`;

  return (
    <Container className="py-10 md:py-14">
      <Steps current={2} />
      <h1 className="type-h1 mt-6">Review</h1>
      {problem || !quote ? (
        <div className="mt-8 max-w-[40rem]">
          <p role="alert" className="type-body border-l-4 border-error pl-4">
            {problem ?? "We couldn't price this plan. Please try again."}
          </p>
          <Link href={back} className={buttonClasses("secondary", "md", "mt-6")}>
            Go back
          </Link>
        </div>
      ) : (
        <div className="mt-8 grid gap-10 lg:grid-cols-[1fr_26rem] lg:gap-16">
          <div className="grid content-start gap-8">
            <section aria-labelledby="first-week">
              <h2 id="first-week" className="type-h3">
                Your first week
              </h2>
              <p className="type-body mt-3">
                <strong>{quote.first_week_label}</strong>. Choose your {quote.plan.meals_per_week} meals by{" "}
                <strong>{deadline(quote.first_week_cutoff_at)}</strong> (Trinidad and Tobago time). The menu opens for choosing as soon
                as it&apos;s published.
              </p>
            </section>
            <section aria-labelledby="deliver-to">
              <h2 id="deliver-to" className="type-h3">
                Delivered to
              </h2>
              <address className="type-body mt-3 not-italic">
                {[addr.recipient_name, addr.line1, addr.line2, addr.area, addr.city, addr.region_label].filter(Boolean).join(", ")}
              </address>
              <Link href={back} className="type-body-sm mt-2 inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
                Change
              </Link>
            </section>
            <section aria-labelledby="terms">
              <h2 id="terms" className="type-h3">
                What happens next
              </h2>
              <p className="type-body mt-3 max-w-[40rem]">{quote.terms}</p>
              <p className="type-body-sm mt-3 text-muted">{quote.tax_note}</p>
            </section>
          </div>

          <aside aria-labelledby="pay-now" className="h-fit border border-border bg-surface p-6 lg:sticky lg:top-24">
            <h2 id="pay-now" className="type-h3">
              {quote.plan.name} plan
            </h2>
            {quote.promotion ? <p className="type-body-sm mt-1 font-semibold text-secondary">{quote.promotion}</p> : null}
            <div className="mt-5">
              <AmountTable amounts={quote.due_now} caption="Due now" totalLabel="Pay now" />
            </div>
            <p className="type-body-sm mt-4 text-muted">
              Then {formatMoney(quote.renewal.total_minor, quote.renewal.currency)} each week until you pause or cancel.
            </p>
            {quote.payments.test ? <TestModeNotice className="mt-5" /> : null}
            <Form action={startSubscription} className="mt-6">
              <input type="hidden" name="plan_id" value={quote.plan.id} />
              <input type="hidden" name="address_id" value={addr.id} />
              <input type="hidden" name="promotion_code" value={code?.trim() ?? ""} />
              <FormMessage />
              <SubmitButton pending="Opening secure checkout…" className="w-full">
                Continue to payment
              </SubmitButton>
              <p className="type-caption text-muted">
                You&apos;ll pay on the payment provider&apos;s page. Mise never sees or stores your card number.
              </p>
            </Form>
          </aside>
        </div>
      )}
    </Container>
  );
}
