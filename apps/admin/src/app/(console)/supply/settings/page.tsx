import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { CATEGORY_OPTIONS, Notice, Options } from "@/components/kitchen-ui";
import { LOCATION_TYPES, SUPPLIER_TYPES } from "@/components/supply-ui";
import { humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { saveChecklistItem, saveCriterion, saveLocation, savePolicy, saveProcurementSettings } from "../../supply-actions";

export const metadata: Metadata = { title: "Supply settings" };

/**
 * Policies the business decides: nothing here is assumed. Each one starts
 * empty and the screens say what it blocks until it is set.
 */
export default async function SupplySettingsPage() {
  const { token, admin } = await requireAdmin();
  const proc = can(admin, "procurement.configure");
  const inv = can(admin, "inventory.configure");
  if (!proc && !inv && !can(admin, "procurement.read")) notFound();
  const [settings, criteria, locations, policies, checklist] = await Promise.all([
    can(admin, "procurement.read") ? api.supply.procurementSettings(token) : Promise.resolve(null),
    can(admin, "suppliers.read") ? api.supply.criteria(token) : Promise.resolve(null),
    api.supply.locations(token),
    can(admin, "inventory.read") ? api.supply.policies(token) : Promise.resolve(null),
    can(admin, "receiving.read") ? api.supply.checklist(token) : Promise.resolve(null),
  ]);
  return (
    <>
      <PageHeader title="Supply settings" description="Approval limits, tolerances, currencies, storage, allocation, receiving checks and supplier criteria, as the business's procedures set them." />
      <div className="grid gap-6 [&>*]:min-w-0">
        {settings ? (
          <Panel title="Purchasing policies">
            <Notice tone="warning" title="Not configured" items={settings.notes} />
            {proc ? (
              <KForm action={saveProcurementSettings} submit="Save policies" pending="Saving…" label="Purchasing policies">
                <div className="grid gap-3 sm:grid-cols-3">
                  <Labelled label="Approval limit ($)" hint="Above it, a second approval. Blank: no limit.">
                    <TextInput name="approval_limit" inputMode="decimal" defaultValue={settings.approval_limit_minor != null ? (settings.approval_limit_minor / 100).toFixed(2) : ""} />
                  </Labelled>
                  <Labelled label="Receiving tolerance (%)" hint="Allowed over the ordered quantity. Blank: none.">
                    <TextInput name="receiving_tolerance_percent" inputMode="decimal" defaultValue={settings.receiving_tolerance_percent ?? ""} />
                  </Labelled>
                  <Labelled label="Purchasing currencies" hint="No conversion between them is assumed.">
                    <TextInput name="currencies" defaultValue={settings.currencies.join(", ")} />
                  </Labelled>
                </div>
                <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
                  <input type="checkbox" name="allow_unapproved_suppliers" defaultChecked={settings.allow_unapproved_suppliers} className="size-5" /> An approved exception policy
                  allows orders from unapproved suppliers (each order still needs its own reason)
                </label>
                <Labelled label="That policy" hint="Required when the exception is allowed.">
                  <TextInput name="unapproved_policy_ref" defaultValue={settings.unapproved_policy_ref} maxLength={200} />
                </Labelled>
              </KForm>
            ) : (
              <p className="type-body-sm">
                Approval limit: {settings.approval_limit_minor != null ? money(settings.approval_limit_minor) : "none"} · tolerance:{" "}
                {settings.receiving_tolerance_percent ?? "none"}% · currencies: {settings.currencies.join(", ")}
              </p>
            )}
          </Panel>
        ) : null}
        <Panel title="Storage locations" note="As the business's facilities are. Temperature notes only as an approved procedure states them.">
          {locations.items.length ? (
            <DataTable head={["Code", "Name", "Kind", "Notes", "Lots", "Status"]} minWidth="40rem">
              {locations.items.map((l) => (
                <tr key={l.id}>
                  <td className="p-3 font-semibold">{l.code}</td>
                  <td className="p-3">{l.name}</td>
                  <td className="p-3">{humanize(l.type)}</td>
                  <td className="type-caption p-3">{[l.temperature_note, l.capacity_note].filter(Boolean).join(" · ") || "—"}</td>
                  <td className="p-3">{l.lots}</td>
                  <td className="p-3">
                    <StatusTag status={l.active ? "active" : "draft"} label={l.active ? "Open" : "Closed"} />
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <p className="type-body-sm text-muted">None configured. Nothing can be received until a location exists.</p>
          )}
          {inv ? (
            <div className="mt-5">
              <KForm action={saveLocation} submit="Add location" pending="Saving…" label="Add a location" reset>
                <div className="grid gap-3 sm:grid-cols-4">
                  <Labelled label="Code">
                    <TextInput name="code" required maxLength={20} placeholder="DRY-1" />
                  </Labelled>
                  <Labelled label="Name">
                    <TextInput name="name" required maxLength={120} />
                  </Labelled>
                  <Labelled label="Kind">
                    <SelectInput name="type" defaultValue="dry">
                      <Options values={LOCATION_TYPES} />
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Temperature note (approved)">
                    <TextInput name="temperature_note" maxLength={300} />
                  </Labelled>
                </div>
              </KForm>
            </div>
          ) : null}
        </Panel>
        {policies ? (
          <Panel title="Allocation by ingredient category" note="FEFO uses only recorded expiry dates; lots without one are not allocated under FEFO. A category with no policy uses FIFO.">
            <DataTable head={["Category", "Method", "Policy"]} minWidth="32rem">
              {policies.items.map((p) => (
                <tr key={p.category}>
                  <td className="p-3 font-semibold">{humanize(p.category)}</td>
                  <td className="p-3">
                    {p.method.toUpperCase()} {p.default ? <StatusTag status="draft" label="Default" /> : null}
                  </td>
                  <td className="type-caption p-3">{p.note}</td>
                </tr>
              ))}
            </DataTable>
            {inv ? (
              <div className="mt-5">
                <KForm action={savePolicy} submit="Set policy" pending="Saving…" label="Set an allocation policy" inline>
                  <Labelled label="Category">
                    <SelectInput name="category" defaultValue="produce">
                      <Options values={CATEGORY_OPTIONS} />
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Method">
                    <SelectInput name="method" defaultValue="fefo">
                      <option value="fifo">FIFO: first received</option>
                      <option value="fefo">FEFO: first expiring</option>
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Approved policy">
                    <TextInput name="note" required maxLength={300} className="w-56" />
                  </Labelled>
                </KForm>
              </div>
            ) : null}
          </Panel>
        ) : null}
        {checklist ? (
          <Panel title="Receiving checklist" note="What every delivery is inspected for, from the approved procedure. Without it nothing can be accepted.">
            {checklist.items.length ? (
              <ul className="type-body-sm grid gap-2">
                {checklist.items.map((c) => (
                  <li key={c.id}>
                    <strong>{c.name}</strong> {c.active ? null : <StatusTag status="draft" label="Inactive" />} · {c.criteria}
                    {c.categories.length ? ` · for ${c.categories.map(humanize).join(", ")}` : " · all ingredients"}
                    {c.procedure_ref ? ` · ${c.procedure_ref}` : ""}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="type-body-sm text-muted">None configured.</p>
            )}
            {inv ? (
              <div className="mt-5">
                <KForm action={saveChecklistItem} submit="Add check" pending="Saving…" label="Add a receiving check" reset>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Labelled label="Check">
                      <TextInput name="name" required maxLength={120} />
                    </Labelled>
                    <Labelled label="Procedure reference">
                      <TextInput name="procedure_ref" maxLength={120} />
                    </Labelled>
                  </div>
                  <Labelled label="Pass criteria (from the procedure)">
                    <TextArea name="criteria" rows={2} required maxLength={1000} />
                  </Labelled>
                  <input type="hidden" name="categories_present" value="1" />
                  <fieldset className="flex flex-wrap gap-3">
                    <legend className="type-label mb-1">Only for these categories (none: all)</legend>
                    {CATEGORY_OPTIONS.map((c) => (
                      <label key={c} className="type-caption inline-flex min-h-11 items-center gap-1">
                        <input type="checkbox" name="categories" value={c} className="size-4" /> {humanize(c)}
                      </label>
                    ))}
                  </fieldset>
                </KForm>
              </div>
            ) : null}
          </Panel>
        ) : null}
        {criteria ? (
          <Panel title="Supplier qualification criteria" note="From the approved procedure. A supplier is approved only when every required criterion for its type is met.">
            {criteria.items.length ? (
              <ul className="type-body-sm grid gap-2">
                {criteria.items.map((c) => (
                  <li key={c.id}>
                    <strong>{c.name}</strong> {c.required ? <StatusTag status="pending" label="Required" /> : null} {c.active ? null : <StatusTag status="draft" label="Inactive" />}
                    {c.supplier_types.length ? ` · for ${c.supplier_types.map(humanize).join(", ")}` : " · every supplier"}
                    {c.procedure_ref ? ` · ${c.procedure_ref}` : ""}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="type-body-sm text-muted">None configured: no supplier can be approved.</p>
            )}
            {proc ? (
              <div className="mt-5">
                <KForm action={saveCriterion} submit="Add criterion" pending="Saving…" label="Add a qualification criterion" reset>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Labelled label="Criterion">
                      <TextInput name="name" required maxLength={120} />
                    </Labelled>
                    <Labelled label="Procedure reference">
                      <TextInput name="procedure_ref" maxLength={120} />
                    </Labelled>
                  </div>
                  <Labelled label="What it means">
                    <TextArea name="description" rows={2} maxLength={1000} />
                  </Labelled>
                  <input type="hidden" name="types_present" value="1" />
                  <fieldset className="flex flex-wrap gap-3">
                    <legend className="type-label mb-1">Applies to (none: every supplier)</legend>
                    {SUPPLIER_TYPES.map((t) => (
                      <label key={t} className="type-caption inline-flex min-h-11 items-center gap-1">
                        <input type="checkbox" name="supplier_types" value={t} className="size-4" /> {humanize(t)}
                      </label>
                    ))}
                  </fieldset>
                  <input type="hidden" name="required_present" value="1" />
                  <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
                    <input type="checkbox" name="required" defaultChecked className="size-5" /> Required for approval
                  </label>
                </KForm>
              </div>
            ) : null}
          </Panel>
        ) : null}
      </div>
    </>
  );
}
