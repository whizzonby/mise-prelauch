import { buttonClasses, Tag } from "@mise/ui";

import { orderStatus } from "@/lib/order-status";
import type { Metadata } from "next";
import Link from "next/link";

import { EmptyState } from "@/components/meal-card";
import { formatMoney, shortDate } from "@/lib/commerce";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Orders" };

/** Orders are made when a week's menu closes, from the meals the customer confirmed. */
export default async function OrdersPage() {
  const { token } = await requireCustomer();
  const { items } = await api.commerce.orders(token);
  return (
    <>
      <h1 className="type-h2">Orders</h1>
      <p className="type-body mt-3 max-w-[44rem] text-muted">
        An order is made when a week&apos;s menu closes, from the meals you confirmed for it.
      </p>
      <div className="mt-8">
        {items.length ? (
          <ul className="divide-y divide-border border-y border-border">
            {items.map((o) => (
              <li key={o.id}>
                <Link href={`/orders/${o.id}`} className="grid gap-1 py-4 hover:bg-surface sm:grid-cols-[1fr_auto] sm:items-center sm:px-2">
                  <span>
                    <span className="type-h4 flex flex-wrap items-center gap-2">
                      Week of {shortDate(o.week)}
                      <Tag tone={orderStatus(o.status, o.delivery_outcome).tone}>{orderStatus(o.status, o.delivery_outcome).label}</Tag>
                    </span>
                    <span className="type-caption text-muted">
                      {o.number} · {o.plan_name} · {o.meals_count} meals
                    </span>
                  </span>
                  <span className="type-body type-figures font-semibold">{formatMoney(o.total_minor, o.currency)}</span>
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState
            title="No orders yet."
            action={
              <Link href="/menu" className={buttonClasses("primary")}>
                Choose meals
              </Link>
            }
          >
            Your first order appears here once the menu for your first week closes.
          </EmptyState>
        )}
      </div>
    </>
  );
}
