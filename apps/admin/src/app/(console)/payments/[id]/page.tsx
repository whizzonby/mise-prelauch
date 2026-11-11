import { ApiError } from "@mise/api-client";
import { Heading, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { randomUUID } from "node:crypto";

import { CatalogForm } from "@/components/catalog-form";
import { DataTable, Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { commerceTone, formatDateTime, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { refund } from "../../commerce-actions";

export const metadata: Metadata = { title: "Payment" };

export default async function PaymentPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "payments.read")) notFound();
  const { id } = await params;
  const p = await api.admin.payment(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const refundable = (p.status === "succeeded" || p.status === "partially_refunded") && can(admin, "payments.refund");
  const left = p.amount_minor - p.refunded_minor;
  return (
    <>
      <Link href="/payments" className="type-body-sm underline underline-offset-4">
        All payments
      </Link>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Heading as="h1" size="h2">
          {money(p.amount_minor, p.currency)}
        </Heading>
        <Tag tone={commerceTone(p.status)}>{humanize(p.status)}</Tag>
        {p.test ? <Tag tone="warning">Test provider</Tag> : null}
      </div>
      <p className="type-body-sm mt-2 text-muted">
        {p.kind === "checkout" ? "Paid at checkout" : "Charged automatically"} through {p.provider} · {formatDateTime(p.created_at)}
        {p.provider_ref ? ` · provider reference ${p.provider_ref}` : ""}
      </p>
      {p.needs_attention ? <p className="type-body mt-4 border-l-4 border-error pl-4">{p.attention_reason}</p> : null}
      {p.failure_message ? <p className="type-body-sm mt-2 text-error">{p.failure_message}</p> : null}

      <div className="mt-8 grid gap-8 xl:grid-cols-[1fr_24rem]">
        <div className="grid content-start gap-8">
          <Panel title="Status history">
            <DataTable head={["When", "Change", "Source"]} minWidth="28rem">
              {p.history.map((h, i) => (
                <tr key={i}>
                  <td className="p-3">{formatDateTime(h.created_at)}</td>
                  <td className="p-3">
                    {h.from ? `${humanize(h.from)} → ` : ""}
                    {humanize(h.to)}
                  </td>
                  <td className="p-3">{humanize(h.source)}</td>
                </tr>
              ))}
            </DataTable>
          </Panel>
          {p.refunds.length ? (
            <Panel title="Refunds">
              <DataTable head={["When", "Amount", "Status", "Reason"]} minWidth="28rem">
                {p.refunds.map((r) => (
                  <tr key={r.id}>
                    <td className="p-3">{formatDateTime(r.created_at)}</td>
                    <td className="p-3">{money(r.amount_minor, r.currency)}</td>
                    <td className="p-3">{humanize(r.status)}</td>
                    <td className="p-3">{r.reason}</td>
                  </tr>
                ))}
              </DataTable>
            </Panel>
          ) : null}
        </div>
        {refundable ? (
          <Panel title="Refund">
            <p className="type-body-sm mb-4 text-muted">
              Refunds go through the provider. Up to {money(left, p.currency)} can still be refunded. Refunds never cancel or change the
              subscription.
            </p>
            <CatalogForm action={refund} submit="Refund" pending="Refunding…" label="Refund">
              <input type="hidden" name="id" value={p.id} />
              {/* A fresh key per form shown: submitting this form twice makes one refund. */}
              <input type="hidden" name="request_key" value={randomUUID()} />
              <Labelled label="Amount (TTD)">
                <TextInput name="amount" inputMode="decimal" required placeholder={(left / 100).toFixed(2)} />
              </Labelled>
              <Labelled label="Reason">
                <TextInput name="reason" required minLength={3} maxLength={300} />
              </Labelled>
            </CatalogForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
