import { buttonClasses, Heading, Tag, Text } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { DataTable, Empty, Panel, TextInput } from "@/components/commerce-ui";
import { commerceTone, formatDay, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Orders" };

/**
 * Orders for a delivery week, and the week's demand: kits per meal version
 * from confirmed orders. This is what the future kitchen and procurement
 * modules will read; nothing here plans production.
 */
export default async function OrdersPage({ searchParams }: { searchParams: Promise<{ week?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "orders.read")) notFound();
  const { week } = await searchParams;
  const demand = await api.admin.demand(token, week);
  const { items } = await api.admin.orders(token, { week: demand.week });
  return (
    <>
      <Heading as="h1" size="h2">
        Orders
      </Heading>
      <Text className="mt-3 max-w-[48rem]" tone="muted">
        Orders are made when a week&apos;s menu closes, from paid weeks with confirmed meals.
      </Text>
      <form className="mt-6 flex flex-wrap items-end gap-3">
        <label className="grid gap-1.5">
          <span className="type-label">Delivery week (Monday)</span>
          <TextInput name="week" type="date" defaultValue={demand.week} />
        </label>
        <button type="submit" className={buttonClasses("secondary")}>
          Show
        </button>
      </form>

      <Panel title={`Demand for the week of ${formatDay(demand.week)}`} className="mt-8">
        <p className="type-body-sm mb-4 text-muted">
          {demand.status === "final" ? "Final: the cutoff has passed." : "Provisional: the cutoff hasn't passed yet."} {demand.kits} kits in{" "}
          {demand.orders} orders.
        </p>
        {demand.lines.length ? (
          <DataTable head={["Meal", "Version", "Kits", "Orders", "Customer allergies (orders)"]}>
            {demand.lines.map((l) => (
              <tr key={`${l.meal_id}-${l.meal_version}`}>
                <td className="p-3">{l.meal_name}</td>
                <td className="p-3">v{l.meal_version}</td>
                <td className="p-3 font-semibold">{l.quantity}</td>
                <td className="p-3">{l.orders}</td>
                <td className="p-3">
                  {Object.entries(l.customer_allergies)
                    .map(([k, n]) => `${humanize(k)} (${n})`)
                    .join(", ") || "None declared"}
                </td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <Empty>No confirmed orders for this week.</Empty>
        )}
      </Panel>

      <div className="mt-8">
        {items.length ? (
          <DataTable head={["Order", "Status", "Plan", "Meals", "Total"]}>
            {items.map((o) => (
              <tr key={o.id}>
                <td className="p-3">
                  <Link href={`/orders/${o.id}`} className="font-semibold underline underline-offset-4">
                    {o.number}
                  </Link>
                </td>
                <td className="p-3">
                  <Tag tone={commerceTone(o.status)}>{humanize(o.status)}</Tag>
                  {o.delivery_outcome === "disputed" ? <Tag tone="error">Disputed</Tag> : null}
                </td>
                <td className="p-3">{o.plan_name}</td>
                <td className="p-3">{o.meals_count}</td>
                <td className="p-3">{money(o.total_minor, o.currency)}</td>
              </tr>
            ))}
          </DataTable>
        ) : null}
      </div>
    </>
  );
}
