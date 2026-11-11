import { buttonClasses } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { Form, FormMessage, SubmitButton } from "@/components/forms";
import { formatMoney } from "@/lib/commerce";
import { api, requireCustomer } from "@/lib/session";

import { changePlan } from "../actions";

export const metadata: Metadata = { title: "Change plan" };

/**
 * Switching plans. No proration has been approved, so a change applies
 * from the first week not yet billed; weeks already billed keep their plan.
 */
export default async function ChangePlanPage({ searchParams }: { searchParams: Promise<{ plan?: string }> }) {
  const { token } = await requireCustomer();
  const [{ subscription: sub }, { items }, { plan: preselect }] = await Promise.all([
    api.commerce.subscription(token),
    api.shop.plans(),
    searchParams,
  ]);
  if (!sub) redirect("/plans");
  const choices = items.filter((p) => p.purchasable && p.id !== sub.plan.id);

  return (
    <div className="max-w-[44rem]">
      <h1 className="type-h2">Change plan</h1>
      <p className="type-body mt-3 text-muted">
        You&apos;re on <strong className="text-foreground">{sub.plan.name}</strong> ({sub.plan.meals_per_week} meals, {sub.price_formatted} a
        week). A new plan starts with your first week that hasn&apos;t been billed yet. Weeks already billed stay as they are, and nothing is
        charged or refunded now.
      </p>
      {!sub.actions.change_plan ? (
        <p className="type-body mt-6 border-l-4 border-warning pl-4">Your plan can&apos;t be changed right now ({sub.status_label.toLowerCase()}).</p>
      ) : choices.length ? (
        <Form action={changePlan} className="mt-8">
          <input type="hidden" name="subscription_id" value={sub.id} />
          <fieldset className="grid gap-3">
            <legend className="sr-only">New plan</legend>
            {choices.map((p) => (
              <label
                key={p.id}
                className="flex cursor-pointer gap-4 border border-border bg-surface p-4 has-[:checked]:border-primary has-[:checked]:ring-1 has-[:checked]:ring-primary"
              >
                <input type="radio" name="plan_id" value={p.id} required defaultChecked={p.id === preselect} className="mt-1 size-5" />
                <span>
                  <span className="type-h4 block">{p.name}</span>
                  <span className="type-body-sm text-muted">
                    {p.meals_per_week} meals a week · {p.price ? formatMoney(p.price.amount_minor, p.price.currency) : ""} a week
                  </span>
                </span>
              </label>
            ))}
          </fieldset>
          <p className="type-body-sm text-muted">If you&apos;ve already confirmed meals for a week the new plan applies to, you&apos;ll be asked to confirm them again.</p>
          <FormMessage />
          <div className="flex flex-wrap gap-3">
            <SubmitButton pending="Changing…">Change plan</SubmitButton>
            <Link href="/subscription" className={buttonClasses("secondary")}>
              Keep my plan
            </Link>
          </div>
        </Form>
      ) : (
        <p className="type-body mt-6 text-muted">There are no other plans to switch to right now.</p>
      )}
    </div>
  );
}
