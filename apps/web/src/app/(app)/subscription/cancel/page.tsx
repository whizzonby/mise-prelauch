import { buttonClasses } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { Form, FormMessage, SubmitButton } from "@/components/forms";
import { api, requireCustomer } from "@/lib/session";

import { cancel } from "../actions";

export const metadata: Metadata = { title: "Cancel subscription" };

const reasons: Record<string, string> = {
  too_expensive: "It costs too much",
  not_enough_time: "I don't have time to cook",
  meals_not_for_me: "The meals aren't for me",
  moving: "I'm moving",
  temporary: "I only needed it for a while",
  other: "Something else",
};

/** Plain and quick: no hoops, no guilt. Pausing is offered once, as an option. */
export default async function CancelPage() {
  const { token } = await requireCustomer();
  const { subscription: sub } = await api.commerce.subscription(token);
  if (!sub) redirect("/plans");
  if (!sub.actions.cancel) redirect("/subscription");

  return (
    <div className="max-w-[44rem]">
      <h1 className="type-h2">Cancel subscription</h1>
      <ul className="type-body mt-4 grid gap-2">
        <li>Weeks you&apos;ve already paid for are still delivered.</li>
        <li>Any week not yet paid is cancelled, and you won&apos;t be charged again.</li>
        <li>Payments aren&apos;t refunded automatically. If something went wrong with an order, contact us.</li>
        <li>Your orders and invoices stay in your account.</li>
      </ul>
      {sub.actions.pause ? (
        <p className="type-body-sm mt-6 text-muted">
          Need a break instead?{" "}
          <Link href="/subscription/pause" className="font-semibold text-foreground underline underline-offset-4">
            Pause for a few weeks
          </Link>
          .
        </p>
      ) : null}
      <Form action={cancel} className="mt-8">
        <input type="hidden" name="subscription_id" value={sub.id} />
        <fieldset>
          <legend className="type-h4">
            Why are you leaving? <span className="type-body-sm font-normal text-muted">(optional)</span>
          </legend>
          <div className="mt-3 grid gap-1">
            {Object.entries(reasons).map(([key, label]) => (
              <label key={key} className="type-body-sm flex min-h-11 items-center gap-3">
                <input type="radio" name="reason" value={key} className="size-4" />
                {label}
              </label>
            ))}
          </div>
        </fieldset>
        <FormMessage />
        <div className="flex flex-wrap gap-3">
          <SubmitButton pending="Cancelling…">Cancel subscription</SubmitButton>
          <Link href="/subscription" className={buttonClasses("secondary")}>
            Keep my subscription
          </Link>
        </div>
      </Form>
    </div>
  );
}
