import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { Panel } from "@/components/commerce-ui";
import { formatDate, formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Goods receipt" };

export default async function ReceiptPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "receiving.read")) notFound();
  const { id } = await params;
  const g = await api.supply.receipt(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  return (
    <>
      <PageHeader
        back={{ href: "/receiving", label: "Goods receiving" }}
        title={`Receipt ${g.number}`}
        description={
          <>
            {g.supplier_name} · order{" "}
            <Link href={`/purchase-orders/${g.po_id}`} className="underline underline-offset-4">
              {g.po_number}
            </Link>{" "}
            · {formatDateTime(g.received_at)} · {g.received_by_name}
            {g.delivery_ref ? ` · delivery note ${g.delivery_ref}` : ""}
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        {g.lines.map((l) => (
          <Panel key={l.id} title={l.description}>
            <dl className="type-body-sm grid grid-cols-2 gap-3 sm:grid-cols-4">
              {[
                ["Delivered", l.delivered_packs],
                ["Accepted", l.accepted_packs],
                ["Quarantined", l.quarantined_packs],
                ["Rejected", l.rejected_packs],
              ].map(([k, v]) => (
                <div key={k}>
                  <dt className="type-caption text-muted">{k} (packs)</dt>
                  <dd className="text-[1.25rem] font-semibold">{v}</dd>
                </div>
              ))}
            </dl>
            <p className="type-body-sm mt-3">
              Supplier lot {l.supplier_lot_ref || "not given"} · expiry {l.expiry_date ? formatDate(l.expiry_date) : "not recorded"}
              {l.temperature_c ? ` · ${l.temperature_c} °C` : ""}
              {l.location_name ? ` · ${l.location_name}` : ""}
            </p>
            {l.discrepancy !== "none" ? (
              <p className="type-body-sm mt-2 border-l-2 border-error pl-3">
                {humanize(l.discrepancy)}: {l.discrepancy_note}
              </p>
            ) : null}
            {l.inspection.length ? (
              <ul className="type-body-sm mt-3 grid gap-1">
                {l.inspection.map((r) => (
                  <li key={r.item_id}>
                    <StatusTag status={r.result === "pass" ? "passed" : r.result === "fail" ? "failed" : "draft"} label={humanize(r.result)} /> {r.name}
                    {r.note ? ` · ${r.note}` : ""}
                  </li>
                ))}
              </ul>
            ) : null}
            {l.lots.length ? (
              <p className="type-body-sm mt-3">
                Lots:{" "}
                {l.lots.map((x, i) => (
                  <span key={x.id}>
                    {i ? ", " : ""}
                    <Link href={`/inventory/lots/${x.id}`} className="font-semibold underline underline-offset-4">
                      {x.code}
                    </Link>{" "}
                    ({humanize(x.status)})
                  </span>
                ))}
              </p>
            ) : null}
          </Panel>
        ))}
        {g.notes ? <Panel title="Notes">{g.notes}</Panel> : null}
      </div>
    </>
  );
}
