import type { Subscription, SubscriptionCycle } from "@mise/api-client";
import { buttonClasses, Tag } from "@mise/ui";
import Link from "next/link";

import { deadline, selectHref } from "@/lib/commerce";

/**
 * The one thing a subscriber most needs to know on arrival: the next week,
 * when choosing closes, and whether their meals are confirmed. For anyone
 * else, a quiet way to see the plans.
 */
export function YourWeek({ subscription, nextWeek }: { subscription: Subscription | null; nextWeek?: SubscriptionCycle }) {
  if (!subscription) {
    return (
      <aside aria-labelledby="your-week" className="mt-8 flex flex-col gap-4 border border-border bg-surface p-5 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 id="your-week" className="type-h4">
            Start cooking with Mise
          </h2>
          <p className="type-body-sm mt-1 text-muted">Choose how many meals you want each week, then pick them from the weekly menu.</p>
        </div>
        <Link href="/plans" className={buttonClasses("primary", "md", "shrink-0")}>
          See plans
        </Link>
      </aside>
    );
  }
  if (subscription.status === "pending_payment" || subscription.status === "past_due") {
    return (
      <aside aria-labelledby="your-week" className="mt-8 flex flex-col gap-4 border border-error bg-surface p-5 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 id="your-week" className="type-h4">
            {subscription.status === "pending_payment" ? "Finish subscribing" : "A payment needs your attention"}
          </h2>
          <p className="type-body-sm mt-1 text-muted">
            {subscription.status === "pending_payment"
              ? "Your subscription starts once your first payment is confirmed."
              : "Pay before the menu closes to have the week delivered."}
          </p>
        </div>
        <Link href="/subscription" className={buttonClasses("primary", "md", "shrink-0")}>
          Go to subscription
        </Link>
      </aside>
    );
  }
  if (!nextWeek) {
    return (
      <aside aria-labelledby="your-week" className="mt-8 border-l-4 border-accent bg-surface p-5">
        <h2 id="your-week" className="type-h4">
          {subscription.plan.name} plan · {subscription.status_label}
        </h2>
        <p className="type-body-sm mt-1 text-muted">
          {subscription.pause ? `Paused ${subscription.pause.label}.` : subscription.cancel ? subscription.cancel.label + "." : "Your next week will appear here."}{" "}
          <Link href="/subscription" className="font-semibold text-foreground underline underline-offset-4">
            Manage
          </Link>
        </p>
      </aside>
    );
  }
  const confirmed = nextWeek.selection_status === "confirmed";
  return (
    <aside aria-labelledby="your-week" className="mt-8 flex flex-col gap-4 border-l-4 border-accent bg-surface p-5 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <p className="type-caption font-semibold uppercase tracking-[0.08em] text-accent">Your next week</p>
        <h2 id="your-week" className="type-h3 mt-1 flex flex-wrap items-center gap-2">
          {nextWeek.week_label}
          <Tag tone={confirmed ? "success" : "accent"}>{confirmed ? "Meals confirmed" : "Choose your meals"}</Tag>
        </h2>
        <p className="type-body-sm mt-1 text-muted">
          {nextWeek.menu_published
            ? `${nextWeek.plan.meals_per_week} meals, by ${deadline(nextWeek.cutoff_at)}.`
            : `The menu is coming soon. Choose by ${deadline(nextWeek.cutoff_at)}.`}
        </p>
      </div>
      {nextWeek.selection_open ? (
        <Link href={selectHref(nextWeek.week)} className={buttonClasses(confirmed ? "secondary" : "primary", "md", "shrink-0")}>
          {confirmed ? "Change meals" : "Choose meals"}
        </Link>
      ) : null}
    </aside>
  );
}
