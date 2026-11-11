import { buttonClasses, Container, Wordmark } from "@mise/ui";
import { FlaskConical } from "lucide-react";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { formatMoney } from "@/lib/commerce";
import { api } from "@/lib/session";

import { completeTestPayment } from "./actions";

export const metadata: Metadata = { title: "Test payment", robots: { index: false } };

/**
 * The test payment provider's hosted page. It stands in for a real
 * provider's checkout while none is chosen: there are no card fields, only
 * buttons, and the API serves it only outside production. Pressing a
 * button makes the simulator send a signed webhook to the API, as a real
 * provider would.
 */
export default async function TestCheckoutPage({ params }: { params: Promise<{ ref: string }> }) {
  const { ref } = await params;
  const session = await api.testpay.session(ref).catch(() => null);
  if (!session) notFound();
  const open = session.status === "open";
  return (
    <div className="min-h-svh bg-background">
      <div className="border-b-4 border-warning bg-surface">
        <Container className="type-body-sm flex items-center gap-3 py-3 font-semibold">
          <FlaskConical aria-hidden="true" className="size-5 text-warning" strokeWidth={1.75} />
          Test payment provider. No real money moves and no card is needed.
        </Container>
      </div>
      <Container className="max-w-[32rem] py-14">
        <p className="text-[1.75rem] text-primary">
          <Wordmark />
        </p>
        <h1 className="type-h2 mt-8">Test checkout</h1>
        <p className="type-body mt-2 text-muted">{session.description}</p>
        <p className="type-figures mt-6 font-display text-[2.5rem] leading-none">{formatMoney(session.amount_minor, session.currency)}</p>

        {open ? (
          <form action={completeTestPayment} className="mt-10 grid gap-6">
            <input type="hidden" name="ref" value={ref} />
            <fieldset className="grid gap-3">
              <legend className="type-h4">The saved test card should, on later weekly charges:</legend>
              <label className="type-body-sm flex min-h-11 items-center gap-3">
                <input type="radio" name="card" value="approve" defaultChecked className="size-5" /> Approve them
              </label>
              <label className="type-body-sm flex min-h-11 items-center gap-3">
                <input type="radio" name="card" value="decline" className="size-5" /> Decline them (to try a failed renewal)
              </label>
            </fieldset>
            <div className="grid gap-3 sm:grid-cols-2">
              <button type="submit" name="outcome" value="approve" className={buttonClasses("primary", "lg")}>
                Approve payment
              </button>
              <button type="submit" name="outcome" value="decline" className={buttonClasses("secondary", "lg")}>
                Decline payment
              </button>
            </div>
            <button type="submit" name="outcome" value="cancel" className="type-body-sm min-h-11 justify-self-start font-semibold underline underline-offset-4">
              Cancel and go back
            </button>
          </form>
        ) : (
          <div className="mt-10">
            <p className="type-body">This test checkout is finished ({session.status}).</p>
            <a href={session.return_url} className={buttonClasses("primary", "md", "mt-6")}>
              Return to Mise
            </a>
          </div>
        )}
      </Container>
    </div>
  );
}
