"use client";

import { buttonClasses } from "@mise/ui";
import { CircleCheck, CircleAlert, LoaderCircle } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";

import { checkoutStatus } from "@/app/(app)/subscribe/actions";

interface State {
  outcome: "pending" | "succeeded" | "failed" | "expired";
  status: string;
  weekLabel: string;
  invoiceId: string | null;
}

/**
 * Asks the server every two seconds (for up to a minute) until the payment
 * has an outcome. The page only ever shows what the server reports.
 */
export function CheckoutOutcome({ id, initial }: { id: string; initial: State }) {
  const [state, setState] = useState(initial);
  const [gaveUp, setGaveUp] = useState(false);

  useEffect(() => {
    if (state.outcome !== "pending") return;
    let tries = 0;
    const timer = setInterval(async () => {
      tries += 1;
      const next = await checkoutStatus(id);
      if (next) setState(next);
      if ((next && next.outcome !== "pending") || tries >= 30) {
        clearInterval(timer);
        if (tries >= 30) setGaveUp(true);
      }
    }, 2000);
    return () => clearInterval(timer);
  }, [id, state.outcome]);

  if (state.outcome === "succeeded") {
    const first = state.status === "active";
    return (
      <div aria-live="polite">
        <CircleCheck aria-hidden="true" className="size-10 text-success" strokeWidth={1.5} />
        <h1 className="type-h1 mt-4">{first ? "You're subscribed." : "Payment received."}</h1>
        <p className="type-body-lg mt-4 max-w-[40rem] text-muted">
          We received your payment for the week of {state.weekLabel}. A receipt is on its way to your inbox.
        </p>
        <div className="mt-8 flex flex-wrap gap-3">
          <Link href="/menu" className={buttonClasses("primary", "lg")}>
            Choose my meals
          </Link>
          {state.invoiceId ? (
            <Link href={`/billing/invoices/${state.invoiceId}`} className={buttonClasses("secondary", "lg")}>
              View invoice
            </Link>
          ) : null}
        </div>
      </div>
    );
  }
  if (state.outcome === "failed" || state.outcome === "expired") {
    return (
      <div aria-live="polite">
        <CircleAlert aria-hidden="true" className="size-10 text-error" strokeWidth={1.5} />
        <h1 className="type-h1 mt-4">{state.outcome === "expired" ? "Checkout timed out." : "Your payment didn't go through."}</h1>
        <p className="type-body-lg mt-4 max-w-[40rem] text-muted">Nothing was charged. You can try again from your subscription page.</p>
        <Link href="/subscription" className={buttonClasses("primary", "lg", "mt-8")}>
          Try again
        </Link>
      </div>
    );
  }
  return (
    <div aria-live="polite" aria-busy={!gaveUp}>
      <LoaderCircle aria-hidden="true" className="size-10 text-muted motion-safe:animate-spin" strokeWidth={1.5} />
      <h1 className="type-h1 mt-4">Confirming your payment…</h1>
      <p className="type-body-lg mt-4 max-w-[40rem] text-muted">
        {gaveUp
          ? "This is taking longer than usual. You can leave this page: your subscription page will show the result, and we'll email your receipt."
          : "We're waiting for the payment provider to confirm. This usually takes a few seconds."}
      </p>
      {gaveUp ? (
        <Link href="/subscription" className={buttonClasses("secondary", "md", "mt-8")}>
          Go to your subscription
        </Link>
      ) : null}
    </div>
  );
}
