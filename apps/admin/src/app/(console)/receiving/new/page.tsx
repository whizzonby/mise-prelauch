import { randomUUID } from "node:crypto";

import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound, redirect } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice, Options } from "@/components/kitchen-ui";
import { DISCREPANCIES } from "@/components/supply-ui";
import { formatDay, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { receive } from "../../supply-actions";

export const metadata: Metadata = { title: "Receive a delivery" };

/**
 * One delivery against one order, one card per line, sized for a phone:
 * how much came, what happened to it, the lot details and the inspection.
 */
export default async function ReceivePage({ searchParams }: { searchParams: Promise<{ po?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "receiving.manage") || !can(admin, "receiving.inspect")) notFound();
  const { po: poID } = await searchParams;
  if (!poID) redirect("/receiving"); // an order is chosen on the receiving list
  const po = await api.supply.purchaseOrder(token, poID).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const [checklist, locations, ingredients] = await Promise.all([
    api.supply.checklist(token),
    api.supply.locations(token),
    api.kitchen.ingredients(token, { page_size: 200 }),
  ]);
  const category = new Map(ingredients.items.map((i) => [i.id, i.category]));
  const active = checklist.items.filter((c) => c.active);
  const usable = locations.items.filter((l) => l.active && l.type !== "quarantine");
  const quarantine = locations.items.filter((l) => l.active && l.type === "quarantine");
  const lines = (po.lines ?? []).filter((l) => Number(l.open_packs) > 0);
  const problems = [
    ...(active.length === 0 ? ["No receiving checklist is configured from the approved procedure: stock can be quarantined or rejected, not accepted."] : []),
    ...(usable.length === 0 ? ["No storage location is configured: an inventory manager must add one before stock can be accepted."] : []),
    ...(quarantine.length === 0 ? ["No quarantine location is configured: quarantined or rejected stock cannot be recorded."] : []),
  ];
  return (
    <>
      <PageHeader
        back={{ href: "/receiving", label: "Goods receiving" }}
        title={`Receive from ${po.supplier_name}`}
        description={
          <>
            Order{" "}
            <Link href={`/purchase-orders/${po.id}`} className="underline underline-offset-4">
              {po.number}
            </Link>
            {po.expected_delivery ? ` · expected ${formatDay(po.expected_delivery)}` : ""}. Enter only what is in front of you; an empty line is not received.
          </>
        }
      />
      <Notice tone="warning" title="Before you start" items={problems} />
      <KForm action={receive} submit="Record the delivery" pending="Recording…" label="Receive a delivery" size="lg">
        <input type="hidden" name="po_id" value={po.id} />
        <input type="hidden" name="idempotency_key" value={randomUUID()} />
        {active.map((c) => (
          <input key={c.id} type="hidden" name="checklist_item" value={c.id} />
        ))}
        <div className="grid gap-5">
          {lines.map((l) => {
            const items = active.filter((c) => c.categories.length === 0 || (l.ingredient_id && c.categories.includes(category.get(l.ingredient_id) ?? "")));
            return (
              <section key={l.id} aria-label={l.description} className="grid gap-4 border border-border bg-surface p-4 sm:p-5">
                <input type="hidden" name="po_line_id" value={l.id} />
                <div>
                  <h2 className="text-[1.25rem] font-semibold leading-snug">{l.description}</h2>
                  <p className="type-body-sm text-muted">
                    {l.open_packs} of {l.packs} packs still expected
                    {l.quantity_base && l.base_unit ? ` · the order is ${quantity(l.quantity_base, l.base_unit)}` : ""}
                  </p>
                </div>
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                  <Labelled label="Packs delivered">
                    <TextInput name={`delivered_${l.id}`} inputMode="decimal" className="text-[1.25rem]" />
                  </Labelled>
                  <Labelled label="Accepted">
                    <TextInput name={`accepted_${l.id}`} inputMode="decimal" className="text-[1.25rem]" />
                  </Labelled>
                  <Labelled label="Quarantined">
                    <TextInput name={`quarantined_${l.id}`} inputMode="decimal" className="text-[1.25rem]" />
                  </Labelled>
                  <Labelled label="Rejected">
                    <TextInput name={`rejected_${l.id}`} inputMode="decimal" className="text-[1.25rem]" />
                  </Labelled>
                </div>
                {l.ingredient_id ? (
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Labelled label="Supplier's lot reference">
                      <TextInput name={`lotref_${l.id}`} maxLength={120} />
                    </Labelled>
                    <Labelled label="Expiry or use-by date" hint="As printed. Leave blank if there is none: the lot then waits for a review.">
                      <TextInput type="date" name={`expiry_${l.id}`} />
                    </Labelled>
                    <Labelled label="Store accepted stock in">
                      <SelectInput name={`location_${l.id}`} defaultValue={po.location_id ?? usable[0]?.id ?? ""}>
                        {usable.map((x) => (
                          <option key={x.id} value={x.id}>
                            {x.name}
                          </option>
                        ))}
                      </SelectInput>
                    </Labelled>
                    <Labelled label="Quarantine or rejected stock to">
                      <SelectInput name={`quarantine_${l.id}`} defaultValue={quarantine[0]?.id ?? ""}>
                        <option value="">—</option>
                        {quarantine.map((x) => (
                          <option key={x.id} value={x.id}>
                            {x.name}
                          </option>
                        ))}
                      </SelectInput>
                    </Labelled>
                    <Labelled label="Temperature (°C)" hint="Where the procedure asks for it.">
                      <TextInput name={`temp_${l.id}`} inputMode="decimal" />
                    </Labelled>
                  </div>
                ) : null}
                {items.length ? (
                  <fieldset className="grid gap-3">
                    <legend className="type-label mb-1">Inspection</legend>
                    {items.map((c) => (
                      <div key={c.id} className="grid gap-2 border-b border-border pb-3">
                        <p className="type-body-sm font-semibold">{c.name}</p>
                        <p className="type-caption text-muted">{c.criteria}</p>
                        <div className="flex flex-wrap gap-4">
                          {[
                            ["pass", "Pass"],
                            ["fail", "Fail"],
                            ["not_applicable", "Not applicable"],
                          ].map(([v, label]) => (
                            <label key={v} className="type-body inline-flex min-h-11 items-center gap-2">
                              <input type="radio" name={`check_${l.id}_${c.id}`} value={v} className="size-6" /> {label}
                            </label>
                          ))}
                        </div>
                        <TextInput name={`checknote_${l.id}_${c.id}`} placeholder="Note (required for a fail)" aria-label={`Note: ${c.name}`} maxLength={500} />
                      </div>
                    ))}
                  </fieldset>
                ) : null}
                <div className="grid gap-3 sm:grid-cols-[14rem_1fr]">
                  <Labelled label="Discrepancy">
                    <SelectInput name={`discrepancy_${l.id}`} defaultValue="none">
                      <Options values={DISCREPANCIES} />
                    </SelectInput>
                  </Labelled>
                  <Labelled label="What was wrong">
                    <TextInput name={`discnote_${l.id}`} maxLength={1000} />
                  </Labelled>
                </div>
              </section>
            );
          })}
        </div>
        <Panel title="The delivery">
          <div className="grid gap-3 sm:grid-cols-2">
            <Labelled label="Delivery note reference">
              <TextInput name="delivery_ref" maxLength={120} />
            </Labelled>
            <Labelled label="Notes">
              <TextArea name="notes" rows={2} maxLength={2000} />
            </Labelled>
          </div>
        </Panel>
      </KForm>
    </>
  );
}
