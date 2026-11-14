import { ApiError, type InventoryLot } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice, Options } from "@/components/kitchen-ui";
import { ADJUST_CATEGORIES } from "@/components/supply-ui";
import { formatDate, formatDateTime, humanize, money, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { adjust, lotAction, releaseReservation } from "../../../supply-actions";

export const metadata: Metadata = { title: "Lot" };

export default async function LotPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "inventory.read")) notFound();
  const { id } = await params;
  const l = await api.supply.lot(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const inspect = can(admin, "receiving.inspect");
  const adjustOk = can(admin, "inventory.adjust");
  const locations = adjustOk ? await api.supply.locations(token) : null;
  return (
    <>
      <PageHeader
        back={{ href: "/inventory/lots", label: "All lots" }}
        title={`${l.ingredient_name} · ${l.code}`}
        description={
          <>
            <StatusTag status={l.status} /> · from{" "}
            <Link href={`/suppliers/${l.supplier_id}`} className="underline underline-offset-4">
              {l.supplier_name}
            </Link>{" "}
            ·{" "}
            <Link href={`/purchase-orders/${l.po_id}`} className="underline underline-offset-4">
              {l.po_number}
            </Link>{" "}
            ·{" "}
            <Link href={`/receiving/${l.receipt_id}`} className="underline underline-offset-4">
              {l.receipt_number}
            </Link>
            {l.supplier_lot_ref ? ` · supplier lot ${l.supplier_lot_ref}` : ""}
          </>
        }
        actions={
          can(admin, "traceability.read") ? (
            <Link href={`/traceability?lot=${l.id}`} className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Trace this lot
            </Link>
          ) : null
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <section className="grid gap-4 border border-border bg-surface p-5 sm:grid-cols-4">
          {[
            ["On hand", quantity(l.on_hand, l.unit)],
            ["Reserved", quantity(l.reserved, l.unit)],
            ["Received", quantity(l.received_quantity, l.unit)],
            ["Expiry", l.expiry_date ? formatDate(l.expiry_date) : l.expiry_status === "none_applies" ? "None applies" : "Not recorded"],
          ].map(([k, v]) => (
            <div key={k}>
              <p className="type-caption font-semibold uppercase tracking-[0.06em] text-muted">{k}</p>
              <p className="mt-1 font-display text-[1.75rem] leading-none">{v}</p>
            </div>
          ))}
        </section>
        {l.blocked ? <Notice tone={l.status === "available" ? "warning" : "error"} title="Not allocated" items={[l.blocked]} /> : null}
        <p className="type-body-sm text-muted">
          In {l.location_name}
          {l.unit_cost_minor != null && l.currency ? ` · cost ${money(l.unit_cost_minor, l.currency)} per pack as ordered` : ""}
          {l.expiry_note ? ` · ${l.expiry_note}` : ""}
        </p>
        <div className="grid gap-6 [&>*]:min-w-0 lg:grid-cols-2">
          {inspect && l.expiry_status === "not_recorded" && Number(l.on_hand) > 0 ? <ExpiryPanel l={l} /> : null}
          {inspect ? <StatusPanel l={l} /> : null}
          {adjustOk && locations && Number(l.on_hand) > 0 ? (
            <Panel title="Move" note="The whole lot changes location.">
              <KForm action={lotAction} submit="Move lot" pending="Moving…" label="Move lot" inline>
                <input type="hidden" name="id" value={l.id} />
                <input type="hidden" name="action" value="transfer" />
                <Labelled label="To">
                  <SelectInput name="location_id" defaultValue="">
                    <option value="" disabled>
                      Choose
                    </option>
                    {locations.items
                      .filter((x) => x.active && x.id !== l.location_id)
                      .map((x) => (
                        <option key={x.id} value={x.id}>
                          {x.name}
                        </option>
                      ))}
                  </SelectInput>
                </Labelled>
              </KForm>
            </Panel>
          ) : null}
          {adjustOk && Number(l.on_hand) > 0 ? (
            <Panel title={l.status === "rejected" || l.status === "quarantined" ? "Return or waste" : "Store waste"} note="Spoiled or damaged in storage. Kitchen waste during a batch is recorded on the batch, not here.">
              <KForm action={lotAction} submit="Record store waste" pending="Saving…" label="Record store waste" secondary={l.status === "rejected" || l.status === "quarantined" ? { name: "to_supplier", value: "1", label: "Returned to supplier" } : undefined}>
                <input type="hidden" name="id" value={l.id} />
                <input type="hidden" name="action" value="waste" />
                <div className="grid grid-cols-2 gap-3">
                  <Labelled label={`Quantity (${l.unit})`}>
                    <TextInput name="quantity" inputMode="decimal" required />
                  </Labelled>
                  <Labelled label="Reason">
                    <TextInput name="reason" required maxLength={500} />
                  </Labelled>
                </div>
              </KForm>
            </Panel>
          ) : null}
          {adjustOk && l.status !== "rejected" ? (
            <Panel title="Adjust" note="Sets the quantity actually there, with the reason. The previous and new balance are kept; nothing is erased.">
              <KForm action={adjust} submit="Record adjustment" pending="Saving…" label="Adjust stock">
                <input type="hidden" name="lot_id" value={l.id} />
                <div className="grid grid-cols-2 gap-3">
                  <Labelled label={`Quantity there (${l.unit})`}>
                    <TextInput name="new_on_hand" inputMode="decimal" required />
                  </Labelled>
                  <Labelled label="Why">
                    <SelectInput name="category" defaultValue="measurement_correction">
                      <Options values={ADJUST_CATEGORIES} />
                    </SelectInput>
                  </Labelled>
                </div>
                <Labelled label="Explanation">
                  <TextInput name="reason" required maxLength={1000} />
                </Labelled>
                <Labelled label="Evidence (optional)" hint="For example, the scale log entry.">
                  <TextInput name="evidence" maxLength={500} />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}
        </div>
        {l.reservations?.length ? (
          <Panel title="Reservations">
            <ul className="grid gap-3">
              {l.reservations.map((r) => (
                <li key={r.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
                  <span className="type-body-sm">
                    Plan {r.plan_number}: {quantity(r.open_quantity, r.unit)} held of {quantity(r.quantity, r.unit)} <StatusTag status={r.status} />
                    {r.release_reason ? ` · ${r.release_reason}` : ""}
                  </span>
                  {r.status === "active" && can(admin, "inventory.reserve") ? (
                    <KForm action={releaseReservation} submit="Release" pending="…" label={`Release reservation for ${r.plan_number}`} variant="secondary" inline>
                      <input type="hidden" name="id" value={r.id} />
                      <input type="hidden" name="back" value={`/inventory/lots/${l.id}`} />
                      <Labelled label="Reason">
                        <TextInput name="reason" required maxLength={500} />
                      </Labelled>
                    </KForm>
                  ) : null}
                </li>
              ))}
            </ul>
          </Panel>
        ) : null}
        {l.batches?.length ? (
          <Panel title="Issued to batches">
            <DataTable head={["Batch", "Meal", "Issued", "Returned", "Consumed"]} minWidth="36rem">
              {l.batches.map((b) => (
                <tr key={b.batch_id}>
                  <td className="p-3">
                    <Link href={`/production/batches/${b.batch_id}`} className="font-semibold underline underline-offset-4">
                      {b.batch_code}
                    </Link>
                  </td>
                  <td className="p-3">{b.meal_name}</td>
                  <td className="p-3">{quantity(b.issued, b.unit)}</td>
                  <td className="p-3">{quantity(b.returned, b.unit)}</td>
                  <td className="p-3">{quantity(b.consumed, b.unit)}</td>
                </tr>
              ))}
            </DataTable>
          </Panel>
        ) : null}
        <Panel title="Ledger" note="Every change to this lot, newest first. Never edited or deleted.">
          <DataTable head={["When", "Movement", "On hand", "Reserved", "Detail"]} minWidth="44rem">
            {(l.movements ?? []).map((m) => (
              <tr key={m.id}>
                <td className="p-3">
                  {formatDateTime(m.created_at)}
                  <span className="type-caption block text-muted">{m.actor_name}</span>
                </td>
                <td className="p-3 font-semibold">{humanize(m.type)}</td>
                <td className="p-3">
                  {Number(m.on_hand_delta) ? `${Number(m.on_hand_delta) > 0 ? "+" : ""}${m.on_hand_delta}` : "—"} → {m.on_hand_after}
                </td>
                <td className="p-3">
                  {Number(m.reserved_delta) ? `${Number(m.reserved_delta) > 0 ? "+" : ""}${m.reserved_delta}` : "—"} → {m.reserved_after}
                </td>
                <td className="type-caption p-3">
                  {m.reason}
                  {m.from_location || m.to_location ? ` · ${m.from_location ?? "—"} → ${m.to_location ?? "—"}` : ""}
                  {m.from_status || m.to_status ? ` · ${humanize(m.from_status ?? "")} → ${humanize(m.to_status ?? "")}` : ""}
                  {m.batch_code ? ` · batch ${m.batch_code}` : ""}
                </td>
              </tr>
            ))}
          </DataTable>
        </Panel>
      </div>
    </>
  );
}

function ExpiryPanel({ l }: { l: InventoryLot }) {
  return (
    <Panel title="Record the expiry" note="The date printed on the product or the supplier's documents, or confirm no date applies. Never a guess: until then, this lot is not used.">
      <KForm action={lotAction} submit="Record expiry" pending="Saving…" label="Record expiry">
        <input type="hidden" name="id" value={l.id} />
        <input type="hidden" name="action" value="expiry" />
        <Labelled label="Expiry or use-by date">
          <TextInput type="date" name="expiry_date" />
        </Labelled>
        <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
          <input type="checkbox" name="none_applies" className="size-5" /> No date applies to this product
        </label>
        <Labelled label="Basis" hint="Required when no date applies, for example the supplier's specification.">
          <TextInput name="note" maxLength={300} />
        </Labelled>
      </KForm>
    </Panel>
  );
}

function StatusPanel({ l }: { l: InventoryLot }) {
  const moves: Record<string, [string, string][]> = {
    quarantined: [
      ["available", "Release for use"],
      ["rejected", "Reject"],
    ],
    available: [["on_hold", "Put on hold"]],
    on_hold: [
      ["available", "Release for use"],
      ["rejected", "Reject"],
    ],
  };
  const options = moves[l.status] ?? [];
  if (!options.length) return null;
  return (
    <Panel title="Status" note={Number(l.reserved) > 0 ? "Reserved stock must be released before the lot is held or rejected." : "With a reason; the change is recorded in the ledger."}>
      <KForm action={lotAction} submit="Change status" pending="Saving…" label="Change lot status">
        <input type="hidden" name="id" value={l.id} />
        <input type="hidden" name="action" value="status" />
        <Labelled label="New status">
          <SelectInput name="status" defaultValue={options[0]![0]}>
            {options.map(([v, label]) => (
              <option key={v} value={v}>
                {label}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <Labelled label="Reason">
          <TextInput name="reason" required maxLength={500} />
        </Labelled>
      </KForm>
    </Panel>
  );
}
