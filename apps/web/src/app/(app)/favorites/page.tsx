import { buttonClasses, Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { EmptyState, MealCard, MealGrid } from "@/components/meal-card";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Saved meals" };

export default async function FavoritesPage() {
  const { token } = await requireCustomer();
  const { items } = await api.account.favorites(token);
  return (
    <Container className="py-10 md:py-14">
      <h1 className="type-h1">Saved meals</h1>
      <p className="type-body-lg mt-4 max-w-[40rem] text-muted">
        The meals you would like to cook. When weekly ordering opens, you will be able to choose from here.
      </p>
      <div className="mt-10">
        {items.length ? (
          <MealGrid>
            {items.map((m, i) => (
              <MealCard key={m.id} meal={m} saved priority={i < 3} />
            ))}
          </MealGrid>
        ) : (
          <EmptyState
            title="Your saved meals will appear here."
            action={
              <Link href="/meals" className={buttonClasses("primary")}>
                Explore meals
              </Link>
            }
          >
            Tap the heart on any meal to keep it for later.
          </EmptyState>
        )}
      </div>
    </Container>
  );
}
