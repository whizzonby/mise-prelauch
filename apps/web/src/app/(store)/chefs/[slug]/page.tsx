import { ApiError } from "@mise/api-client";
import { Container } from "@mise/ui";
import type { Metadata } from "next";
import Image from "next/image";
import { notFound } from "next/navigation";

import { DemoTag, EmptyState, MealCard, MealGrid, SectionHeading } from "@/components/meal-card";
import { savedMealIds } from "@/lib/data";
import { api, optionalSession } from "@/lib/session";

type Params = Promise<{ slug: string }>;

export const metadata: Metadata = { title: "Chef" };

/** A published chef profile: only what the chef has approved, and their meals. */
export default async function ChefPage({ params }: { params: Params }) {
  const token = (await optionalSession())?.token ?? null;
  const { slug } = await params;
  const chef = await api.catalog.chef(slug).catch((error) => {
    if (error instanceof ApiError && error.status === 404) notFound();
    throw error;
  });
  const saved = token ? await savedMealIds(token) : null;
  return (
    <Container className="py-10 md:py-14">
      <div className="grid gap-8 md:grid-cols-[1fr_1.4fr] md:gap-14">
        {chef.image ? (
          <div className="relative aspect-[4/3] overflow-hidden rounded-lg bg-border">
            <Image src={chef.image.src} alt={chef.image.alt} fill priority sizes="(min-width: 768px) 40vw, 92vw" className="object-cover" />
          </div>
        ) : null}
        <div>
          <p className="type-caption font-semibold uppercase tracking-[0.08em] text-accent">Recipes by</p>
          <h1 className="type-h1 mt-2">{chef.name}</h1>
          {chef.is_demo ? (
            <p className="mt-3">
              <DemoTag />
            </p>
          ) : null}
          {chef.role_title ? <p className="type-body-sm mt-4 font-semibold text-muted">{chef.role_title}</p> : null}
          <p className="type-body mt-4 max-w-[56ch] whitespace-pre-line">{chef.bio}</p>
        </div>
      </div>
      <section aria-labelledby="meals" className="mt-16">
        <SectionHeading id="meals" title={`Meals from ${chef.name}`} />
        {chef.meals.length ? (
          <MealGrid className="mt-8">
            {chef.meals.map((m) => (
              <MealCard key={m.id} meal={m} saved={saved ? saved.has(m.id) : null} />
            ))}
          </MealGrid>
        ) : (
          <EmptyState title="New meals are coming soon." />
        )}
      </section>
    </Container>
  );
}
