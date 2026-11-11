import { Heading } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { MealEditor } from "@/components/meal-editor";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "New meal" };

export default async function NewMealPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "catalog.manage")) notFound();
  const [reference, categories, chefs] = await Promise.all([
    api.catalog.reference(),
    api.admin.catalogCategories(token),
    api.admin.catalogChefs(token),
  ]);
  return (
    <>
      <Link href="/meals" className="type-body-sm underline underline-offset-4">
        All meals
      </Link>
      <Heading as="h1" size="h2" className="mt-3">
        New meal
      </Heading>
      <p className="type-body-sm mt-2 text-muted">It is saved as a draft. Customers see nothing until it is published.</p>
      <div className="mt-8 max-w-[52rem] border border-border bg-surface p-5 sm:p-6">
        {categories.items.length ? (
          <MealEditor reference={reference} categories={categories.items} chefs={chefs.items} />
        ) : (
          <p className="type-body-sm">
            Add a category first, on the{" "}
            <Link href="/meals/setup" className="underline underline-offset-4">
              categories and chefs
            </Link>{" "}
            page.
          </p>
        )}
      </div>
    </>
  );
}
