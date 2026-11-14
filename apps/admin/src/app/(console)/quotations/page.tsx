import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime, formatDay, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createRFQ } from "../supply-actions";

export const metadata: Metadata = { title: "Quotations" };

/**
 * Requests for quotation and the quotations received. Staff contact
 * suppliers themselves; Mise records who was asked, how, and what they quoted.
 */
export default async function QuotationsPage({ searchParams }: { searchParams: Promise<{ run?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "procurement.read")) notFound();
  const sp = await searchParams;
  const plan = can(admin, "procurement.plan");
  const [rfqs, quotes, suppliers, ingredients, units, run] = await Promise.all([
    api.supply.rfqs(token),
    api.supply.quotations(token),
    plan && can(admin, "suppliers.read") ? api.supply.suppliers(token, { status: "approved", page_size: 200 }) : Promise.resolve(null),
    plan ? api.kitchen.ingredients(token, { active: "true", page_size: 200 }) : Promise.resolve(null),
    plan ? api.kitchen.units(token) : Promise.resolve(null),
    sp.run ? api.supply.run(token, sp.run).catch(() => null) : Promise.resolve(null),
  ]);
  // From a run: ask about what is short.
  const short = (run?.plan_lines ?? []).filter((l) => l.shortage != null && Number(l.shortage) > 0);
  const rows = [...short.map((l) => ({ ing: l.ingredient_id, qty: l.shortage ?? "", unit: l.unit })), ...Array.from({ length: Math.max(1, 3 - short.length) }, () => null)];
  return (
    <>
      <PageHeader
        title="Quotations"
        description="Ask suppliers for prices by phone, WhatsApp, email or in person, then record what they quoted. A recorded quotation never changes: a revised quote is recorded as a new one, and its prices join the supplier's price history."
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          <Panel title="Requests for quotation">
            {rfqs.items.length ? (
              <DataTable head={["Request", "Items", "Suppliers asked", "Quotes", "Due", "Status"]} minWidth="40rem">
                {rfqs.items.map((r) => (
                  <tr key={r.id}>
                    <td className="p-3">
                      <Link href={`/quotations/${r.id}`} className="font-semibold underline underline-offset-4">
                        {r.number}
                      </Link>
                      <span className="type-caption block text-muted">{r.buyer_name}</span>
                    </td>
                    <td className="p-3">{r.lines.map((l) => l.description).join(", ")}</td>
                    <td className="p-3">
                      {r.suppliers.filter((x) => x.invited_at).length} of {r.suppliers.length}
                    </td>
                    <td className="p-3">{r.quotations.length}</td>
                    <td className="p-3">{r.deadline ? formatDateTime(r.deadline) : "—"}</td>
                    <td className="p-3">
                      <StatusTag status={r.status === "open" ? "in_progress" : r.status} label={humanize(r.status)} />
                    </td>
                  </tr>
                ))}
              </DataTable>
            ) : (
              <EmptyState>No requests yet.</EmptyState>
            )}
          </Panel>
          <Panel title="Quotations received">
            {quotes.items.length ? (
              <DataTable head={["Supplier", "Reference", "Lines", "Valid until", "Received"]} minWidth="36rem">
                {quotes.items.map((q) => (
                  <tr key={q.id}>
                    <td className="p-3 font-semibold">{q.supplier_name}</td>
                    <td className="p-3">
                      {q.rfq_id ? (
                        <Link href={`/quotations/${q.rfq_id}`} className="underline underline-offset-4">
                          {q.reference || "—"}
                        </Link>
                      ) : (
                        q.reference || "—"
                      )}
                    </td>
                    <td className="p-3">{q.lines.map((l) => `${l.product_name} × ${l.packs}`).join(", ")}</td>
                    <td className="p-3">{q.valid_until ? formatDay(q.valid_until) : "—"}</td>
                    <td className="p-3">
                      {formatDateTime(q.received_at)}
                      <span className="type-caption block text-muted">
                        by {humanize(q.received_via)} · {q.recorded_by_name}
                      </span>
                    </td>
                  </tr>
                ))}
              </DataTable>
            ) : (
              <EmptyState>No quotations recorded.</EmptyState>
            )}
          </Panel>
        </div>
        {plan && suppliers && ingredients && units ? (
          <Panel title="Ask for quotations" note={run ? `For run ${run.number}: the short ingredients are filled in.` : "What you want priced, and whom you will ask."}>
            <KForm action={createRFQ} submit="Create request" pending="Creating…" label="Create a request for quotation">
              {run ? <input type="hidden" name="run_id" value={run.id} /> : null}
              {rows.map((l, i) => (
                <div key={i} className="grid grid-cols-[1fr_6rem_5rem] gap-2">
                  <Labelled label={`Item ${i + 1}`}>
                    <SelectInput name="line_ingredient" defaultValue={l?.ing ?? ""}>
                      <option value="">—</option>
                      {ingredients.items.map((x) => (
                        <option key={x.id} value={x.id}>
                          {x.name}
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Quantity">
                    <TextInput name="line_quantity" inputMode="decimal" defaultValue={l?.qty ?? ""} />
                  </Labelled>
                  <Labelled label="Unit">
                    <SelectInput name="line_unit" defaultValue={l?.unit ?? "kg"}>
                      {units.items.map((u) => (
                        <option key={u.code}>{u.code}</option>
                      ))}
                    </SelectInput>
                  </Labelled>
                </div>
              ))}
              <fieldset className="grid gap-1">
                <legend className="type-label mb-1">Suppliers to ask</legend>
                {suppliers.items.map((s) => (
                  <label key={s.id} className="type-body-sm inline-flex min-h-11 items-center gap-2">
                    <input type="checkbox" name="supplier_ids" value={s.id} className="size-5" /> {s.name}
                  </label>
                ))}
                {suppliers.items.length === 0 ? <p className="type-caption text-muted">No approved suppliers yet.</p> : null}
              </fieldset>
              <div className="grid grid-cols-2 gap-3">
                <Labelled label="Needed by">
                  <TextInput type="date" name="required_by" defaultValue={run?.required_by ?? ""} />
                </Labelled>
                <Labelled label="Quotes due">
                  <TextInput type="date" name="deadline" />
                </Labelled>
              </div>
              <Labelled label="Notes">
                <TextArea name="notes" rows={2} maxLength={2000} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
