import { ApiError, type ProcurementLine, type ProcurementRun } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay, humanize, money, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { decide, reserve, runAction } from "../../supply-actions";

export const metadata: Metadata = { title: "Procurement run" };

export default async function RunPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "procurement.read")) notFound();
  const { id } = await params;
  const r = await api.supply.run(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const plan = can(admin, "procurement.plan") && r.status === "open";
  // Buy decisions not yet on an order (drafting again would make nothing).
  const buys = (r.plan_lines ?? []).filter((l) => l.decision === "buy" && l.purchase_orders.every((po) => po.status === "cancelled")).length;
  return (
    <>
      <PageHeader
        back={{ href: "/procurement", label: "All runs" }}
        title={`Run ${r.number}`}
        description={
          <>
            <StatusTag status={r.status === "open" ? "in_progress" : r.status} label={r.status === "open" ? "Open" : undefined} /> · production plan {r.plan_number} (demand revision{" "}
            {r.snapshot_revision}) · ingredients needed by {formatDay(r.required_by)} · {r.created_by_name}, {formatDateTime(r.created_at)}
            {r.approved_by_name ? ` · decisions approved by ${r.approved_by_name}` : ""}
          </>
        }
        actions={
          <Link href={`/quotations?run=${r.id}`} className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
            Ask for quotations
          </Link>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <Notice tone="warning" title="For your attention" items={r.warnings} />
        {(r.plan_lines ?? []).map((l) => (
          <LineCard key={l.id} r={r} l={l} plan={plan} reserveOk={can(admin, "inventory.reserve")} />
        ))}
        <Panel title="Gross requirements as released" note="Copied from the production plan when the run was made; never edited. Each preparation separately, with the batches it is for.">
          <DataTable head={["Ingredient", "Preparation", "Quantity", "Status"]} minWidth="34rem">
            {(r.requirements ?? []).map((q, i) => (
              <tr key={i}>
                <td className="p-3 font-semibold">{q.ingredient_name}</td>
                <td className="p-3">{q.preparation || "—"}</td>
                <td className="p-3">{quantity(q.quantity, q.unit)}</td>
                <td className="p-3">
                  <StatusTag status={q.status} />
                  {q.approximate ? <span className="type-caption block text-muted">Includes an average conversion.</span> : null}
                </td>
              </tr>
            ))}
          </DataTable>
        </Panel>
        <RunActions r={r} buys={buys} approve={can(admin, "procurement.approve")} orders={can(admin, "purchase_orders.create")} />
      </div>
    </>
  );
}

function LineCard({ r, l, plan, reserveOk }: { r: ProcurementRun; l: ProcurementLine; plan: boolean; reserveOk: boolean }) {
  const short = l.shortage != null && Number(l.shortage) > 0;
  const toReserve = l.gross != null ? Math.max(0, Number(l.gross) - Number(l.reserved) - Number(l.issued)) : 0;
  return (
    <section className="border border-border bg-surface p-5" aria-label={l.ingredient_name}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <h2 className="font-display text-[1.5rem] leading-tight">{l.ingredient_name}</h2>
        <div className="flex flex-wrap gap-2">
          {l.incomplete ? <StatusTag status="failed" label="Incomplete" /> : short ? <StatusTag status="failed" label={`Short ${quantity(l.shortage, l.unit)}`} /> : <StatusTag status="ok" label="Covered" />}
          <StatusTag status={l.decision === "pending" ? "pending" : l.decision === "buy" ? "approved" : "draft"} label={humanize(l.decision)} />
        </div>
      </div>
      <dl className="type-body-sm mt-3 grid grid-cols-2 gap-3 sm:grid-cols-6">
        {[
          ["Needed", quantity(l.gross, l.unit)],
          ["Reserved for this plan", quantity(l.reserved, l.unit)],
          ["Issued to its batches", quantity(l.issued, l.unit)],
          ["On order (in time)", quantity(l.inbound, l.unit)],
          ["Short", l.shortage == null ? "Unknown" : quantity(l.shortage, l.unit)],
          ["Usable, not reserved", quantity(l.available, l.unit)],
        ].map(([k, v]) => (
          <div key={k}>
            <dt className="type-caption text-muted">{k}</dt>
            <dd className="font-semibold">{v}</dd>
          </div>
        ))}
      </dl>
      <details className="mt-3">
        <summary className="type-body-sm cursor-pointer font-semibold underline underline-offset-4">Why this quantity</summary>
        <ul className="type-caption mt-2 grid list-disc gap-1 pl-5">
          {l.explain.map((e, i) => (
            <li key={i}>{e}</li>
          ))}
        </ul>
      </details>
      {reserveOk && Number(l.available) > 0 && toReserve > 0 ? (
        <div className="mt-4">
          <KForm action={reserve} submit={`Reserve ${quantity(String(Math.min(toReserve, Number(l.available))), l.unit)}`} pending="Reserving…" label={`Reserve ${l.ingredient_name}`} variant="secondary" inline>
            <input type="hidden" name="plan_id" value={r.plan_id} />
            <input type="hidden" name="ingredient_id" value={l.ingredient_id} />
            <input type="hidden" name="quantity" value={String(Math.min(toReserve, Number(l.available)))} />
            <input type="hidden" name="back" value={`/procurement/${r.id}`} />
          </KForm>
        </div>
      ) : null}
      {l.purchase_orders.length ? (
        <p className="type-body-sm mt-3">
          On order:{" "}
          {l.purchase_orders.map((po, i) => (
            <span key={po.po_id}>
              {i ? ", " : ""}
              <Link href={`/purchase-orders/${po.po_id}`} className="underline underline-offset-4">
                {po.number}
              </Link>{" "}
              ({po.packs} packs, {humanize(po.status)}
              {po.expected_delivery ? `, expected ${formatDay(po.expected_delivery)}` : ""})
            </span>
          ))}
        </p>
      ) : null}
      {l.decision !== "pending" ? (
        <p className="type-body-sm mt-3 border-l-2 border-border-strong pl-3">
          Decision: {humanize(l.decision)}
          {l.packs ? ` · ${l.packs} packs` : ""}
          {l.decision_note ? ` · “${l.decision_note}”` : ""}
          {l.decided_by_name ? ` · ${l.decided_by_name}` : ""}
        </p>
      ) : null}
      {plan && (short || l.incomplete || l.decision !== "pending") ? (
        <div className="mt-4">
          <KForm action={decide} submit="Record decision" pending="Saving…" label={`Decision for ${l.ingredient_name}`}>
            <input type="hidden" name="run_id" value={r.id} />
            <input type="hidden" name="line_id" value={l.id} />
            {l.options.length ? (
              <DataTable head={["", "Supplier option", "Packs", "Cost", "Lead time", "Notes"]} minWidth="44rem">
                {l.options.map((o) => (
                  <tr key={o.product_id}>
                    <td className="p-3">
                      <input
                        type="radio"
                        name="choice"
                        value={o.product_id}
                        defaultChecked={l.product_id === o.product_id || l.suggested_product_id === o.product_id}
                        aria-label={`Buy ${o.product_name} from ${o.supplier_name}`}
                        className="size-5"
                      />
                    </td>
                    <td className="p-3">
                      <span className="font-semibold">{o.supplier_name}</span>
                      <span className="type-caption block">
                        {o.product_name} · {o.pack_quantity} {o.pack_unit} a pack
                      </span>
                    </td>
                    <td className="p-3">
                      <TextInput name={`packs_${o.product_id}`} inputMode="numeric" defaultValue={l.product_id === o.product_id && l.packs ? l.packs : o.packs} className="w-20" aria-label={`Packs from ${o.supplier_name}`} />
                      <span className="type-caption block text-muted">
                        = {quantity(o.quantity, l.unit)}, min {o.min_order_packs}
                      </span>
                    </td>
                    <td className="p-3">{o.cost_minor != null && o.currency ? money(o.cost_minor, o.currency) : "—"}</td>
                    <td className="p-3">
                      {o.lead_time_days != null ? `${o.lead_time_days} days` : "—"}
                      {o.lead_time_risk ? <StatusTag status="failed" label="Late" /> : null}
                    </td>
                    <td className="type-caption p-3">{o.notes.join(" ") || "—"}</td>
                  </tr>
                ))}
              </DataTable>
            ) : (
              <p className="type-body-sm text-muted">No approved supplier offers this ingredient with a confirmed mapping. Record a supplier product, or ask for quotations.</p>
            )}
            {l.suggested_product_id ? <p className="type-caption text-muted">Pre-selected because it is the only option, not because of its price.</p> : null}
            <div className="flex flex-wrap gap-4">
              {[
                ["no_purchase", "Do not buy"],
                ["deferred", "Decide later"],
              ].map(([v, label]) => (
                <label key={v} className="type-body-sm inline-flex min-h-11 items-center gap-2">
                  <input type="radio" name="choice" value={v} defaultChecked={l.decision === v} className="size-5" /> {label}
                </label>
              ))}
            </div>
            <Labelled label="Why (required not to buy or to defer)">
              <TextInput name="note" defaultValue={l.decision_note} maxLength={1000} />
            </Labelled>
          </KForm>
        </div>
      ) : null}
    </section>
  );
}

function RunActions({ r, buys, approve, orders }: { r: ProcurementRun; buys: number; approve: boolean; orders: boolean }) {
  const hidden = (action: string) => (
    <>
      <input type="hidden" name="id" value={r.id} />
      <input type="hidden" name="action" value={action} />
    </>
  );
  return (
    <div className="grid gap-6 [&>*]:min-w-0 lg:grid-cols-2">
      {approve && r.status === "open" ? (
        <Panel title="Approve the purchasing decisions" note="Every short or incomplete ingredient needs a decision. Approved decisions only change if the run is reopened with a reason.">
          <KForm action={runAction} submit="Approve decisions" pending="Approving…" label="Approve decisions" size="lg">
            {hidden("approve")}
          </KForm>
        </Panel>
      ) : null}
      {orders && r.status === "approved" && buys > 0 ? (
        <Panel title="Purchase orders" note="One draft per supplier for the buy decisions not already on an order. Drafts are approved by someone other than their buyer.">
          <KForm action={runAction} submit="Draft purchase orders" pending="Drafting…" label="Draft purchase orders" size="lg">
            {hidden("orders")}
          </KForm>
          <Link href={`/purchase-orders?run_id=${r.id}`} className="type-body-sm mt-3 inline-flex min-h-11 items-center underline underline-offset-4">
            Orders for this run
          </Link>
        </Panel>
      ) : null}
      {approve && r.status === "approved" ? (
        <Panel title="Change the decisions">
          <KForm action={runAction} submit="Reopen" pending="Saving…" label="Reopen run" variant="secondary">
            {hidden("reopen")}
            <Labelled label="Reason">
              <TextInput name="reason" required maxLength={500} />
            </Labelled>
          </KForm>
        </Panel>
      ) : null}
    </div>
  );
}
