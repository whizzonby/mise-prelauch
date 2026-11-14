import { ApiError, type PurchaseOrder } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag, Timeline } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice, Options } from "@/components/kitchen-ui";
import { CONTACT_CHANNELS } from "@/components/supply-ui";
import { formatDateTime, formatDay, humanize, money, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { poAction, updatePO } from "../../supply-actions";

export const metadata: Metadata = { title: "Purchase order" };

export default async function PurchaseOrderPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "receiving.read") && !can(admin, "purchase_orders.read")) notFound();
  const { id } = await params;
  const po = await api.supply.purchaseOrder(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const prices = can(admin, "purchase_orders.read");
  const create = can(admin, "purchase_orders.create");
  const editable = create && ["draft", "pending_approval", "approved", "issued", "partially_received"].includes(po.status);
  const [products, locations, receipts] = await Promise.all([
    editable && can(admin, "supplier_products.read") ? api.supply.products(token, { supplier_id: po.supplier_id }) : Promise.resolve(null),
    editable ? api.supply.locations(token) : Promise.resolve(null),
    can(admin, "receiving.read") ? api.supply.receipts(token, po.id) : Promise.resolve(null),
  ]);
  const receivable = ["approved", "issued", "partially_received"].includes(po.status);
  return (
    <>
      <PageHeader
        back={{ href: "/purchase-orders", label: "All purchase orders" }}
        title={`Order ${po.number}`}
        description={
          <>
            <StatusTag status={po.status} /> · {po.supplier_name}
            {!po.supplier_qualified ? " (not an approved supplier)" : ""} · revision {po.revision} · buyer {po.buyer_name || "—"}
            {po.expected_delivery ? ` · expected ${formatDay(po.expected_delivery)}` : ""}
            {po.location_name ? ` · to ${po.location_name}` : ""}
            {po.run_number ? (
              <>
                {" "}
                · for{" "}
                <Link href={`/procurement/${po.run_id}`} className="underline underline-offset-4">
                  {po.run_number}
                </Link>
              </>
            ) : null}
          </>
        }
        actions={
          <div className="flex flex-wrap gap-4">
            {receivable && can(admin, "receiving.manage") ? (
              <Link href={`/receiving/new?po=${po.id}`} className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
                Receive a delivery
              </Link>
            ) : null}
            {prices ? (
              <Link href={`/purchase-orders/${po.id}/print`} className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
                Printable order
              </Link>
            ) : null}
          </div>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <Notice tone="error" title="Before the next step" items={po.blocking} />
        <Panel title="Lines" note={prices ? `Prices per pack, ${po.currency}. ${po.above_limit ? "Above the configured approval limit." : ""}` : "Prices are shown to purchasing only."}>
          <DataTable head={prices ? ["Item", "Packs", "Quantity", "Price per pack", "Line total", "Received", "Accepted"] : ["Item", "Packs", "Quantity", "Received", "Accepted"]} minWidth="44rem">
            {(po.lines ?? []).map((l) => (
              <tr key={l.id}>
                <td className="p-3 font-semibold">{l.description}</td>
                <td className="p-3">{l.packs}</td>
                <td className="p-3">{l.quantity_base && l.base_unit ? quantity(l.quantity_base, l.base_unit) : `${l.packs} × ${l.pack_quantity} ${l.pack_unit}`}</td>
                {prices ? <td className="p-3">{l.unit_price_minor != null ? money(l.unit_price_minor, po.currency) : "—"}</td> : null}
                {prices ? <td className="p-3">{l.line_total_minor != null ? money(l.line_total_minor, po.currency) : "—"}</td> : null}
                <td className="p-3">{l.received_packs}</td>
                <td className="p-3">{l.accepted_packs}</td>
              </tr>
            ))}
          </DataTable>
          {prices ? (
            <dl className="type-body-sm mt-3 grid gap-1">
              <div>Lines: {money(po.subtotal_minor, po.currency)}</div>
              <div>Delivery fee: {money(po.delivery_fee_minor, po.currency)}</div>
              <div>Tax: {money(po.tax_minor, po.currency)} (none configured)</div>
              <div className="font-semibold">Total: {money(po.total_minor, po.currency)}</div>
            </dl>
          ) : null}
        </Panel>
        {editable && products && locations && prices ? <Editor po={po} products={products.items} locations={locations.items} /> : null}
        <Actions po={po} perms={admin.permissions} />
        <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
          <Panel title="History">
            <Timeline
              items={(po.history ?? []).map((h) => ({
                at: h.created_at,
                title: `${h.from && h.from !== h.to ? humanize(h.from) + " → " : ""}${humanize(h.to)}${h.actor_name ? ` · ${h.actor_name}` : ""}`,
                detail: h.note ?? undefined,
              }))}
            />
          </Panel>
          {receipts ? (
            <Panel title="Deliveries received">
              {receipts.items.length ? (
                <ul className="type-body-sm grid gap-2">
                  {receipts.items.map((g) => (
                    <li key={g.id}>
                      <Link href={`/receiving/${g.id}`} className="font-semibold underline underline-offset-4">
                        {g.number}
                      </Link>{" "}
                      · {formatDateTime(g.received_at)} · {g.received_by_name}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="type-body-sm text-muted">Nothing received yet.</p>
              )}
            </Panel>
          ) : null}
        </div>
        {prices && po.revisions?.length ? (
          <Panel title="Approved revisions" note="The order as approved at each revision. Never changed.">
            <ul className="type-body-sm grid gap-2">
              {po.revisions.map((v) => (
                <li key={v.revision}>
                  Revision {v.revision}: {money(v.total_minor, po.currency)}, approved by {v.approved_by_name}, {formatDateTime(v.approved_at)}
                  {v.reason ? ` · ${v.reason}` : ""}
                </li>
              ))}
            </ul>
          </Panel>
        ) : null}
      </div>
    </>
  );
}

function Editor({
  po,
  products,
  locations,
}: {
  po: PurchaseOrder;
  products: { id: string; name: string; pack_quantity: string; pack_unit: string; mapping_status: string; active: boolean; kind: string; price?: { amount_minor: number } | null }[];
  locations: { id: string; name: string; type: string; active: boolean }[];
}) {
  const approved = po.status !== "draft" && po.status !== "pending_approval";
  const options = products.filter((p) => p.active && (p.kind === "packaging" || p.mapping_status === "confirmed"));
  const rows = [...(po.lines ?? []).map((l) => ({ product: l.product_id, packs: l.packs, price: l.unit_price_minor, procurement: l.procurement_line_id ?? "" })), ...Array.from({ length: 3 }, () => null)];
  return (
    <Panel
      title={approved ? "Change this approved order" : "Edit the order"}
      note={
        approved
          ? "A change needs a reason and starts a new revision, which must be approved again. The approved revision stays as it was."
          : "Leave a row blank to drop it. Without a price, the supplier's price in force is used."
      }
    >
      <KForm action={updatePO} submit={approved ? "Save as a new revision" : "Save order"} pending="Saving…" label="Edit order">
        <input type="hidden" name="id" value={po.id} />
        <input type="hidden" name="lines_present" value="1" />
        <ol className="grid gap-3">
          {rows.map((l, i) => (
            <li key={i} className="grid gap-3 border-b border-border pb-3 sm:grid-cols-[2fr_7rem_9rem]">
              <Labelled label={`Line ${i + 1}: product`}>
                <SelectInput name="line_product" defaultValue={l?.product ?? ""}>
                  <option value="">—</option>
                  {options.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} ({p.pack_quantity} {p.pack_unit})
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Packs">
                <TextInput name="line_packs" inputMode="decimal" defaultValue={l?.packs ?? ""} />
              </Labelled>
              <Labelled label="Price per pack ($)">
                <TextInput name="line_price" inputMode="decimal" defaultValue={l?.price != null ? (l.price / 100).toFixed(2) : ""} />
              </Labelled>
              <input type="hidden" name="line_procurement" value={l?.procurement ?? ""} />
            </li>
          ))}
        </ol>
        <div className="grid gap-3 sm:grid-cols-3">
          <Labelled label="Expected delivery">
            <TextInput type="date" name="expected_delivery" defaultValue={po.expected_delivery ?? ""} />
          </Labelled>
          <Labelled label="Deliver to">
            <SelectInput name="location_id" defaultValue={po.location_id ?? ""}>
              <option value="">Not set</option>
              {locations
                .filter((x) => x.active && x.type !== "quarantine")
                .map((x) => (
                  <option key={x.id} value={x.id}>
                    {x.name}
                  </option>
                ))}
            </SelectInput>
          </Labelled>
          <Labelled label="Delivery fee ($)">
            <TextInput name="delivery_fee" inputMode="decimal" defaultValue={(po.delivery_fee_minor / 100).toFixed(2)} />
          </Labelled>
        </div>
        <Labelled label="Notes for the supplier">
          <TextArea name="notes" rows={2} defaultValue={po.notes} maxLength={2000} />
        </Labelled>
        {!po.supplier_qualified ? (
          <Labelled label="Exception reason" hint="Only if the business's exception policy allows ordering from an unapproved supplier.">
            <TextInput name="supplier_exception" defaultValue={po.supplier_exception ?? ""} maxLength={500} />
          </Labelled>
        ) : null}
        {approved ? (
          <Labelled label="Reason for the change">
            <TextInput name="reason" required maxLength={500} />
          </Labelled>
        ) : null}
      </KForm>
    </Panel>
  );
}

function Actions({ po, perms }: { po: PurchaseOrder; perms: string[] }) {
  const has = (p: string) => perms.includes(p);
  const hidden = (action: string) => (
    <>
      <input type="hidden" name="id" value={po.id} />
      <input type="hidden" name="action" value={action} />
    </>
  );
  const items = [];
  if (po.status === "draft" && has("purchase_orders.create")) {
    items.push(
      <Panel key="submit" title="Send for approval" note="Someone other than you approves it.">
        <KForm action={poAction} submit="Submit for approval" pending="Sending…" label="Submit order" size="lg">
          {hidden("submit")}
        </KForm>
      </Panel>,
    );
  }
  if (po.status === "pending_approval" && (has("purchase_orders.approve") || has("purchase_orders.approve_above_limit"))) {
    items.push(
      <Panel
        key="approve"
        title="Approve"
        note={po.above_limit ? "Above the approval limit: it also needs someone allowed to approve above it (one person can give both if they hold both)." : "You cannot approve an order you prepared."}
      >
        <KForm action={poAction} submit="Approve order" pending="Approving…" label="Approve order" size="lg">
          {hidden("approve")}
        </KForm>
        <div className="mt-4">
          <KForm action={poAction} submit="Return to buyer" pending="Saving…" label="Reject order" variant="secondary">
            {hidden("reject")}
            <Labelled label="What needs to change">
              <TextInput name="reason" required maxLength={500} />
            </Labelled>
          </KForm>
        </div>
      </Panel>,
    );
  }
  if (po.status === "approved" && has("purchase_orders.issue")) {
    items.push(
      <Panel key="issue" title="Issue to the supplier" note="Print or download the order and send it yourself; then record how. The contact is added to the supplier's log.">
        <KForm action={poAction} submit="Record as sent" pending="Saving…" label="Issue order" size="lg">
          {hidden("issue")}
          <Labelled label="Sent by">
            <SelectInput name="channel" defaultValue="whatsapp">
              <Options values={CONTACT_CHANNELS} />
            </SelectInput>
          </Labelled>
          <Labelled label="Note">
            <TextInput name="note" maxLength={500} />
          </Labelled>
        </KForm>
      </Panel>,
    );
  }
  if (["draft", "pending_approval", "approved", "issued"].includes(po.status) && has("purchase_orders.create")) {
    items.push(
      <Panel key="cancel" title="Cancel" note="Only while nothing has been received.">
        <KForm action={poAction} submit="Cancel order" pending="Saving…" label="Cancel order" variant="secondary">
          {hidden("cancel")}
          <Labelled label="Reason">
            <TextInput name="reason" required maxLength={500} />
          </Labelled>
        </KForm>
      </Panel>,
    );
  }
  if (["partially_received", "received"].includes(po.status) && has("purchase_orders.create")) {
    items.push(
      <Panel key="close" title="Close" note="Nothing more is expected against this order.">
        <KForm action={poAction} submit="Close order" pending="Saving…" label="Close order" variant="secondary">
          {hidden("close")}
          <Labelled label="Reason">
            <TextInput name="reason" required maxLength={500} />
          </Labelled>
        </KForm>
      </Panel>,
    );
  }
  return items.length ? <div className="grid gap-6 [&>*]:min-w-0 lg:grid-cols-2">{items}</div> : null;
}
