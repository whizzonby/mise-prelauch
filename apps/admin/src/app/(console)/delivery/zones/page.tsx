import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { Hidden } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { saveZone } from "../../delivery-actions";

export const metadata: Metadata = { title: "Delivery zones" };

/**
 * Operational zones for dispatch. Customer-facing serviceability stays per
 * region (the business decision of 11 Nov 2026); a zone covers regions by
 * explicit rules. A region with no zone is flagged, never assumed.
 */
export default async function ZonesPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_zones.read")) notFound();
  const [zones, coverage] = await Promise.all([api.delivery.zones(token), api.delivery.coverage(token)]);
  const manage = can(admin, "delivery_zones.manage");
  const free = coverage.items.filter((c) => !c.zone_id);
  const regionBox = (selected: string[]) => (
    <fieldset className="grid gap-1 sm:grid-cols-2">
      <legend className="type-body-sm mb-1 font-semibold">Regions covered</legend>
      {coverage.items
        .filter((c) => !c.zone_id || selected.includes(c.region))
        .map((c) => (
          <label key={c.region} className="flex min-h-11 items-center gap-3">
            <input type="checkbox" name="regions" value={c.region} defaultChecked={selected.includes(c.region)} className="size-5" />
            {humanize(c.region)} {c.status !== "active" ? <span className="type-caption text-muted">({c.status})</span> : null}
          </label>
        ))}
    </fieldset>
  );
  return (
    <>
      <PageHeader title="Delivery zones" description="Zones group regions for dispatch, capacity and partner coverage. None are assumed: each is configured from approved business information." />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
        <Panel title="Region coverage" note={`${free.length} served region(s) have no zone: their deliveries need a zone assigned by hand.`}>
          <DataTable head={["Region", "Customer-facing status", "Zone"]} minWidth="24rem">
            {coverage.items.map((c) => (
              <tr key={c.region}>
                <td className="p-3">{humanize(c.region)}</td>
                <td className="p-3">
                  <StatusTag status={c.status} />
                </td>
                <td className="p-3">{c.zone_code ?? <span className="text-muted">none</span>}</td>
              </tr>
            ))}
          </DataTable>
        </Panel>
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          {zones.items.map((z) => (
            <Panel key={z.id} title={`${z.code} · ${z.name}`} note={`${z.regions.length} region(s) · ${z.partner_count} partner(s) · ${z.status}`}>
              {manage ? (
                <KForm action={saveZone} submit="Save zone" pending="Saving…" label={`Edit zone ${z.code}`}>
                  <Hidden values={{ id: z.id }} />
                  <div className="grid gap-3 sm:grid-cols-3">
                    <Labelled label="Code">
                      <TextInput name="code" defaultValue={z.code} />
                    </Labelled>
                    <Labelled label="Name">
                      <TextInput name="name" defaultValue={z.name} />
                    </Labelled>
                    <Labelled label="Status">
                      <SelectInput name="status" defaultValue={z.status}>
                        <option value="draft">Draft</option>
                        <option value="active">Active</option>
                        <option value="inactive">Inactive</option>
                      </SelectInput>
                    </Labelled>
                  </div>
                  {regionBox(z.regions.map((r) => r.region))}
                  <Labelled label="Delivery fee policy reference" hint="Only an approved policy; fees are not invented.">
                    <TextInput name="fee_policy_ref" defaultValue={z.fee_policy_ref} />
                  </Labelled>
                  <Labelled label="Operational notes">
                    <TextInput name="notes" defaultValue={z.notes} />
                  </Labelled>
                </KForm>
              ) : (
                <p className="type-body-sm">{z.regions.map((r) => humanize(r.region)).join(", ") || "No regions."}</p>
              )}
            </Panel>
          ))}
          {manage ? (
            <Panel title="Add a zone">
              <KForm action={saveZone} submit="Add zone" pending="Saving…" label="Add a zone">
                <div className="grid gap-3 sm:grid-cols-2">
                  <Labelled label="Code" hint="e.g. POS-WEST">
                    <TextInput name="code" />
                  </Labelled>
                  <Labelled label="Name">
                    <TextInput name="name" />
                  </Labelled>
                </div>
                {regionBox([])}
              </KForm>
            </Panel>
          ) : null}
        </div>
      </div>
    </>
  );
}
