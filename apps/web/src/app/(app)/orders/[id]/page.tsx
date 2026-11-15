import { ApiError } from "@mise/api-client";
import { Tag } from "@mise/ui";
import { ArrowLeft } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { formatMoney, shortDate } from "@/lib/commerce";
import { orderStatus } from "@/lib/order-status";
import { api, requireCustomer } from "@/lib/session";

import { DeliverySection } from "./delivery-section";

export const metadata: Metadata = { title: "Order" };



/**
 * One order, as it was made: the meals and versions, the amounts and the
 * address at the time, and its delivery: only milestones that have
 * actually happened, in plain words.
 */
export default async function OrderPage({ params }: { params: Promise<{ id: string }> }) {
  const { token } = await requireCustomer();
  const { id } = await params;
  const order = await api.commerce.order(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const delivery = await api.commerce.orderDelivery(token, id).catch(() => null);
  const a = order.address;
  return (
    <>
      <Link href="/orders" className="type-body-sm inline-flex min-h-11 items-center gap-2 font-semibold hover:underline">
        <ArrowLeft aria-hidden="true" className="size-4" strokeWidth={1.75} /> All orders
      </Link>
      <div className="mt-4 flex flex-wrap items-center gap-3">
        <h1 className="type-h2">Week of {shortDate(order.week)}</h1>
        <Tag tone={orderStatus(order.status, order.delivery_outcome?.state).tone}>{orderStatus(order.status, order.delivery_outcome?.state).label}</Tag>
      </div>
      <p className="type-body-sm mt-2 text-muted">
        Order {order.number} · {order.plan_name} plan
      </p>

      {delivery ? (
        <div className="mt-8 max-w-[40rem]">
          <DeliverySection orderId={order.id} d={delivery} />
        </div>
      ) : null}

      <div className="mt-8 grid gap-10 lg:grid-cols-[1fr_20rem]">
        <section aria-labelledby="meals">
          <h2 id="meals" className="type-h3">
            Your {order.meals_count} meals
          </h2>
          <ul className="mt-4 divide-y divide-border border-y border-border">
            {order.items.map((it) => (
              <li key={it.meal_id} className="flex items-center justify-between gap-4 py-3">
                <span>
                  <Link href={`/meals/${it.slug}`} className="type-h4 hover:underline">
                    {it.name}
                  </Link>
                  <span className="type-caption block text-muted">
                    Serves {it.servings}
                    {it.allergen_status === "declared" && it.allergens.length ? ` · Contains ${it.allergens.join(", ")}` : ""}
                  </span>
                </span>
                <span className="type-body type-figures font-semibold">×{it.quantity}</span>
              </li>
            ))}
          </ul>
          <p className="type-caption mt-4 text-muted">Meal kits are included in your plan&apos;s weekly price.</p>
        </section>
        <aside className="grid content-start gap-8">
          <section aria-labelledby="paid">
            <h2 id="paid" className="type-h4">
              Paid
            </h2>
            <dl className="type-body-sm type-figures mt-2 grid grid-cols-[1fr_auto] gap-y-1.5">
              <dt className="text-muted">Plan, one week</dt>
              <dd>{formatMoney(order.subtotal_minor, order.currency)}</dd>
              {order.discount_minor > 0 ? (
                <>
                  <dt className="text-muted">Discount</dt>
                  <dd>{formatMoney(-order.discount_minor, order.currency)}</dd>
                </>
              ) : null}
              <dt className="text-muted">Delivery</dt>
              <dd>{formatMoney(order.delivery_fee_minor, order.currency)}</dd>
              <dt className="font-semibold">Total</dt>
              <dd className="font-semibold">{formatMoney(order.total_minor, order.currency)}</dd>
            </dl>
          </section>
          <section aria-labelledby="deliver">
            <h2 id="deliver" className="type-h4">
              Delivery address
            </h2>
            <address className="type-body-sm mt-2 not-italic text-muted">
              {[a.recipient_name, a.line1, a.line2, a.area, a.city, a.region_label].filter(Boolean).join(", ")}
            </address>
          </section>
        </aside>
      </div>
    </>
  );
}
