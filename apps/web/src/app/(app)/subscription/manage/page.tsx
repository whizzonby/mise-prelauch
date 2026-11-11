import { buttonClasses } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { Form, FormMessage, SubmitButton } from "@/components/forms";
import { availabilityText } from "@/lib/format";
import { api, requireCustomer } from "@/lib/session";

import { setAddress } from "../actions";

export const metadata: Metadata = { title: "Delivery address" };

/** Where future orders go. Orders already made keep the address they were made with. */
export default async function ManagePage() {
  const { token } = await requireCustomer();
  const [{ subscription: sub }, addresses] = await Promise.all([api.commerce.subscription(token), api.account.addresses(token)]);
  if (!sub) redirect("/plans");
  return (
    <div className="max-w-[44rem]">
      <h1 className="type-h2">Delivery address</h1>
      <p className="type-body mt-3 text-muted">
        Used for orders made from now on. Orders already confirmed keep their address.
      </p>
      <Form action={setAddress} className="mt-8">
        <input type="hidden" name="subscription_id" value={sub.id} />
        <fieldset className="grid gap-3">
          <legend className="sr-only">Address</legend>
          {addresses.items.map((a) => {
            const ok = a.delivery_availability === "available";
            return (
              <label
                key={a.id}
                className={`flex gap-4 border border-border bg-surface p-4 ${ok ? "cursor-pointer has-[:checked]:border-primary has-[:checked]:ring-1 has-[:checked]:ring-primary" : "opacity-70"}`}
              >
                <input type="radio" name="address_id" value={a.id} defaultChecked={a.id === sub.address?.id} disabled={!ok} required className="mt-1 size-5" />
                <span>
                  <span className="type-h4 block">{a.label ?? a.recipient_name}</span>
                  <span className="type-body-sm text-muted">{[a.line1, a.line2, a.area, a.city, a.region_label].filter(Boolean).join(", ")}</span>
                  {!ok ? <span className="type-caption mt-1 block font-semibold">{availabilityText(a.delivery_availability)}</span> : null}
                </span>
              </label>
            );
          })}
        </fieldset>
        <Link href="/profile/addresses" className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
          Add or edit addresses
        </Link>
        <FormMessage />
        <div className="flex flex-wrap gap-3">
          <SubmitButton pending="Saving…">Use this address</SubmitButton>
          <Link href="/subscription" className={buttonClasses("secondary")}>
            Back
          </Link>
        </div>
      </Form>
    </div>
  );
}
