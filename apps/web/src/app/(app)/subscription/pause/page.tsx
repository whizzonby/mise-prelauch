import { buttonClasses } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { Form, FormMessage, SubmitButton } from "@/components/forms";
import { api, requireCustomer } from "@/lib/session";

import { pause, resume } from "../actions";

export const metadata: Metadata = { title: "Pause deliveries" };

export default async function PausePage() {
  const { token } = await requireCustomer();
  const { subscription: sub } = await api.commerce.subscription(token);
  if (!sub) redirect("/plans");
  const weeks = Array.from({ length: sub.actions.max_pause_weeks }, (_, i) => i + 1);

  return (
    <div className="max-w-[44rem]">
      <h1 className="type-h2">{sub.actions.resume ? "Your pause" : "Pause deliveries"}</h1>
      {sub.pause ? (
        <>
          <p className="type-body mt-3">
            Paused <strong>{sub.pause.label}</strong>. Deliveries start again after that, or now if you resume.
          </p>
          <Form action={resume} className="mt-6">
            <input type="hidden" name="subscription_id" value={sub.id} />
            <FormMessage />
            <SubmitButton pending="Resuming…">Resume now</SubmitButton>
          </Form>
        </>
      ) : sub.actions.pause ? (
        <>
          <p className="type-body mt-3 text-muted">
            Skip a few weeks without cancelling. The pause starts with your first week that hasn&apos;t been paid yet: a week you&apos;ve
            already paid for is still delivered, and nothing is refunded. You won&apos;t be charged during the pause.
          </p>
          <Form action={pause} className="mt-8">
            <input type="hidden" name="subscription_id" value={sub.id} />
            <fieldset>
              <legend className="type-h4">How long?</legend>
              <div className="mt-3 flex flex-wrap gap-3">
                {weeks.map((w) => (
                  <label
                    key={w}
                    className="type-body-sm flex min-h-12 cursor-pointer items-center gap-2 border border-border bg-surface px-4 font-semibold has-[:checked]:border-primary has-[:checked]:ring-1 has-[:checked]:ring-primary"
                  >
                    <input type="radio" name="weeks" value={w} defaultChecked={w === 1} className="size-4" />
                    {w} {w === 1 ? "week" : "weeks"}
                  </label>
                ))}
              </div>
            </fieldset>
            <FormMessage />
            <div className="flex flex-wrap gap-3">
              <SubmitButton pending="Pausing…">Pause deliveries</SubmitButton>
              <Link href="/subscription" className={buttonClasses("secondary")}>
                Back
              </Link>
            </div>
          </Form>
        </>
      ) : (
        <p className="type-body mt-6 border-l-4 border-warning pl-4">
          Your subscription can&apos;t be paused right now ({sub.status_label.toLowerCase()}).
        </p>
      )}
    </div>
  );
}
