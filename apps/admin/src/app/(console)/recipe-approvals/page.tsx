import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { Panel } from "@/components/commerce-ui";
import { RecipeQueue } from "@/components/recipe-queue";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Recipe approvals" };

/** The culinary team's queues: review, final approval, publication. */
export default async function ApprovalsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "recipes.review")) notFound();
  const [culinary, nutrition, final, publish] = await Promise.all([
    api.kitchen.queue(token, "culinary"),
    api.kitchen.queue(token, "nutrition"),
    api.kitchen.queue(token, "final"),
    api.kitchen.queue(token, "publish"),
  ]);
  return (
    <>
      <PageHeader
        title="Recipe approvals"
        description="Chef submits → culinary review → nutrition and allergen sign-off → final approval → publication. Nobody reviews or approves a version they wrote."
      />
      <div className="grid gap-6">
        <Panel title={`Culinary review (${culinary.items.length})`}>
          <RecipeQueue items={culinary.items} empty="Nothing submitted." />
        </Panel>
        <Panel title={`With the nutritionist (${nutrition.items.length})`} note="Waiting for nutrition and allergen sign-off.">
          <RecipeQueue items={nutrition.items} empty="Nothing waiting." />
        </Panel>
        {can(admin, "recipes.approve") ? (
          <Panel title={`Final approval (${final.items.length})`} note="Both sign-offs given.">
            <RecipeQueue items={final.items} empty="Nothing ready for final approval." />
          </Panel>
        ) : null}
        {can(admin, "recipes.publish") ? (
          <Panel title={`Ready to publish (${publish.items.length})`}>
            <RecipeQueue items={publish.items} empty="No approved versions waiting." />
          </Panel>
        ) : null}
      </div>
    </>
  );
}
