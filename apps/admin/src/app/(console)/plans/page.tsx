import { Heading, Tag, Text } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { CatalogForm } from "@/components/catalog-form";
import { DataTable, Empty, Panel } from "@/components/commerce-ui";
import { PlanFields } from "@/components/plan-fields";
import { commerceTone, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createPlan } from "../commerce-actions";

export const metadata: Metadata = { title: "Plans" };

/**
 * Subscription plans. Prices are versions: a new price applies to new sales
 * from its start, and existing subscribers keep the price they bought at.
 */
export default async function PlansPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "subscriptions.read")) notFound();
  const { items } = await api.admin.plans(token);
  return (
    <>
      <Heading as="h1" size="h2">
        Plans
      </Heading>
      <Text className="mt-3 max-w-[48rem]" tone="muted">
        The business-plan figures are not approved prices: they exist only as demo plans in local development. Real plans are created here,
        start as drafts, and need a price before they can be activated.
      </Text>
      <div className="mt-6">
        {items.length ? (
          <DataTable head={["Plan", "Status", "Meals", "Price now", "Next price", "Online"]}>
            {items.map((p) => (
              <tr key={p.id}>
                <td className="p-3">
                  <Link href={`/plans/${p.id}`} className="font-semibold underline underline-offset-4">
                    {p.name}
                  </Link>
                  {p.is_demo ? (
                    <span className="ml-2">
                      <Tag tone="warning">Demo</Tag>
                    </span>
                  ) : null}
                </td>
                <td className="p-3">
                  <Tag tone={commerceTone(p.status)}>{humanize(p.status)}</Tag>
                </td>
                <td className="p-3">{p.meals_per_week} a week</td>
                <td className="p-3">{p.price ? money(p.price.amount_minor, p.price.currency) : "None"}</td>
                <td className="p-3">{p.next_price ? `${p.next_price.formatted} from ${new Date(p.next_price.effective_from).toLocaleDateString("en-GB")}` : ""}</td>
                <td className="p-3">{p.self_serve ? "Yes" : "Arranged by the team"}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <Empty>No plans yet.</Empty>
        )}
      </div>
      {can(admin, "plans.manage") ? (
        <Panel title="New plan" className="mt-10 max-w-[52rem]">
          <CatalogForm action={createPlan} submit="Create draft plan" pending="Creating…" label="New plan">
            <PlanFields />
          </CatalogForm>
        </Panel>
      ) : null}
    </>
  );
}
