import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { savePartner } from "../../delivery-actions";

export const metadata: Metadata = { title: "Delivery partners" };

export default async function PartnersPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_partners.read")) notFound();
  const list = await api.delivery.partners(token);
  return (
    <>
      <PageHeader
        title="Delivery partners"
        description="Carrier-neutral. A partner takes live deliveries only once approved: a signed agreement and every qualification criterion met with evidence. No company is assumed to be a partner."
      />
      {list.items.length ? (
        <DataTable head={["Partner", "Mode", "Agreement", "Zones", "Couriers", "Status"]}>
          {list.items.map((p) => (
            <tr key={p.id}>
              <td className="p-3">
                <Link href={`/delivery/partners/${p.id}`} className="font-semibold underline underline-offset-4">
                  {p.name}
                </Link>
                {p.demo ? <span className="type-caption ml-2 text-muted">local demo</span> : null}
              </td>
              <td className="p-3">{humanize(p.integration_mode)}</td>
              <td className="p-3">{humanize(p.agreement_status)}</td>
              <td className="p-3">{p.zone_ids.length}</td>
              <td className="p-3">{p.courier_count}</td>
              <td className="p-3">
                <StatusTag status={p.status} />
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>No delivery partners recorded. Mise has no signed delivery partnership yet.</Empty>
      )}
      {can(admin, "delivery_partners.manage") ? (
        <div className="mt-6">
          <Panel title="Add a partner" note="Starts as a draft. Qualification and approval follow on its page.">
            <KForm action={savePartner} submit="Add partner" pending="Saving…" label="Add a delivery partner">
              <div className="grid gap-3 sm:grid-cols-3">
                <Labelled label="Name">
                  <TextInput name="name" />
                </Labelled>
                <Labelled label="Integration">
                  <SelectInput name="integration_mode">
                    <option value="manual">Manual (works from the manifest)</option>
                    <option value="internal_fleet">Internal fleet</option>
                    <option value="file">File exchange (not built)</option>
                    <option value="api">API (not built)</option>
                  </SelectInput>
                </Labelled>
                <Labelled label="Contact">
                  <TextInput name="contact_name" />
                </Labelled>
              </div>
            </KForm>
          </Panel>
        </div>
      ) : null}
    </>
  );
}
