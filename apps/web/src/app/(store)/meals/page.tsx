import type { MealFilter } from "@mise/api-client";
import { ApiError } from "@mise/api-client";
import { buttonClasses, Choice, Container, Input, Select } from "@mise/ui";
import { Search } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";

import { FilterForm, FilterPanel } from "@/components/filter-form";
import { EmptyState, MealCard, MealGrid } from "@/components/meal-card";
import { reference, savedMealIds } from "@/lib/data";
import { api, optionalSession } from "@/lib/session";

export const metadata: Metadata = {
  title: "Meal kits",
  description: "Browse Mise's Caribbean meal kits: measured ingredients and a recipe card, cooked at home in Trinidad and Tobago.",
  alternates: { canonical: "/meals" },
};

type Search = Promise<Record<string, string | string[] | undefined>>;

const one = (v: string | string[] | undefined) => (Array.isArray(v) ? v[0] : v) ?? "";
const many = (v: string | string[] | undefined) => (Array.isArray(v) ? v : v ? v.split(",") : []).filter(Boolean);

const times = [
  { key: "20", label: "20 minutes or less" },
  { key: "30", label: "30 minutes or less" },
  { key: "45", label: "45 minutes or less" },
  { key: "60", label: "An hour or less" },
];

const PAGE_SIZE = 12;

/**
 * Browsing the published menu. Filtering happens in the database through the
 * API, one page at a time; the browser never receives the whole catalogue.
 */
export default async function MealsPage({ searchParams }: { searchParams: Search }) {
  const token = (await optionalSession())?.token ?? null;
  const params = await searchParams;
  const ref = await reference();
  // "Hide my allergens" needs the customer's own allergy list, so only a signed-in customer has it.
  const mine = token !== null && one(params.allergies) === "mine";

  const [categories, chefs, prefs, saved] = await Promise.all([
    api.catalog.categories(),
    api.catalog.chefs(),
    mine && token ? api.account.preferences(token) : Promise.resolve(null),
    token ? savedMealIds(token) : Promise.resolve(null),
  ]);
  const myAllergens = (prefs?.allergies ?? []).map((a) => a.allergen).filter((a) => a !== "other");

  const page = Math.max(1, Number(one(params.page)) || 1);
  const filter: MealFilter = {
    q: one(params.q).slice(0, 100) || undefined,
    category: one(params.category) || undefined,
    diet: many(params.diet),
    max_minutes: Number(one(params.max_minutes)) || undefined,
    protein: one(params.protein) || undefined,
    chef: one(params.chef) || undefined,
    exclude_allergens: mine ? myAllergens : undefined,
    sort: (["quickest", "name"].includes(one(params.sort)) ? one(params.sort) : undefined) as MealFilter["sort"],
    page,
    page_size: PAGE_SIZE,
  };
  let result = null;
  let invalid = false;
  try {
    result = await api.catalog.meals(filter);
  } catch (error) {
    if (!(error instanceof ApiError && error.status === 422)) throw error;
    invalid = true;
  }
  const active =
    [filter.category, filter.max_minutes, filter.protein, filter.chef, filter.sort, mine].filter(Boolean).length + (filter.diet?.length ?? 0);
  const filtered = Boolean(filter.q || filter.category || filter.diet?.length || filter.max_minutes || filter.protein || filter.chef || mine);
  const pages = result ? Math.max(1, Math.ceil(result.total / PAGE_SIZE)) : 1;
  const pageHref = (p: number) => {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) for (const x of many(v)) if (k !== "page") q.append(k, x);
    if (p > 1) q.set("page", String(p));
    const s = q.toString();
    return s ? `/meals?${s}` : "/meals";
  };

  return (
    <Container className="py-10 md:py-14">
      <h1 className="type-h1">Explore meals</h1>
      <p className="type-body-lg mt-4 max-w-[44rem] text-muted">
        Each Mise kit brings the measured ingredients and a recipe card. You do the cooking.
      </p>

      <FilterForm className="mt-10 border-y border-border py-6">
        <div className="max-w-[36rem]">
          <label htmlFor="q" className="type-label mb-1.5 block">
            Search
          </label>
          <div className="relative">
            <Input id="q" name="q" type="search" defaultValue={filter.q} placeholder="Curry, plantain, oxtail…" className="pr-12" maxLength={100} />
            <button type="submit" className="absolute right-1 top-1/2 inline-flex size-10 -translate-y-1/2 items-center justify-center rounded-md text-muted hover:text-foreground">
              <Search aria-hidden="true" className="size-5" strokeWidth={1.75} />
              <span className="sr-only">Search meals</span>
            </button>
          </div>
        </div>
        <FilterPanel active={active}>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4 lg:items-end">
          <FilterSelect id="category" label="Category" value={filter.category} options={categories.items.map((c) => ({ key: c.slug, label: c.name }))} />
          <FilterSelect id="max_minutes" label="Cooking time" value={filter.max_minutes ? String(filter.max_minutes) : ""} options={times} any="Any time" />
          <FilterSelect id="protein" label="Protein" value={filter.protein} options={ref.proteins} />
          <FilterSelect id="sort" label="Sort by" value={filter.sort ?? ""} options={[{ key: "quickest", label: "Quickest first" }, { key: "name", label: "Name" }]} any="Newest first" />
        </div>
        <fieldset className="mt-5">
          <legend className="type-label mb-2">Dietary</legend>
          <div className="flex flex-wrap gap-2">
            {ref.dietary.map((d) => (
              <Choice key={d.key} name="diet" value={d.key} label={d.label} defaultChecked={filter.diet?.includes(d.key)} />
            ))}
          </div>
        </fieldset>
        {chefs.items.length > 1 ? (
          <div className="mt-5 max-w-[20rem]">
            <FilterSelect id="chef" label="Chef" value={filter.chef} options={chefs.items.map((c) => ({ key: c.slug, label: c.name }))} />
          </div>
        ) : null}
        <div className="mt-5 flex flex-wrap items-center gap-x-6 gap-y-3">
          {token ? <Choice name="allergies" value="mine" label="Hide meals with my allergens" defaultChecked={mine} /> : null}
          <noscript>
            <button type="submit" className={buttonClasses("secondary")}>
              Apply filters
            </button>
          </noscript>
          {filtered ? (
            <Link href="/meals" data-clear-filters className="type-body-sm font-semibold underline underline-offset-4">
              Clear all filters
            </Link>
          ) : null}
        </div>
        {mine ? (
          <p className="type-caption mt-4 max-w-[52rem] border-l-2 border-warning pl-3">
            {myAllergens.length
              ? "Hidden: meals that list one of your allergens, and meals whose allergens are not confirmed yet. "
              : "You have not told us about any allergies, so nothing is hidden. "}
            {ref.cross_contact_notice}
          </p>
        ) : null}
        </FilterPanel>
      </FilterForm>

      <section aria-labelledby="results" className="mt-10">
        <h2 id="results" className="sr-only">
          Meals
        </h2>
        {invalid ? (
          <EmptyState title="Those filters do not work together." action={<Link href="/meals" className={buttonClasses("secondary")}>Clear filters</Link>} />
        ) : result && result.items.length ? (
          <>
            <p className="type-body-sm mb-8 text-muted" aria-live="polite">
              {result.total} {result.total === 1 ? "meal" : "meals"}
              {filtered ? " match your filters" : ""}
            </p>
            <MealGrid>
              {result.items.map((m, i) => (
                <MealCard key={m.id} meal={m} saved={saved ? saved.has(m.id) : null} priority={i < 3 && page === 1} />
              ))}
            </MealGrid>
            {pages > 1 ? (
              <nav aria-label="Pages" className="mt-14 flex items-center justify-between border-t border-border pt-6">
                {page > 1 ? (
                  <Link href={pageHref(page - 1)} className="type-body-sm font-semibold underline underline-offset-4">
                    Previous page
                  </Link>
                ) : (
                  <span />
                )}
                <span className="type-caption type-figures text-muted">
                  Page {page} of {pages}
                </span>
                {page < pages ? (
                  <Link href={pageHref(page + 1)} className="type-body-sm font-semibold underline underline-offset-4">
                    Next page
                  </Link>
                ) : (
                  <span />
                )}
              </nav>
            ) : null}
          </>
        ) : filtered ? (
          <EmptyState
            title="No meals match those filters."
            action={
              <Link href="/meals" className={buttonClasses("secondary")}>
                Clear all filters
              </Link>
            }
          >
            Try fewer filters, or a different search.
          </EmptyState>
        ) : (
          <EmptyState title="New meals are coming soon.">
            The first menus are being written and tested in our kitchen. They will appear here as soon as they are ready.
          </EmptyState>
        )}
      </section>
    </Container>
  );
}

function FilterSelect({
  id,
  label,
  value,
  options,
  any = "All",
}: {
  id: string;
  label: string;
  value?: string;
  options: { key: string; label: string }[];
  any?: string;
}) {
  return (
    <div>
      <label htmlFor={id} className="type-label mb-1.5 block">
        {label}
      </label>
      <Select id={id} name={id} defaultValue={value ?? ""}>
        <option value="">{any}</option>
        {options.map((o) => (
          <option key={o.key} value={o.key}>
            {o.label}
          </option>
        ))}
      </Select>
    </div>
  );
}
