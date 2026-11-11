import { buttonClasses, Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { CheckoutOutcome } from "@/components/checkout-outcome";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Payment" };

/**
 * Where the payment provider sends the customer back. Coming back proves
 * nothing: this page shows what the API has recorded, and keeps asking
 * until the provider's confirmation has been processed.
 */
export default async function ResultPage({ searchParams }: { searchParams: Promise<{ checkout?: string; activated?: string }> }) {
  const { token } = await requireCustomer();
  const { checkout, activated } = await searchParams;
  if (activated) {
    return (
      <Container className="py-14 md:py-20">
        <h1 className="type-h1">You&apos;re subscribed.</h1>
        <p className="type-body-lg mt-4 max-w-[40rem] text-muted">Nothing was due today. Choose your meals for your first week.</p>
        <Link href="/menu" className={buttonClasses("primary", "lg", "mt-8")}>
          Choose my meals
        </Link>
      </Container>
    );
  }
  const initial = checkout ? await api.commerce.checkout(token, checkout).catch(() => null) : null;
  return (
    <Container className="py-14 md:py-20">
      {initial && checkout ? (
        <CheckoutOutcome
          id={checkout}
          initial={{ outcome: initial.outcome, status: initial.subscription_status, weekLabel: initial.week_label, invoiceId: initial.invoice_id }}
        />
      ) : (
        <>
          <h1 className="type-h1">We couldn&apos;t find that payment.</h1>
          <Link href="/subscription" className={buttonClasses("primary", "md", "mt-8")}>
            Go to your subscription
          </Link>
        </>
      )}
    </Container>
  );
}
