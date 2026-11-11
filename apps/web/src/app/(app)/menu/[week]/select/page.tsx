import { buttonClasses, Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState } from "@/components/meal-card";
import { MealSelector } from "@/components/meal-selector";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Choose your meals" };

export default async function SelectPage({ params }: { params: Promise<{ week: string }> }) {
  const { token } = await requireCustomer();
  const { week } = await params;
  if (!/^\d{4}-\d{2}-\d{2}$/.test(week)) notFound();
  const { items } = await api.commerce.weeks(token);
  const mine = items.find((w) => w.week === week);
  if (!mine) {
    return (
      <Container className="py-10 md:py-14">
        <EmptyState
          title="You don't have a delivery this week."
          action={
            <Link href="/subscription" className={buttonClasses("primary")}>
              Your subscription
            </Link>
          }
        >
          Weeks appear here once your subscription is active.
        </EmptyState>
      </Container>
    );
  }
  if (mine.state === "paused" || mine.state === "cancelled") {
    return (
      <Container className="py-10 md:py-14">
        <EmptyState title={`${mine.week_label} isn't being delivered.`}>This week is {mine.state_label.toLowerCase()}.</EmptyState>
      </Container>
    );
  }
  const page = await api.commerce.selection(token, mine.id);
  return (
    <Container className="py-10 pb-36 md:py-14 lg:pb-14">
      <MealSelector initial={page} />
    </Container>
  );
}
