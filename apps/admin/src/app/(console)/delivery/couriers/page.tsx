import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { api, can, requireAdmin } from "@/lib/session";

import { saveCourier } from "../../delivery-actions";

export const metadata: Metadata = { title: "Couriers" };

/** Lightweight courier records. No driver app: a courier with a login sees only their own runs here. */
export default async function CouriersPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_partners.read")) notFound();
  const [list, partners] = await Promise.all([api.delivery.couriers(token), api.delivery.partners(token)]);
  return (
    <>
      <PageHeader
        title="Couriers"
        description="Each courier works for a partner (Mise's own couriers belong to an internal-fleet partner). Linking a staff login with the courier role lets them record their own runs on a phone."
      />
      {list.items.length ? (
        <DataTable head={["Courier", "Partner", "Phone", "Login", "Runs", "Status"]}>
          {list.items.map((c) => (
            <tr key={c.id}>
              <td className="p-3 font-semibold">
                {c.name}
                {c.demo ? <span className="type-caption ml-2 font-normal text-muted">local demo</span> : null}
              </td>
              <td className="p-3">{c.partner_name}</td>
              <td className="p-3">{c.phone || "—"}</td>
              <td className="p-3">{c.user_email ?? "No login"}</td>
              <td className="p-3">{c.runs}</td>
              <td className="p-3">
                <StatusTag status={c.status} />
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>No couriers recorded.</Empty>
      )}
      {can(admin, "delivery_partners.manage") && partners.items.length ? (
        <div className="mt-6">
          <Panel title="Add a courier">
            <KForm action={saveCourier} submit="Add courier" pending="Saving…" label="Add a courier" reset>
              <div className="grid gap-3 sm:grid-cols-2">
                <Labelled label="Name">
                  <TextInput name="name" />
                </Labelled>
                <Labelled label="Partner">
                  <SelectInput name="partner_id">
                    {partners.items.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Phone">
                  <TextInput name="phone" type="tel" />
                </Labelled>
                <Labelled label="Staff login email (optional)" hint="Must hold the courier role.">
                  <TextInput name="user_email" type="email" />
                </Labelled>
              </div>
            </KForm>
          </Panel>
        </div>
      ) : null}
    </>
  );
}
