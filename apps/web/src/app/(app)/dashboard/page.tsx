import { buttonClasses, Container } from "@mise/ui";
import { ArrowRight } from "lucide-react";
import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";

import { DemoTag, EmptyState, MealCard, MealGrid, SectionHeading } from "@/components/meal-card";
import { YourWeek } from "@/components/your-week";
import { savedMealIds } from "@/lib/data";
import { greeting, longDate } from "@/lib/greeting";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Home" };

/**
 * The customer's home. Discovery and next steps, nothing invented: the
 * subscription panel shows only what the API reports (the next week, its
 * deadline, whether meals are confirmed), and nothing about deliveries,
 * which are arranged in a later stage.
 */
export default async function Dashboard() {
  const { token, customer } = await requireCustomer();
  const [onboarding, recs, categories, chefs, saved, { subscription }] = await Promise.all([
    api.account.onboarding(token),
    api.account.recommendations(token, 6),
    api.catalog.categories(),
    api.catalog.chefs(),
    savedMealIds(token),
    api.commerce.subscription(token),
  ]);
  const nextWeek = subscription?.cycles
    .filter((c) => c.state === "scheduled")
    .sort((a, b) => a.week.localeCompare(b.week))[0];
  const spotlight = chefs.items[0] ? await api.catalog.chef(chefs.items[0].slug).catch(() => null) : null;
  const now = new Date();
  const browsable = categories.items.filter((c) => c.meal_count > 0);

  return (
    <>
      <Container className="pt-10 md:pt-14">
        <p className="type-caption font-semibold uppercase tracking-[0.08em] text-muted">{longDate(now)}</p>
        <h1 className="type-h1 mt-3 max-w-[18ch]">
          {greeting(now)}, {customer.first_name}.
        </h1>
        <p className="type-body-lg mt-4 max-w-[40rem] text-muted">Welcome to your Mise kitchen.</p>

        {!onboarding.completed_at ? (
          <aside aria-labelledby="finish-setup" className="mt-8 flex flex-col gap-4 border-l-4 border-accent bg-surface p-5 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 id="finish-setup" className="type-h4">
                Finish setting up your kitchen
              </h2>
              <p className="type-body-sm mt-1 text-muted">
                A few quick questions about who you cook for and what you eat, so the meals we suggest suit you.
              </p>
            </div>
            <Link href="/onboarding" className={buttonClasses("primary", "md", "shrink-0")}>
              Continue setup
            </Link>
          </aside>
        ) : null}

        <YourWeek subscription={subscription} nextWeek={nextWeek} />
      </Container>

      {/* Editorial hero: photograph beside a solid panel. No overlays, no gradients. */}
      <section aria-labelledby="hero-title" className="mt-10 md:mt-14">
        <Container>
          <div className="grid overflow-hidden rounded-lg md:grid-cols-[1.25fr_1fr]">
            <div className="relative aspect-[4/3] md:aspect-auto md:min-h-[26rem]">
              <Image
                src="/brand/market-stall.jpg"
                alt="A market stall piled with fresh peppers, tomatoes and greens"
                fill
                priority
                sizes="(min-width: 768px) 55vw, 100vw"
                className="object-cover"
              />
            </div>
            <div className="on-dark flex flex-col justify-center bg-primary p-7 text-primary-foreground md:p-12">
              <h2 id="hero-title" className="type-h2">
                Good food starts with great ingredients.
              </h2>
              <p className="type-body mt-4 max-w-[34ch] text-primary-foreground/80">
                Chef-written Caribbean recipes, with every ingredient measured out for you to cook at home.
              </p>
              <Link href="/meals" className={buttonClasses("primary", "lg", "mt-8 self-start")}>
                Explore meals <ArrowRight aria-hidden="true" className="size-5" strokeWidth={1.75} />
              </Link>
              <p className="type-caption mt-5 text-primary-foreground/75">
                Choose a plan, then pick your meals from each week&apos;s menu.
              </p>
            </div>
          </div>
        </Container>
      </section>

      <section aria-labelledby="recommended" className="mt-16 md:mt-24">
        <Container>
          <SectionHeading
            id="recommended"
            eyebrow={recs.personalised ? "For your kitchen" : "From the menu"}
            title={recs.personalised ? "Chosen from your preferences" : "Recently added"}
            action={
              <Link href="/meals" className="type-body-sm font-semibold underline underline-offset-4">
                See all meals
              </Link>
            }
          />
          {recs.items.length ? (
            <>
              <MealGrid className="mt-8">
                {recs.items.map((r, i) => (
                  <MealCard key={r.meal.id} meal={r.meal} saved={saved.has(r.meal.id)} reasons={r.reasons} priority={i < 3} />
                ))}
              </MealGrid>
              {recs.personalised ? (
                <p className="type-caption mt-8 max-w-[52rem] text-muted">
                  Suggested by matching the preferences you gave us against each recipe. Filtering by preference or
                  allergy does not guarantee a meal is free of an allergen.{" "}
                  <Link href="/profile/preferences" className="underline underline-offset-4">
                    Change your preferences
                  </Link>
                </p>
              ) : null}
            </>
          ) : (
            <EmptyState title="New meals are coming soon.">
              The first menus are being written and tested in our kitchen. They will appear here as soon as they are ready.
            </EmptyState>
          )}
        </Container>
      </section>

      {browsable.length ? (
        <section aria-labelledby="categories" className="mt-16 md:mt-24">
          <Container>
            <SectionHeading id="categories" eyebrow="Browse" title="Explore by kind of meal" />
            <ul className="mt-8 grid border-t border-foreground sm:grid-cols-2">
              {browsable.map((c) => (
                <li key={c.slug} className="border-b border-border sm:odd:border-r sm:odd:pr-8 sm:even:pl-8">
                  <Link href={`/meals?category=${c.slug}`} className="group flex items-start justify-between gap-6 py-6">
                    <span>
                      <span className="type-h3 block group-hover:underline group-hover:decoration-1 group-hover:underline-offset-4">{c.name}</span>
                      {c.description ? <span className="type-body-sm mt-1.5 block max-w-[38ch] text-muted">{c.description}</span> : null}
                    </span>
                    <span className="type-caption type-figures shrink-0 pt-2 text-muted">
                      {c.meal_count} {c.meal_count === 1 ? "meal" : "meals"}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          </Container>
        </section>
      ) : null}

      {spotlight ? (
        <section aria-labelledby="chef" className="mt-16 md:mt-24">
          <Container>
            <div className="grid gap-8 border-t border-foreground pt-10 md:grid-cols-[1fr_1.4fr] md:gap-14">
              {spotlight.image ? (
                <div className="relative aspect-[4/3] overflow-hidden rounded-lg bg-border">
                  <Image src={spotlight.image.src} alt={spotlight.image.alt} fill sizes="(min-width: 768px) 40vw, 92vw" className="object-cover" />
                </div>
              ) : null}
              <div>
                <p className="type-caption font-semibold uppercase tracking-[0.08em] text-accent">Who writes the recipes</p>
                <h2 id="chef" className="type-h2 mt-1">
                  {spotlight.name}
                </h2>
                {spotlight.is_demo ? (
                  <p className="mt-3">
                    <DemoTag />
                  </p>
                ) : null}
                {spotlight.role_title ? <p className="type-body-sm mt-3 font-semibold text-muted">{spotlight.role_title}</p> : null}
                <p className="type-body mt-4 max-w-[52ch]">{spotlight.bio}</p>
                <Link href={`/chefs/${spotlight.slug}`} className="type-body-sm mt-6 inline-flex font-semibold underline underline-offset-4">
                  Meals from {spotlight.name}
                </Link>
              </div>
            </div>
          </Container>
        </section>
      ) : null}

      <section aria-labelledby="story" className="mb-16 mt-16 md:mb-24 md:mt-24">
        <Container>
          <div className="grid gap-8 md:grid-cols-[1.4fr_1fr] md:items-center md:gap-14">
            <div>
              <p className="type-caption font-semibold uppercase tracking-[0.08em] text-accent">The Mise way</p>
              <h2 id="story" className="type-h2 mt-1 max-w-[20ch]">
                Fresh, local, and portioned so nothing goes to waste.
              </h2>
              <p className="type-body mt-4 max-w-[52ch] text-muted">
                Menus are planned around what growers in Trinidad and Tobago are harvesting. We season, chop and portion
                each ingredient in our kitchen, and you cook it at home from the recipe card.
              </p>
            </div>
            <div className="relative aspect-[4/3] overflow-hidden rounded-lg bg-border">
              <Image src="/brand/story-kit.jpg" alt="A box of fresh fruit and vegetables" fill sizes="(min-width: 768px) 38vw, 92vw" className="object-cover" />
            </div>
          </div>
        </Container>
      </section>
    </>
  );
}
