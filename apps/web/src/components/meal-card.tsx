import type { MealCard as Meal } from "@mise/api-client";
import { cx, Tag } from "@mise/ui";
import Image from "next/image";
import Link from "next/link";
import type { ReactNode } from "react";

import { minutes } from "@/lib/format";

import { FavoriteButton } from "./favorite-button";

/** Marks placeholder content so nobody mistakes it for the real menu. */
export function DemoTag() {
  return <Tag tone="warning">Demo content</Tag>;
}

/**
 * One meal, set like a cookbook entry: photograph, category, name, who wrote
 * it, how long it takes. Saving sits on the photograph, out of the way of
 * the link.
 */
export function MealCard({
  meal,
  saved,
  reasons,
  priority,
  className,
}: {
  meal: Meal;
  saved: boolean;
  /** Why it was recommended, shown under the summary. */
  reasons?: string[];
  priority?: boolean;
  className?: string;
}) {
  return (
    <article className={cx("group relative flex flex-col", className)}>
      <div className="relative aspect-[4/3] overflow-hidden rounded-lg bg-border">
        {meal.image ? (
          <Image
            src={meal.image.src}
            alt={meal.image.alt}
            fill
            priority={priority}
            sizes="(min-width: 1280px) 30vw, (min-width: 768px) 45vw, 92vw"
            className="object-cover transition-transform duration-(--duration-editorial) ease-settle motion-safe:group-hover:scale-[1.02]"
          />
        ) : null}
        <div className="absolute right-3 top-3 z-10">
          <FavoriteButton mealId={meal.id} mealName={meal.name} initial={saved} />
        </div>
      </div>
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <span className="type-caption font-semibold uppercase tracking-[0.08em] text-secondary">{meal.category.name}</span>
        {meal.is_demo ? <DemoTag /> : null}
      </div>
      <h3 className="type-h3 mt-1.5">
        <Link href={`/meals/${meal.slug}`} className="after:absolute after:inset-0 after:content-[''] hover:underline hover:decoration-1 hover:underline-offset-4">
          {meal.name}
        </Link>
      </h3>
      <p className="type-body-sm mt-2 max-w-[42ch] text-muted">{meal.summary}</p>
      {reasons?.length ? <p className="type-caption mt-2 font-semibold text-secondary">{reasons.slice(0, 2).join(" · ")}</p> : null}
      <dl className="type-caption type-figures mt-4 flex flex-wrap gap-x-5 gap-y-1 border-t border-border pt-3 text-muted">
        <div>
          <dt className="sr-only">Time</dt>
          <dd>{minutes(meal.total_minutes)}</dd>
        </div>
        <div>
          <dt className="sr-only">Servings</dt>
          <dd>Serves {meal.servings}</dd>
        </div>
        {meal.dietary.length ? (
          <div>
            <dt className="sr-only">Dietary</dt>
            <dd>{meal.dietary.map((d) => d.label).join(", ")}</dd>
          </div>
        ) : null}
        {meal.nutrition ? (
          <div>
            <dt className="sr-only">Energy</dt>
            <dd>{meal.nutrition.calories} kcal</dd>
          </div>
        ) : null}
      </dl>
    </article>
  );
}

export function MealGrid({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cx("grid gap-x-8 gap-y-12 sm:grid-cols-2 xl:grid-cols-3", className)}>{children}</div>;
}

/** A calm, honest empty state with one way forward. */
export function EmptyState({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="border-t border-foreground py-12">
      <h2 className="type-h3">{title}</h2>
      {children ? <div className="type-body mt-3 max-w-[40rem] text-muted">{children}</div> : null}
      {action ? <div className="mt-6">{action}</div> : null}
    </div>
  );
}

/** The small heading pattern used across the app: an eyebrow and a serif title. */
export function SectionHeading({ eyebrow, title, id, action }: { eyebrow?: string; title: string; id?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div>
        {eyebrow ? <p className="type-caption font-semibold uppercase tracking-[0.08em] text-accent">{eyebrow}</p> : null}
        <h2 id={id} className="type-h2 mt-1">
          {title}
        </h2>
      </div>
      {action}
    </div>
  );
}
