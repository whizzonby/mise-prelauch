"use client";

import { Choice, Container, cx, Heading, ImageReveal, Section, Tag, Text } from "@mise/ui";
import Image from "next/image";
import { useEffect, useRef, useState } from "react";

import { mealCategories, meals, mealsSection, type Meal, type MealCategoryId } from "@/content/meals";

import { useTrack } from "./providers";

type Filter = MealCategoryId | "all";

const categoryLabel = Object.fromEntries(mealCategories.map((c) => [c.id, c.label])) as Record<MealCategoryId, string>;

/** One dish, set like an entry in a cookbook: photograph, name, who wrote it, how long it takes. */
export function MealPreview({ meal, className }: { meal: Meal; className?: string }) {
  const ref = useRef<HTMLElement>(null);
  const track = useTrack();

  useEffect(() => {
    const node = ref.current;
    if (!node || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          track("meal_viewed", { meal: meal.id, category: meal.category });
          observer.disconnect();
        }
      },
      { threshold: 0.6 },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [meal.id, meal.category, track]);

  return (
    <article ref={ref} className={className}>
      <ImageReveal>
        <div className="relative aspect-[4/5] overflow-hidden rounded-lg bg-border">
          <Image
            src={meal.image.src}
            alt={meal.image.alt}
            fill
            placeholder="blur"
            sizes="(min-width: 1024px) 30vw, (min-width: 640px) 46vw, 92vw"
            className="object-cover"
          />
        </div>
      </ImageReveal>

      <p className="type-caption mt-4 text-muted">{categoryLabel[meal.category]}</p>
      <Heading as="h3" className="mt-1">
        {meal.name}
      </Heading>
      <Text className="mt-2 max-w-[38ch]" tone="muted">
        {meal.description}
      </Text>

      <dl className="type-body-sm type-figures mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 border-t border-border pt-4">
        <dt className="text-muted">Recipe by</dt>
        <dd>{meal.chef}</dd>
        <dt className="text-muted">Cooking time</dt>
        <dd>{meal.prepMinutes} minutes</dd>
        {meal.nutrition ? (
          <>
            <dt className="text-muted">Per serving</dt>
            <dd>
              {meal.nutrition.calories} kcal, {meal.nutrition.proteinGrams} g protein
            </dd>
          </>
        ) : null}
      </dl>

      {meal.dietary.length > 0 ? (
        <ul aria-label="Dietary" className="mt-4 flex flex-wrap gap-1.5">
          {meal.dietary.map((item) => (
            <li key={item}>
              <Tag tone="success">{item}</Tag>
            </li>
          ))}
        </ul>
      ) : null}
    </article>
  );
}

export function MealShowcase() {
  const [filter, setFilter] = useState<Filter>("all");
  const visible = filter === "all" ? meals : meals.filter((meal) => meal.category === filter);

  return (
    <Section id={mealsSection.id} tone="surface" aria-labelledby="meals-title">
      <Container>
        <div className="flex flex-col gap-8 lg:flex-row lg:items-end lg:justify-between">
          <Heading as="h2" id="meals-title" className="max-w-[14ch]">
            {mealsSection.title}
          </Heading>

          <fieldset>
            <legend className="sr-only">Show meals from</legend>
            <div className="flex flex-wrap gap-2">
              {[{ id: "all" as const, label: "All" }, ...mealCategories].map((category) => (
                <Choice
                  key={category.id}
                  type="radio"
                  name="meal-category"
                  value={category.id}
                  checked={filter === category.id}
                  onChange={() => setFilter(category.id)}
                  label={category.label}
                />
              ))}
            </div>
          </fieldset>
        </div>

        <div aria-live="polite" className="sr-only">
          Showing {visible.length} {visible.length === 1 ? "meal" : "meals"}
        </div>

        <ul className="mt-12 grid gap-x-8 gap-y-14 sm:grid-cols-2 md:mt-16 lg:grid-cols-3">
          {visible.map((meal, index) => (
            // The middle column drops, so the grid reads as a spread rather than a catalogue.
            <li key={meal.id} className={cx(index % 3 === 1 && "lg:mt-20")}>
              <MealPreview meal={meal} />
            </li>
          ))}
        </ul>

        <Text size="sm" tone="muted" className="mt-14 max-w-[60ch] border-t border-border pt-5">
          {mealsSection.note}
        </Text>
      </Container>
    </Section>
  );
}
