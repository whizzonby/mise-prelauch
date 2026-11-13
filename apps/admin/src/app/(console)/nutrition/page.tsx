import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { Panel } from "@/components/commerce-ui";
import { RecipeQueue } from "@/components/recipe-queue";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Nutrition reviews" };

/** The nutritionist's queue. Open a recipe's Review step to sign off or send back. */
export default async function NutritionPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "nutrition.read")) notFound();
  const queue = await api.kitchen.nutritionQueue(token);
  return (
    <>
      <PageHeader
        title="Nutrition reviews"
        description="Recipe versions past the culinary review. A sign-off covers one version only: any later change to ingredients, portions or figures needs a new review. Only reviewed figures are shown to customers as verified."
      />
      <Panel title={`Waiting (${queue.items.length})`} note="Check the ingredients, serving size, every nutrition figure and its source, and the allergen declaration.">
        <RecipeQueue items={queue.items} empty="Nothing waiting for nutrition review." />
      </Panel>
    </>
  );
}
