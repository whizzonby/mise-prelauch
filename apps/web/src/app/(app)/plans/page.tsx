import { buttonClasses, Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { TestModeNotice } from "@/components/commerce-bits";
import { EmptyState } from "@/components/meal-card";
import { PlanCard } from "@/components/plan-card";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Plans" };

/** Compare plans. Every figure comes from the API; nothing is hard-coded here. */
export default async function PlansPage() {
  const { token } = await requireCustomer();
  const [{ items }, info, { subscription }] = await Promise.all([api.shop.plans(), api.shop.info(), api.commerce.subscription(token)]);
  const live = subscription && subscription.status !== "pending_payment" ? subscription : null;

  return (
    <Container className="py-10 md:py-14">
      <p className="type-caption font-semibold uppercase tracking-[0.08em] text-accent">Plans</p>
      <h1 className="type-h1 mt-2 max-w-[20ch]">Cook Caribbean at home, every week.</h1>
      <p className="type-body-lg mt-4 max-w-[42rem] text-muted">
        Each plan is a number of chef-designed meal kits a week, paid a week at a time. {info.cutoff_rule} Pause or cancel from your
        account.
      </p>
      {info.payments.test ? <TestModeNotice className="mt-6" /> : null}
      {live ? (
        <p className="type-body mt-6 border-l-4 border-accent bg-surface p-4">
          You&apos;re on the <strong>{live.plan.name}</strong> plan.{" "}
          <Link href="/subscription/change-plan" className="font-semibold underline underline-offset-4">
            Change plan
          </Link>
        </p>
      ) : null}

      <ol className="mt-10 grid gap-6 border-y border-border py-6 sm:grid-cols-3" aria-label="How it works">
        {[
          ["1", "Choose a plan", "How many meals you want each week."],
          ["2", "Pick your meals", `From each week's menu, by the cutoff.`],
          ["3", "Cook", "Every ingredient, measured, with the recipe."],
        ].map(([n, title, body]) => (
          <li key={n} className="flex gap-4">
            <span aria-hidden="true" className="font-display text-[2rem] leading-none text-accent">
              {n}
            </span>
            <span>
              <span className="type-h4 block">{title}</span>
              <span className="type-body-sm text-muted">{body}</span>
            </span>
          </li>
        ))}
      </ol>

      <div className="mt-10">
        {items.length ? (
          <div className="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
            {items.map((p) => (
              <PlanCard
                key={p.id}
                plan={p}
                current={live?.plan.id === p.id}
                action={
                  live ? (
                    live.plan.id === p.id ? (
                      <Link href="/subscription" className={buttonClasses("secondary", "md", "w-full")}>
                        Manage your plan
                      </Link>
                    ) : p.purchasable ? (
                      <Link href={`/subscription/change-plan?plan=${p.id}`} className={buttonClasses("secondary", "md", "w-full")}>
                        Switch to {p.name}
                      </Link>
                    ) : undefined
                  ) : undefined
                }
              />
            ))}
          </div>
        ) : (
          <EmptyState title="Plans are being finalised.">We&apos;ll show them here as soon as they are ready.</EmptyState>
        )}
      </div>

      <p className="type-caption mt-10 max-w-[44rem] text-muted">
        Prices are per week in Trinidad and Tobago dollars. Before you pay you&apos;ll see the full amount for your first week, any
        delivery fee or tax, and what each following week costs.
      </p>
    </Container>
  );
}
