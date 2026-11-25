import { ApiError } from "@mise/api-client";
import { buttonClasses, Container, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { Steps, TestModeNotice } from "@/components/commerce-bits";
import { formatMoney } from "@/lib/commerce";
import { availabilityText } from "@/lib/format";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Subscribe" };

/**
 * Step 1 of subscribing: confirm the plan, choose where it is delivered,
 * and add a code if you have one. A plain GET form, so the next step has a
 * URL and works without JavaScript.
 */
export default async function SubscribePage({ searchParams }: { searchParams: Promise<{ plan?: string; error?: string }> }) {
  const { token } = await requireCustomer();
  const { plan: slug } = await searchParams;
  if (!slug) redirect("/plans");
  const [plan, addresses, { subscription }, info] = await Promise.all([
    api.shop.plan(slug).catch((e) => {
      if (e instanceof ApiError && e.status === 404) return null;
      throw e;
    }),
    api.account.addresses(token),
    api.commerce.subscription(token),
    api.shop.info(),
  ]);
  if (!plan) redirect("/plans");
  if (subscription && subscription.status !== "pending_payment") redirect("/subscription");

  const usable = addresses.items.filter((a) => a.delivery_availability === "available");
  const fallback = usable.find((a) => a.is_default) ?? usable[0];

  return (
    <Container className="py-10 md:py-14">
      <Steps current={1} />
      <h1 className="type-h1 mt-6">Your {plan.name} plan</h1>
      <p className="type-body-lg mt-3 text-muted">
        {plan.meals_per_week} meal kits a week{plan.price ? `, ${formatMoney(plan.price.amount_minor, plan.price.currency)} a week` : ""}.{" "}
        <Link href="/plans" className="font-semibold text-foreground underline underline-offset-4">
          Change
        </Link>
      </p>
      {info.payments.test ? <TestModeNotice className="mt-6 max-w-[44rem]" /> : null}

      {!plan.purchasable ? (
        <p className="type-body mt-8 border-l-4 border-error pl-4">{plan.unavailable_reason}</p>
      ) : !info.payments.available ? (
        <p className="type-body mt-8 border-l-4 border-warning pl-4">Online payment is not available yet. Please try again later.</p>
      ) : (
        <form action="/subscribe/review" method="get" className="mt-10 grid max-w-[44rem] gap-10">
          <input type="hidden" name="plan" value={plan.slug} />
          <fieldset>
            <legend className="type-h3">Where should we deliver?</legend>
            {addresses.items.length ? (
              <div className="mt-5 grid gap-3">
                {addresses.items.map((a) => {
                  const ok = a.delivery_availability === "available";
                  return (
                    <label
                      key={a.id}
                      className={`flex gap-4 border bg-surface p-4 ${ok ? "cursor-pointer border-border has-[:checked]:border-primary has-[:checked]:ring-1 has-[:checked]:ring-primary" : "border-border opacity-70"}`}
                    >
                      <input type="radio" name="address" value={a.id} defaultChecked={a.id === fallback?.id} disabled={!ok} required className="mt-1 size-5 accent-[var(--color-primary)]" />
                      <span>
                        <span className="type-h4 flex flex-wrap items-center gap-2">
                          {a.label ?? a.recipient_name}
                          {a.is_default ? <Tag tone="accent">Default</Tag> : null}
                        </span>
                        <span className="type-body-sm mt-1 block text-muted">
                          {[a.line1, a.line2, a.area, a.city, a.region_label].filter(Boolean).join(", ")}
                        </span>
                        {!ok ? <span className="type-caption mt-1 block font-semibold">{availabilityText(a.delivery_availability)}</span> : null}
                      </span>
                    </label>
                  );
                })}
              </div>
            ) : (
              <p className="type-body mt-4 text-muted">You haven&apos;t saved an address yet.</p>
            )}
            <Link href={`/profile/addresses?next=${encodeURIComponent(`/subscribe?plan=${slug}`)}`} className="type-body-sm mt-4 inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Add an address
            </Link>
          </fieldset>

          <div>
            <label htmlFor="code" className="type-h4 block">
              Promotion code <span className="type-body-sm font-normal text-muted">(optional)</span>
            </label>
            <input
              id="code"
              name="code"
              autoComplete="off"
              autoCapitalize="characters"
              spellCheck={false}
              maxLength={30}
              className="type-body mt-2 block min-h-12 w-full max-w-[20rem] border border-border-strong bg-surface px-4 uppercase"
            />
          </div>

          <div>
            <button type="submit" disabled={!usable.length} className={buttonClasses("primary", "lg")}>
              Review and pay
            </button>
            {!usable.length ? <p className="type-body-sm mt-3 text-muted">Add an address Mise delivers to first.</p> : null}
          </div>
        </form>
      )}
    </Container>
  );
}
