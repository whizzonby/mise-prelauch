import type { Plan } from "@mise/api-client";
import { buttonClasses, cx, Tag } from "@mise/ui";
import { Check } from "lucide-react";
import Link from "next/link";

import { formatMoney } from "@/lib/commerce";

/**
 * One plan in the comparison. The weekly price leads; the per-meal figure is
 * a plain division, labelled as such. No crossed-out prices, no countdowns,
 * no "most popular" badge: nothing here is invented to push a choice.
 */
export function PlanCard({ plan, current, action }: { plan: Plan; current?: boolean; action?: React.ReactNode }) {
  return (
    <article
      className={cx(
        "flex flex-col border bg-surface p-6 md:p-7",
        current ? "border-primary ring-1 ring-primary" : "border-border",
      )}
      aria-labelledby={`plan-${plan.id}`}
    >
      <div className="flex flex-wrap items-center gap-2">
        <p className="type-caption font-semibold uppercase tracking-[0.08em] text-secondary">{plan.segment.label}</p>
        {current ? <Tag tone="accent">Your plan</Tag> : null}
        {plan.is_demo ? <Tag tone="warning">Demo plan</Tag> : null}
      </div>
      <h2 id={`plan-${plan.id}`} className="type-h3 mt-2">
        {plan.name}
      </h2>
      <p className="type-body-sm mt-2 text-muted">{plan.summary}</p>

      <div className="mt-6 border-t border-border pt-5">
        {plan.price ? (
          <>
            <p className="type-figures">
              <span className="font-display text-[2.25rem] leading-none">{formatMoney(plan.price.amount_minor, plan.price.currency)}</span>
              <span className="type-body-sm text-muted"> a week</span>
            </p>
            <p className="type-caption mt-2 text-muted">
              {plan.meals_per_week} meal kits a week · about {formatMoney(plan.per_meal_minor, plan.price.currency)} each
            </p>
          </>
        ) : (
          <p className="type-body-sm text-muted">Price to be confirmed.</p>
        )}
        {plan.next_price ? (
          <p className="type-caption mt-2 font-semibold">
            From {new Date(plan.next_price.effective_from).toLocaleDateString("en-TT", { day: "numeric", month: "long" })}:{" "}
            {plan.next_price.formatted} a week for new subscriptions.
          </p>
        ) : null}
      </div>

      {plan.benefits.length ? (
        <ul className="mt-5 grid gap-2.5">
          {plan.benefits.map((b) => (
            <li key={b} className="type-body-sm flex gap-2.5">
              <Check aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-secondary" strokeWidth={2} />
              {b}
            </li>
          ))}
        </ul>
      ) : null}

      <div className="mt-auto pt-7">
        {action ??
          (plan.purchasable ? (
            <Link href={`/subscribe?plan=${encodeURIComponent(plan.slug)}`} className={buttonClasses("primary", "md", "w-full")}>
              Choose {plan.name}
            </Link>
          ) : (
            <p className="type-body-sm border-l-2 border-border-strong pl-3 text-muted">{plan.unavailable_reason}</p>
          ))}
        <Link href={`/plans/${plan.slug}`} className="type-body-sm mt-3 inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
          Plan details
        </Link>
      </div>
    </article>
  );
}
