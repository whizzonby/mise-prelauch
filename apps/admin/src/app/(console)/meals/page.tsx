import { buttonClasses, Heading, Tag, Text } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { formatDate } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Meals" };

const STATUSES = ["draft", "published", "unpublished"] as const;
const tone = { draft: "neutral", published: "success", unpublished: "warning" } as const;

/** The customer-facing catalogue, as staff see it: every meal, whatever its status. */
export default async function MealsPage({ searchParams }: { searchParams: Promise<{ status?: string; q?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "catalog.read")) notFound();
  const { status, q } = await searchParams;
  const valid = STATUSES.find((s) => s === status);
  const meals = await api.admin.catalogMeals(token, { status: valid, q });

  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Heading as="h1" size="h2">
          Meals
        </Heading>
        <div className="flex flex-wrap gap-3">
          <Link href="/meals/setup" className={buttonClasses("secondary")}>
            Categories and chefs
          </Link>
          {can(admin, "catalog.manage") ? (
            <Link href="/meals/new" className={buttonClasses("primary")}>
              New meal
            </Link>
          ) : null}
        </div>
      </div>
      <Text className="mt-3 max-w-[48rem]" tone="muted">
        What customers see in the meal catalogue. Drafts and unpublished meals are visible only here. Nutrition reaches
        customers only once a nutritionist verifies it.
      </Text>

      <form className="mt-6 flex flex-wrap items-end gap-3" role="search">
        <label className="grid gap-1.5">
          <span className="type-label">Name contains</span>
          <input name="q" defaultValue={q} className="min-h-11 rounded-md border border-border-strong bg-surface px-3" />
        </label>
        <label className="grid gap-1.5">
          <span className="type-label">Status</span>
          <select name="status" defaultValue={valid ?? ""} className="min-h-11 rounded-md border border-border-strong bg-surface px-3">
            <option value="">Any</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {s[0]!.toUpperCase() + s.slice(1)}
              </option>
            ))}
          </select>
        </label>
        <button type="submit" className={buttonClasses("secondary")}>
          Filter
        </button>
      </form>

      <div className="mt-6 overflow-x-auto border border-border bg-surface">
        <table className="w-full min-w-[44rem] text-left">
          <thead className="type-caption border-b border-border text-muted">
            <tr>
              <th className="p-3 font-semibold">Meal</th>
              <th className="p-3 font-semibold">Status</th>
              <th className="p-3 font-semibold">Category</th>
              <th className="p-3 font-semibold">Allergens</th>
              <th className="p-3 font-semibold">Nutrition</th>
              <th className="p-3 font-semibold">Changed</th>
            </tr>
          </thead>
          <tbody className="type-body-sm divide-y divide-border">
            {meals.items.map((m) => (
              <tr key={m.id}>
                <td className="p-3">
                  <Link href={`/meals/${m.id}`} className="font-semibold underline underline-offset-4">
                    {m.name}
                  </Link>
                  {m.is_demo ? (
                    <span className="ml-2">
                      <Tag tone="warning">Demo</Tag>
                    </span>
                  ) : null}
                </td>
                <td className="p-3">
                  <Tag tone={tone[m.status]}>{m.status}</Tag>
                </td>
                <td className="p-3">{m.category_name}</td>
                <td className="p-3">{m.allergen_status === "declared" ? "Declared" : "Not confirmed"}</td>
                <td className="p-3">{m.nutrition_status === "verified" ? "Verified" : m.nutrition_status === "pending" ? "Pending" : "None"}</td>
                <td className="p-3 text-muted">{formatDate(m.updated_at)}</td>
              </tr>
            ))}
            {meals.items.length === 0 ? (
              <tr>
                <td colSpan={6} className="p-6 text-muted">
                  No meals {valid || q ? "match these filters" : "yet"}.
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
      <p className="type-caption mt-3 text-muted">{meals.total} in total.</p>
    </>
  );
}
