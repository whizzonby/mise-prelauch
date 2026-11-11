import { ApiError, type MealDetail } from "@mise/api-client";
import { Container, Tag } from "@mise/ui";
import { ArrowLeft } from "lucide-react";
import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";
import { notFound } from "next/navigation";

import { FavoriteButton } from "@/components/favorite-button";
import { DemoTag } from "@/components/meal-card";
import { savedMealIds } from "@/lib/data";
import { minutes } from "@/lib/format";
import { shortDateTime } from "@/lib/greeting";
import { api, requireCustomer } from "@/lib/session";

type Params = Promise<{ slug: string }>;

async function load(slug: string): Promise<MealDetail> {
  try {
    return await api.catalog.meal(slug);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) notFound();
    throw error;
  }
}

export async function generateMetadata({ params }: { params: Params }): Promise<Metadata> {
  const { slug } = await params;
  const meal = await api.catalog.meal(slug).catch(() => null);
  return { title: meal?.name ?? "Meal" };
}

export default async function MealPage({ params }: { params: Params }) {
  const { token } = await requireCustomer();
  const { slug } = await params;
  const [meal, saved] = await Promise.all([load(slug), savedMealIds(token)]);
  const hero = meal.images[0] ?? meal.image;
  const facts = [
    { label: "Preparation", value: minutes(meal.prep_minutes) },
    { label: "Cooking", value: minutes(meal.cook_minutes) },
    { label: "Serves", value: String(meal.servings) },
    ...(meal.protein ? [{ label: "Protein", value: meal.protein.label }] : []),
  ];

  return (
    <article>
      <Container className="pt-6 md:pt-10">
        <Link href="/meals" className="type-body-sm inline-flex min-h-11 items-center gap-2 font-semibold text-muted hover:text-foreground">
          <ArrowLeft aria-hidden="true" className="size-4" strokeWidth={1.75} /> All meals
        </Link>
      </Container>

      <Container className="mt-4 grid gap-8 lg:grid-cols-[1.3fr_1fr] lg:gap-14">
        <div className="relative aspect-[4/3] overflow-hidden rounded-lg bg-border lg:aspect-[5/4]">
          {hero ? <Image src={hero.src} alt={hero.alt} fill priority sizes="(min-width: 1024px) 56vw, 100vw" className="object-cover" /> : null}
        </div>
        <div className="flex flex-col">
          <div className="flex flex-wrap items-center gap-2">
            <Link href={`/meals?category=${meal.category.slug}`} className="type-caption font-semibold uppercase tracking-[0.08em] text-secondary hover:underline">
              {meal.category.name}
            </Link>
            {meal.is_demo ? <DemoTag /> : null}
          </div>
          <h1 className="type-h2 mt-3">{meal.name}</h1>
          <p className="type-body-lg mt-4 text-muted">{meal.summary}</p>
          {meal.chef ? (
            <p className="type-body-sm mt-4">
              Recipe by{" "}
              <Link href={`/chefs/${meal.chef.slug}`} className="font-semibold underline underline-offset-4">
                {meal.chef.name}
              </Link>
            </p>
          ) : null}

          <dl className="type-figures mt-8 grid grid-cols-2 gap-px overflow-hidden rounded-md border border-border bg-border sm:grid-cols-4 lg:grid-cols-2 xl:grid-cols-4">
            {facts.map((f) => (
              <div key={f.label} className="bg-surface p-4">
                <dt className="type-caption text-muted">{f.label}</dt>
                <dd className="type-h4 mt-1">{f.value}</dd>
              </div>
            ))}
          </dl>

          {meal.dietary.length ? (
            <ul aria-label="Dietary" className="mt-5 flex flex-wrap gap-2">
              {meal.dietary.map((d) => (
                <li key={d.key}>
                  <Tag tone="success">{d.label}</Tag>
                </li>
              ))}
            </ul>
          ) : null}

          <div className="mt-8 flex flex-wrap items-start gap-4">
            <FavoriteButton mealId={meal.id} mealName={meal.name} initial={saved.has(meal.id)} variant="full" />
          </div>
          <p className="type-caption mt-4 max-w-[40ch] text-muted">
            Choosing meals for weekly delivery opens soon. Save this one and it will be waiting for you.
          </p>
        </div>
      </Container>

      <Container className="mb-16 mt-14 grid gap-12 md:mb-24 lg:grid-cols-[1.3fr_1fr] lg:gap-14">
        <div>
          {meal.description ? (
            <section aria-labelledby="about">
              <h2 id="about" className="type-h3">
                About this meal
              </h2>
              <p className="type-body mt-4 max-w-[62ch] whitespace-pre-line">{meal.description}</p>
            </section>
          ) : null}
          {meal.cooking_overview ? (
            <section aria-labelledby="cooking" className="mt-12">
              <h2 id="cooking" className="type-h3">
                How you will cook it
              </h2>
              <p className="type-body mt-4 max-w-[62ch] whitespace-pre-line">{meal.cooking_overview}</p>
              <p className="type-caption mt-3 text-muted">The full method is on the recipe card in your kit.</p>
            </section>
          ) : null}
          {meal.chef_detail ? (
            <section aria-labelledby="chef" className="mt-12 border-t border-border pt-8">
              <h2 id="chef" className="type-h4">
                About {meal.chef_detail.name}
              </h2>
              <p className="type-body-sm mt-3 max-w-[62ch] text-muted">{meal.chef_detail.bio}</p>
            </section>
          ) : null}
        </div>

        <aside className="grid content-start gap-10">
          <section aria-labelledby="ingredients" className="border-t-4 border-primary bg-surface p-6">
            <h2 id="ingredients" className="type-h3">
              In your kit
            </h2>
            {meal.ingredients.length ? (
              <ul className="mt-4 divide-y divide-border">
                {meal.ingredients.map((i) => (
                  <li key={i.name} className="type-body-sm flex justify-between gap-4 py-2.5">
                    <span>{i.name}</span>
                    {i.quantity ? <span className="type-figures text-right text-muted">{i.quantity}</span> : null}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="type-body-sm mt-3 text-muted">The ingredient list will be published with the recipe.</p>
            )}
          </section>

          <section aria-labelledby="allergens">
            <h2 id="allergens" className="type-h4">
              Allergens
            </h2>
            {meal.allergens.status === "declared" ? (
              meal.allergens.contains.length ? (
                <p className="type-body-sm mt-3">
                  <span className="font-semibold">Contains: </span>
                  {meal.allergens.contains.map((a) => a.label).join(", ")}.
                </p>
              ) : (
                <p className="type-body-sm mt-3">The kitchen has declared none of the major allergens in this recipe&rsquo;s ingredients.</p>
              )
            ) : (
              <p className="type-body-sm mt-3 border-l-2 border-warning pl-3 font-semibold">
                Allergen information for this meal has not been confirmed yet. Do not rely on it if you have an allergy.
              </p>
            )}
            <p className="type-caption mt-3 text-muted">{meal.allergens.notice}</p>
          </section>

          <section aria-labelledby="nutrition">
            <h2 id="nutrition" className="type-h4">
              Nutrition per serving
            </h2>
            {meal.nutrition_detail ? (
              <>
                <dl className="type-body-sm type-figures mt-3 divide-y divide-border border-y border-border">
                  {[
                    ["Energy", `${meal.nutrition_detail.calories} kcal`],
                    ["Protein", `${meal.nutrition_detail.protein_g} g`],
                    ["Carbohydrate", `${meal.nutrition_detail.carbs_g} g`],
                    ["Fat", `${meal.nutrition_detail.fat_g} g`],
                    ...(meal.nutrition_detail.fibre_g != null ? [["Fibre", `${meal.nutrition_detail.fibre_g} g`]] : []),
                    ...(meal.nutrition_detail.sodium_mg != null ? [["Sodium", `${meal.nutrition_detail.sodium_mg} mg`]] : []),
                  ].map(([k, v]) => (
                    <div key={k} className="flex justify-between py-2">
                      <dt>{k}</dt>
                      <dd>{v}</dd>
                    </div>
                  ))}
                </dl>
                {meal.nutrition_detail.verified_at ? (
                  <p className="type-caption mt-2 text-muted">Checked by the Mise nutritionist, {shortDateTime(meal.nutrition_detail.verified_at)}.</p>
                ) : null}
              </>
            ) : (
              <p className="type-body-sm mt-3 text-muted">
                Nutrition figures are published once the Mise nutritionist has checked the final recipe.
              </p>
            )}
          </section>

          {hero?.credit ? <p className="type-caption text-muted">Photograph: {hero.credit}.</p> : null}
        </aside>
      </Container>
    </article>
  );
}
