import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Options } from "@/components/kitchen-ui";
import { CONTACT_CHANNELS } from "@/components/supply-ui";
import { formatDateTime, formatDay, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { endRFQ, recordInvitation, recordQuotation } from "../../supply-actions";

export const metadata: Metadata = { title: "Request for quotation" };

export default async function RFQPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "procurement.read")) notFound();
  const { id } = await params;
  const r = await api.supply.rfq(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const plan = can(admin, "procurement.plan") && (r.status === "draft" || r.status === "open");
  const products = plan && can(admin, "supplier_products.read") ? await api.supply.products(token, {}) : null;
  return (
    <>
      <PageHeader
        back={{ href: "/quotations", label: "All quotations" }}
        title={`Request ${r.number}`}
        description={
          <>
            <StatusTag status={r.status === "open" ? "in_progress" : r.status} label={humanize(r.status)} /> · {r.buyer_name}
            {r.required_by ? ` · needed by ${formatDay(r.required_by)}` : ""}
            {r.deadline ? ` · quotes due ${formatDateTime(r.deadline)}` : ""}
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <Panel title="Asked for">
          <ul className="type-body-sm grid gap-1">
            {r.lines.map((l) => (
              <li key={l.id}>
                {l.description}: {l.quantity} {l.unit}
              </li>
            ))}
          </ul>
          {r.notes ? <p className="type-body-sm mt-2 whitespace-pre-line text-muted">{r.notes}</p> : null}
        </Panel>
        <Panel title="Suppliers" note="Record each supplier once you have asked them, and how. Nothing is sent from Mise.">
          <ul className="grid gap-3">
            {r.suppliers.map((s) => (
              <li key={s.supplier_id} className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
                <span className="type-body-sm">
                  <strong>{s.supplier_name}</strong>{" "}
                  {s.invited_at ? `asked by ${humanize(s.channel ?? "")}, ${formatDateTime(s.invited_at)}` : "not asked yet"}
                  {s.responded ? " · quoted" : ""}
                </span>
                {plan && !s.invited_at ? (
                  <KForm action={recordInvitation} submit="I asked them" pending="…" label={`Asked ${s.supplier_name}`} variant="secondary" inline>
                    <input type="hidden" name="id" value={r.id} />
                    <input type="hidden" name="supplier_id" value={s.supplier_id} />
                    <Labelled label="How">
                      <SelectInput name="channel" defaultValue="whatsapp">
                        <Options values={CONTACT_CHANNELS} />
                      </SelectInput>
                    </Labelled>
                  </KForm>
                ) : null}
              </li>
            ))}
          </ul>
        </Panel>
        <Panel title="Quotations received" note="Prices per pack as quoted. Compare them here; no supplier is chosen automatically.">
          {r.quotations.length ? (
            <DataTable head={["Supplier", "Product", "Packs", "Price per pack", "Lead time", "Valid until", "Terms"]} minWidth="52rem">
              {r.quotations.flatMap((q) =>
                q.lines.map((l) => (
                  <tr key={l.id}>
                    <td className="p-3 font-semibold">
                      {q.supplier_name}
                      <span className="type-caption block font-normal text-muted">{q.reference || "no reference"}</span>
                    </td>
                    <td className="p-3">
                      {l.product_name}
                      <span className="type-caption block text-muted">
                        {l.pack_quantity} {l.pack_unit} a pack
                      </span>
                    </td>
                    <td className="p-3">{l.packs}</td>
                    <td className="p-3">{money(l.unit_price_minor, q.currency)}</td>
                    <td className="p-3">{q.lead_time_days != null ? `${q.lead_time_days} days` : "—"}</td>
                    <td className="p-3">{q.valid_until ? formatDay(q.valid_until) : "—"}</td>
                    <td className="type-caption p-3">{q.delivery_terms || "—"}</td>
                  </tr>
                )),
              )}
            </DataTable>
          ) : (
            <p className="type-body-sm text-muted">None yet.</p>
          )}
        </Panel>
        {plan && products ? (
          <div className="grid gap-6 [&>*]:min-w-0 lg:grid-cols-2">
            <Panel title="Record a quotation" note="As received. The supplier must be one you asked.">
              <KForm action={recordQuotation} submit="Record quotation" pending="Saving…" label="Record a quotation" reset>
                <input type="hidden" name="rfq_id" value={r.id} />
                <Labelled label="Supplier">
                  <SelectInput name="supplier_id" required defaultValue="">
                    <option value="" disabled>
                      Choose
                    </option>
                    {r.suppliers.map((s) => (
                      <option key={s.supplier_id} value={s.supplier_id}>
                        {s.supplier_name}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                {[0, 1, 2].map((i) => (
                  <div key={i} className="grid grid-cols-[1fr_5rem_7rem] gap-2">
                    <Labelled label={`Product ${i + 1}`}>
                      <SelectInput name="q_product" defaultValue="">
                        <option value="">—</option>
                        {products.items
                          .filter((p) => r.suppliers.some((s) => s.supplier_id === p.supplier_id))
                          .map((p) => (
                            <option key={p.id} value={p.id}>
                              {p.supplier_name}: {p.name}
                            </option>
                          ))}
                      </SelectInput>
                    </Labelled>
                    <Labelled label="Packs">
                      <TextInput name="q_packs" inputMode="decimal" />
                    </Labelled>
                    <Labelled label="Per pack ($)">
                      <TextInput name="q_price" inputMode="decimal" />
                    </Labelled>
                  </div>
                ))}
                <div className="grid grid-cols-2 gap-3">
                  <Labelled label="Their reference">
                    <TextInput name="reference" maxLength={120} />
                  </Labelled>
                  <Labelled label="Currency">
                    <TextInput name="currency" defaultValue="TTD" maxLength={3} />
                  </Labelled>
                  <Labelled label="Lead time (days)">
                    <TextInput type="number" name="lead_time_days" min={0} max={365} />
                  </Labelled>
                  <Labelled label="Valid until">
                    <TextInput type="date" name="valid_until" />
                  </Labelled>
                </div>
                <Labelled label="Received by">
                  <SelectInput name="received_via" defaultValue="whatsapp">
                    <Options values={CONTACT_CHANNELS} />
                  </SelectInput>
                </Labelled>
                <Labelled label="Delivery terms">
                  <TextInput name="delivery_terms" maxLength={500} />
                </Labelled>
              </KForm>
            </Panel>
            <Panel title="Finish">
              <div className="flex flex-wrap gap-4">
                <KForm action={endRFQ} submit="Close request" pending="…" label="Close request" variant="secondary" inline>
                  <input type="hidden" name="id" value={r.id} />
                  <input type="hidden" name="action" value="close" />
                </KForm>
                <KForm action={endRFQ} submit="Cancel request" pending="…" label="Cancel request" variant="secondary" inline>
                  <input type="hidden" name="id" value={r.id} />
                  <input type="hidden" name="action" value="cancel" />
                </KForm>
              </div>
            </Panel>
          </div>
        ) : null}
      </div>
    </>
  );
}
