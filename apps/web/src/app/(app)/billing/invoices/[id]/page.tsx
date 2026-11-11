import { ApiError } from "@mise/api-client";
import { buttonClasses, Tag } from "@mise/ui";
import { ArrowLeft, Download } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { formatMoney, shortDate } from "@/lib/commerce";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Invoice" };

export default async function InvoicePage({ params }: { params: Promise<{ id: string }> }) {
  const { token } = await requireCustomer();
  const { id } = await params;
  const inv = await api.commerce.invoice(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const tone = inv.status === "paid" ? "success" : inv.status === "void" ? "neutral" : "accent";
  return (
    <>
      <Link href="/billing" className="type-body-sm inline-flex min-h-11 items-center gap-2 font-semibold hover:underline">
        <ArrowLeft aria-hidden="true" className="size-4" strokeWidth={1.75} /> Billing
      </Link>
      <article className="mt-4 max-w-[46rem] border border-border bg-surface p-6 md:p-10">
        <header className="flex flex-wrap items-start justify-between gap-4 border-b-2 border-primary pb-5">
          <div>
            <p className="font-display text-[1.75rem] text-primary">Mise.</p>
            <p className="type-caption mt-1 text-muted">{inv.business.name}</p>
            {inv.business.address ? <p className="type-caption whitespace-pre-line text-muted">{inv.business.address}</p> : null}
            {inv.business.tax_registration ? <p className="type-caption text-muted">Tax registration: {inv.business.tax_registration}</p> : null}
          </div>
          <div className="text-right">
            <h1 className="type-h3">{inv.status === "paid" ? "Invoice and receipt" : "Invoice"}</h1>
            <p className="type-body-sm mt-1">{inv.number}</p>
            <div className="mt-2">
              <Tag tone={tone}>{inv.status === "paid" ? "Paid" : inv.status === "void" ? "Void" : "Open"}</Tag>
            </div>
          </div>
        </header>
        <dl className="type-body-sm mt-6 grid gap-4 sm:grid-cols-3">
          <div>
            <dt className="type-caption font-semibold uppercase tracking-[0.08em] text-muted">Billed to</dt>
            <dd className="mt-1">
              {inv.customer.name}
              <br />
              {inv.customer.email}
              {inv.customer.address.map((l) => (
                <span key={l} className="block text-muted">
                  {l}
                </span>
              ))}
            </dd>
          </div>
          <div>
            <dt className="type-caption font-semibold uppercase tracking-[0.08em] text-muted">Issued</dt>
            <dd className="mt-1">{shortDate(inv.issued_at)}</dd>
          </div>
          <div>
            <dt className="type-caption font-semibold uppercase tracking-[0.08em] text-muted">Delivery week</dt>
            <dd className="mt-1">
              {shortDate(inv.period_start)} – {shortDate(inv.period_end)}
            </dd>
          </div>
        </dl>
        <table className="type-body-sm type-figures mt-8 w-full">
          <thead>
            <tr className="border-b border-foreground text-left">
              <th scope="col" className="py-2 font-semibold">Description</th>
              <th scope="col" className="py-2 text-right font-semibold">Amount</th>
            </tr>
          </thead>
          <tbody>
            {inv.lines.map((l, i) => (
              <tr key={i} className="border-b border-border">
                <td className="py-3">{l.description}</td>
                <td className="py-3 text-right">{formatMoney(l.amount_minor, inv.currency)}</td>
              </tr>
            ))}
          </tbody>
          <tfoot>
            {inv.tax.mode === "inclusive" ? (
              <tr>
                <td className="pt-3 text-muted">{inv.tax.label} included</td>
                <td className="pt-3 text-right text-muted">{formatMoney(inv.tax_minor, inv.currency)}</td>
              </tr>
            ) : null}
            <tr>
              <th scope="row" className="pt-3 text-left font-semibold">
                Total
              </th>
              <td className="pt-3 text-right font-display text-[1.5rem]">{formatMoney(inv.total_minor, inv.currency)}</td>
            </tr>
          </tfoot>
        </table>
        <p className="type-caption mt-8 text-muted">{inv.tax_note}</p>
        {inv.paid_at ? <p className="type-caption text-muted">Paid on {shortDate(inv.paid_at)}.</p> : null}
      </article>
      <a href={`/billing/invoices/${inv.id}/pdf`} className={buttonClasses("secondary", "md", "mt-6")}>
        <Download aria-hidden="true" className="size-4" strokeWidth={1.75} /> Download PDF
      </a>
    </>
  );
}
