import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { AddressFields } from "@/components/address-fields";
import { Form, FormMessage, SubmitButton } from "@/components/forms";
import { reference } from "@/lib/data";
import { safeNext } from "@/lib/safe-redirect";
import { availabilityText } from "@/lib/format";
import { api, requireCustomer } from "@/lib/session";

import { createAddress, deleteAddress, updateAddress } from "../actions";

export const metadata: Metadata = { title: "Addresses" };

/**
 * Delivery addresses. Each shows whether Mise delivers there, which is
 * "still being confirmed" until delivery areas are set up: an address saved
 * here is never a promise of delivery.
 */
export default async function AddressesPage({ searchParams }: { searchParams: Promise<{ next?: string }> }) {
  const { token } = await requireCustomer();
  // Arriving from a step that needs an address (subscribing): offer the way back once one is saved.
  const { next: rawNext } = await searchParams;
  const next = rawNext ? safeNext(rawNext, "") : "";
  const [{ items }, ref] = await Promise.all([api.account.addresses(token), reference()]);
  return (
    <>
      <h1 className="type-h2">Addresses</h1>
      <p className="type-body mt-3 text-muted">Where you would like your kits delivered. Mise is starting in Trinidad and Tobago.</p>
      {next && items.length ? (
        <p className="mt-6">
          <Link href={next} className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
            Continue with your plan
          </Link>
        </p>
      ) : null}

      {items.length ? (
        <ul className="mt-8 grid gap-4">
          {items.map((a) => (
            <li key={a.id} className="border border-border bg-surface p-5">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <p className="type-h4 flex flex-wrap items-center gap-2">
                    {a.label ?? a.recipient_name}
                    {a.is_default ? <Tag tone="accent">Default</Tag> : null}
                  </p>
                  <address className="type-body-sm mt-2 not-italic text-muted">
                    {[a.recipient_name, a.line1, a.line2, a.area, a.city, a.region_label, "Trinidad and Tobago"].filter(Boolean).join(", ")}
                  </address>
                  {a.instructions ? <p className="type-caption mt-2 text-muted">Instructions: {a.instructions}</p> : null}
                  <p className="type-caption mt-2">{availabilityText(a.delivery_availability)}</p>
                </div>
                <form action={deleteAddress}>
                  <input type="hidden" name="id" value={a.id} />
                  <button type="submit" className="type-body-sm min-h-11 font-semibold text-muted underline underline-offset-4 hover:text-error">
                    Remove<span className="sr-only"> {a.label ?? a.line1}</span>
                  </button>
                </form>
              </div>
              <details className="mt-3 border-t border-border pt-3">
                <summary className="type-body-sm min-h-11 cursor-pointer content-center font-semibold underline underline-offset-4">
                  Edit<span className="sr-only"> {a.label ?? a.line1}</span>
                </summary>
                <Form action={updateAddress} aria-label={`Edit ${a.label ?? a.line1}`} className="mt-4">
                  <input type="hidden" name="id" value={a.id} />
                  <AddressFields prefix={a.id} regions={ref.regions} address={a} />
                  <FormMessage />
                  <div>
                    <SubmitButton pending="Saving…" variant="secondary">
                      Save address
                    </SubmitButton>
                  </div>
                </Form>
              </details>
            </li>
          ))}
        </ul>
      ) : (
        <p className="type-body mt-8 border-t border-foreground pt-6">You have not saved an address yet.</p>
      )}

      {items.length < 10 ? (
        <section aria-labelledby="add" className="mt-12 border-t border-border pt-8">
          <h2 id="add" className="type-h3">
            Add an address
          </h2>
          <Form action={createAddress} resetOnDone aria-label="Add an address" className="mt-6">
            <AddressFields prefix="new" regions={ref.regions} />
            <FormMessage />
            <div>
              <SubmitButton pending="Adding…">Add address</SubmitButton>
            </div>
          </Form>
        </section>
      ) : (
        <p className="type-body-sm mt-8 text-muted">You have saved the maximum of 10 addresses. Remove one to add another.</p>
      )}
    </>
  );
}
