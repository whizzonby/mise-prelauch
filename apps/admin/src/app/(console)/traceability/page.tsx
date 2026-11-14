import { ApiError, type Trace } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput } from "@/components/commerce-ui";
import { formatDate, formatDay, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Traceability" };

type Search = { lot?: string; batch?: string; supplier?: string; purchase_order?: string };

/**
 * Supplier → purchase order → receipt → lot → issue → batch → recipe →
 * customer orders. Batch-level, and the page says so.
 */
export default async function TraceabilityPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "traceability.read")) notFound();
  const sp = await searchParams;
  const asked = sp.lot || sp.batch || sp.supplier || sp.purchase_order;
  const [lots, batches, suppliers] = await Promise.all([
    api.supply.lots(token, { held: false }),
    api.kitchen.batches(token, {}).catch(() => ({ items: [] })),
    can(admin, "suppliers.read") ? api.supply.suppliers(token, { page_size: 200 }) : Promise.resolve(null),
  ]);
  let trace: Trace | null = null;
  let error = "";
  if (asked) {
    trace = await api.supply.trace(token, sp).catch((e) => {
      if (e instanceof ApiError) {
        error = e.message;
        return null;
      }
      throw e;
    });
  }
  return (
    <>
      <PageHeader
        title="Traceability"
        description="Follow an ingredient lot from its supplier to the batches it went into and the customer orders behind them, or the other way round."
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <Panel title="Trace from">
          <form className="grid gap-3 sm:grid-cols-[1fr_auto] lg:grid-cols-4" method="get">
            <Labelled label="A lot">
              <SelectInput name="lot" defaultValue={sp.lot ?? ""}>
                <option value="">—</option>
                {lots.items.map((l) => (
                  <option key={l.id} value={l.id}>
                    {l.code} · {l.ingredient_name}
                  </option>
                ))}
              </SelectInput>
            </Labelled>
            <Labelled label="A batch">
              <SelectInput name="batch" defaultValue={sp.batch ?? ""}>
                <option value="">—</option>
                {batches.items.map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.code} · {b.meal_name}
                  </option>
                ))}
              </SelectInput>
            </Labelled>
            {suppliers ? (
              <Labelled label="A supplier">
                <SelectInput name="supplier" defaultValue={sp.supplier ?? ""}>
                  <option value="">—</option>
                  {suppliers.items.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
            ) : null}
            <div className="flex items-end">
              <button type="submit" className="inline-flex min-h-11 items-center rounded-md bg-primary px-5 font-semibold text-primary-foreground">
                Trace
              </button>
            </div>
          </form>
          <p className="type-caption mt-2 text-muted">Choose one. The first one chosen is used.</p>
        </Panel>
        {error ? <p className="type-body-sm text-error">{error}</p> : null}
        {trace ? (
          <>
            <p className="type-body-sm border-l-2 border-accent pl-3">{trace.notice}</p>
            {trace.lots.length === 0 ? <p className="type-body-sm text-muted">Nothing to trace.</p> : null}
            {trace.lots.map(({ lot, batches: uses }) => (
              <Panel key={lot.id} title={`${lot.ingredient_name} · lot ${lot.code}`}>
                <ol className="type-body-sm grid gap-2 border-l-2 border-border pl-4">
                  <li>
                    <strong>Supplier:</strong>{" "}
                    <Link href={`/suppliers/${lot.supplier_id}`} className="underline underline-offset-4">
                      {lot.supplier_name}
                    </Link>
                    {lot.supplier_lot_ref ? ` · their lot ${lot.supplier_lot_ref}` : ""}
                  </li>
                  <li>
                    <strong>Order:</strong>{" "}
                    <Link href={`/purchase-orders/${lot.po_id}`} className="underline underline-offset-4">
                      {lot.po_number}
                    </Link>
                  </li>
                  <li>
                    <strong>Received:</strong>{" "}
                    <Link href={`/receiving/${lot.receipt_id}`} className="underline underline-offset-4">
                      {lot.receipt_number}
                    </Link>{" "}
                    · {formatDay(lot.received_at.slice(0, 10))} · {quantity(lot.received_quantity, lot.unit)} · <StatusTag status={lot.status} />
                    {lot.expiry_date ? ` · expiry ${formatDate(lot.expiry_date)}` : " · expiry not recorded"}
                  </li>
                  <li>
                    <strong>Lot:</strong>{" "}
                    <Link href={`/inventory/lots/${lot.id}`} className="underline underline-offset-4">
                      {lot.code}
                    </Link>{" "}
                    · {quantity(lot.on_hand, lot.unit)} still on hand
                  </li>
                  {uses.length ? (
                    uses.map((u) => (
                      <li key={u.use.batch_id}>
                        <strong>Batch:</strong>{" "}
                        <Link href={`/production/batches/${u.use.batch_id}`} className="underline underline-offset-4">
                          {u.batch.code}
                        </Link>{" "}
                        · {u.batch.meal_name} · recipe {u.batch.recipe_name} v{u.batch.recipe_version} · production {formatDay(u.batch.production_date)} · used{" "}
                        {quantity(u.use.consumed, u.use.unit)}
                        <span className="block">
                          <strong>Orders in that batch’s plan with this meal:</strong> {u.order_numbers.length ? u.order_numbers.join(", ") : "none recorded"}
                        </span>
                      </li>
                    ))
                  ) : (
                    <li className="text-muted">Not issued to any batch.</li>
                  )}
                </ol>
              </Panel>
            ))}
          </>
        ) : null}
      </div>
    </>
  );
}
