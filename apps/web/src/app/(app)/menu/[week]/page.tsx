import { ApiError } from "@mise/api-client";
import { buttonClasses, Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { MealCard, MealGrid } from "@/components/meal-card";
import { deadline, selectHref } from "@/lib/commerce";
import { savedMealIds } from "@/lib/data";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Weekly menu" };

/** A published week's menu. Subscribers see where they are with it; others, how to join. */
export default async function WeekMenuPage({ params }: { params: Promise<{ week: string }> }) {
  const { token } = await requireCustomer();
  const { week } = await params;
  if (!/^\d{4}-\d{2}-\d{2}$/.test(week)) notFound();
  const [menu, { items: weeks }, { subscription }, saved] = await Promise.all([
    api.shop.menu(week).catch((e) => {
      if (e instanceof ApiError && e.status === 404) return null;
      throw e;
    }),
    api.commerce.weeks(token),
    api.commerce.subscription(token),
    savedMealIds(token),
  ]);
  if (!menu) notFound();
  const mine = weeks.find((w) => w.week === week);

  return (
    <Container className="py-10 md:py-14">
      <p className="type-caption font-semibold uppercase tracking-[0.08em] text-accent">{menu.title ?? "Weekly menu"}</p>
      <h1 className="type-h1 mt-2">{menu.week_label}</h1>
      <p className="type-body-lg mt-3 max-w-[44rem] text-muted">
        {menu.is_open ? (
          <>
            {menu.meal_count} meals. Choose by <strong className="text-foreground">{deadline(menu.cutoff_at)}</strong>.
          </>
        ) : (
          <>{menu.meal_count} meals. Choosing for this week has closed.</>
        )}
      </p>
      {menu.note ? <p className="type-body mt-4 max-w-[44rem]">{menu.note}</p> : null}

      <div className="mt-6">
        {mine?.selection_open ? (
          <Link href={selectHref(week)} className={buttonClasses("primary", "lg")}>
            {mine.selection_status === "confirmed" ? "Change my meals" : "Choose my meals"}
          </Link>
        ) : mine ? (
          <p className="type-body-sm border-l-2 border-border-strong pl-3">
            Your week: <strong>{mine.state_label}</strong>.{" "}
            <Link href={selectHref(week)} className="font-semibold underline underline-offset-4">
              See your meals
            </Link>
          </p>
        ) : !subscription ? (
          <Link href="/plans" className={buttonClasses("primary", "lg")}>
            Subscribe to choose from this menu
          </Link>
        ) : null}
      </div>

      <div className="mt-12">
        <MealGrid>
          {menu.meals.map((m, i) => (
            <MealCard key={m.id} meal={m} saved={saved.has(m.id)} priority={i < 3} />
          ))}
        </MealGrid>
      </div>
    </Container>
  );
}
