import type { MealCard } from "@mise/api-client";
import { buttonClasses, Container, cx, Heading, Section, Tag, Text } from "@mise/ui";
import Image from "next/image";

import { mealsSection } from "@/content/home";
import { env } from "@/lib/env";
import { catalogImage } from "@/lib/storefront";

/** One published meal, set like an entry in a cookbook: photograph, name, who wrote it, how long it takes. */
export function MealPreview({ meal, className }: { meal: MealCard; className?: string }) {
  const image = meal.image ? catalogImage(meal.image.src) : null;
  return (
    <article className={cx("relative", className)}>
      <div className="relative aspect-[4/5] overflow-hidden rounded-lg bg-border">
        {image && meal.image ? (
          <Image
            src={image.src}
            unoptimized={image.unoptimized}
            alt={meal.image.alt}
            fill
            sizes="(min-width: 1024px) 30vw, (min-width: 640px) 46vw, 92vw"
            className="object-cover"
          />
        ) : null}
      </div>

      <p className="type-caption mt-4 flex flex-wrap items-center gap-2 text-muted">
        {meal.category.name}
        {meal.is_demo ? <Tag tone="warning">Demo meal</Tag> : null}
      </p>
      <Heading as="h3" className="mt-1">
        <a href={`${env.appUrl}/meals/${meal.slug}`} className="after:absolute after:inset-0 after:content-[''] hover:underline hover:underline-offset-4">
          {meal.name}
        </a>
      </Heading>
      <Text className="mt-2 max-w-[38ch]" tone="muted">
        {meal.summary}
      </Text>

      <dl className="type-body-sm type-figures mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 border-t border-border pt-4">
        {meal.chef ? (
          <>
            <dt className="text-muted">Recipe by</dt>
            <dd>{meal.chef.name}</dd>
          </>
        ) : null}
        <dt className="text-muted">Cooking time</dt>
        <dd>{meal.total_minutes} minutes</dd>
        <dt className="text-muted">Serves</dt>
        <dd>{meal.servings}</dd>
        {meal.nutrition ? (
          <>
            <dt className="text-muted">Per serving</dt>
            <dd>
              {meal.nutrition.calories} kcal, {meal.nutrition.protein_g} g protein
            </dd>
          </>
        ) : null}
      </dl>

      {meal.dietary.length > 0 ? (
        <ul aria-label="Dietary" className="mt-4 flex flex-wrap gap-1.5">
          {meal.dietary.map((item) => (
            <li key={item.key}>
              <Tag tone="success">{item.label}</Tag>
            </li>
          ))}
        </ul>
      ) : null}
    </article>
  );
}

/**
 * Meals from the live catalogue. With nothing published yet (or the API
 * unreachable) it says so plainly; it never falls back to sample dishes.
 */
export function MealShowcase({ meals }: { meals: MealCard[] | null }) {
  return (
    <Section id={mealsSection.id} tone="surface" aria-labelledby="meals-title">
      <Container>
        <div className="flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
          <Heading as="h2" id="meals-title" className="max-w-[16ch]">
            {mealsSection.title}
          </Heading>
          {meals?.length && env.siteMode === "launch" ? (
            <a href={`${env.appUrl}/meals`} className={buttonClasses("secondary", "md")}>
              {mealsSection.all}
            </a>
          ) : null}
        </div>

        {meals?.length ? (
          <ul className="mt-12 grid gap-x-8 gap-y-14 sm:grid-cols-2 md:mt-16 lg:grid-cols-3">
            {meals.map((meal, index) => (
              // The middle column drops, so the grid reads as a spread rather than a catalogue.
              <li key={meal.id} className={cx(index % 3 === 1 && "lg:mt-20")}>
                <MealPreview meal={meal} />
              </li>
            ))}
          </ul>
        ) : (
          <Text size="lg" className="mt-10 max-w-[48ch]">
            {meals === null ? mealsSection.unavailable : mealsSection.empty}
          </Text>
        )}

        <Text size="sm" tone="muted" className="mt-14 max-w-[60ch] border-t border-border pt-5">
          {mealsSection.note}
        </Text>
      </Container>
    </Section>
  );
}
