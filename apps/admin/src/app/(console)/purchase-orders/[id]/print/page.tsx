import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { formatDateTime, formatDay, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Purchase order (print)" };

/**
 * A plain, printable purchase order to send to the supplier however they
 * prefer (paper, PDF from the browser's print dialog, a photo on WhatsApp).
 */
export default async function PrintPurchaseOrder({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "purchase_orders.read")) notFound();
  const { id } = await params;
  const [po, settings] = await Promise.all([
    api.supply.purchaseOrder(token, id).catch((e) => {
      if (e instanceof ApiError && e.status === 404) notFound();
      throw e;
    }),
    api.admin.settings(token).catch(() => null),
  ]);
  const business = settings?.editable.find((s) => s.key === "business.display_name")?.value || "Mise";
  return (
    <article className="mx-auto max-w-[48rem] bg-white p-8 text-black print:p-0">
      <p className="type-caption print:hidden">
        <Link href={`/purchase-orders/${po.id}`} className="underline underline-offset-4">
          Back to the order
        </Link>{" "}
        · Use your browser’s print dialog to print or save as PDF.
      </p>
      <header className="mt-4 flex flex-wrap items-start justify-between gap-4 border-b-2 border-black pb-4">
        <div>
          <h1 className="font-display text-[2rem] leading-tight">Purchase order {po.number}</h1>
          <p className="type-body-sm">
            Revision {po.revision}
            {po.approved_at ? ` · approved ${formatDateTime(po.approved_at)}` : " · not yet approved"}
          </p>
        </div>
        <p className="type-body-sm text-right">
          <strong>{business}</strong>
          <br />
          Buyer: {po.buyer_name || "—"}
        </p>
      </header>
      {po.status === "draft" || po.status === "pending_approval" ? (
        <p className="type-body-sm mt-3 border-2 border-black p-2 font-semibold">Draft: not approved, not an order.</p>
      ) : null}
      <section className="type-body-sm mt-4 grid gap-1">
        <p>
          <strong>Supplier:</strong> {po.supplier_name}
        </p>
        <p>
          <strong>Deliver by:</strong> {po.expected_delivery ? formatDay(po.expected_delivery) : "to be agreed"}
          {po.location_name ? ` · to ${po.location_name}` : ""}
        </p>
      </section>
      <table className="type-body-sm mt-4 w-full border-collapse">
        <thead>
          <tr className="border-b border-black text-left">
            <th className="py-2">Item</th>
            <th className="py-2">Packs</th>
            <th className="py-2">Pack</th>
            <th className="py-2 text-right">Price per pack</th>
            <th className="py-2 text-right">Total</th>
          </tr>
        </thead>
        <tbody>
          {(po.lines ?? []).map((l) => (
            <tr key={l.id} className="border-b border-black/30">
              <td className="py-2">{l.description}</td>
              <td className="py-2">{l.packs}</td>
              <td className="py-2">
                {l.pack_quantity} {l.pack_unit}
              </td>
              <td className="py-2 text-right">{l.unit_price_minor != null ? money(l.unit_price_minor, po.currency) : "—"}</td>
              <td className="py-2 text-right">{l.line_total_minor != null ? money(l.line_total_minor, po.currency) : "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <dl className="type-body-sm mt-4 grid justify-end gap-1 text-right">
        <div>Delivery: {money(po.delivery_fee_minor, po.currency)}</div>
        <div className="font-semibold">Total: {money(po.total_minor, po.currency)}</div>
      </dl>
      {po.notes ? <p className="type-body-sm mt-4 whitespace-pre-line">{po.notes}</p> : null}
      <p className="type-caption mt-6">Printed {formatDateTime(new Date().toISOString())}.</p>
    </article>
  );
}
