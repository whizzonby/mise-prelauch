import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { api, can, requireAdmin } from "@/lib/session";

import { updateChef } from "../kitchen-actions";

export const metadata: Metadata = { title: "Chefs" };

/**
 * Chefs: the catalogue chef card linked to the person's staff account. No
 * second login; biographies stay on the catalogue card and are never made up.
 */
export default async function ChefsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "recipes.read")) notFound();
  const manage = can(admin, "chefs.manage");
  const [chefs, staff] = await Promise.all([
    api.kitchen.chefs(token),
    manage && can(admin, "users.read") ? api.admin.staff(token) : Promise.resolve(null),
  ]);
  const chefStaff = staff?.items.filter((m) => m.status === "active" && m.roles.some((r) => r === "chef" || r === "culinary_director")) ?? [];
  return (
    <>
      <PageHeader
        title="Chefs"
        description={
          <>
            Chef cards come from the catalogue (<Link href="/meals" className="underline underline-offset-4">Meals</Link>). Linking a card to a staff account holding the chef role makes that person the owner of recipes they start.
          </>
        }
      />
      {chefs.items.length ? (
        <div className="grid gap-4 lg:grid-cols-2">
          {chefs.items.map((c) => (
            <Panel key={c.id} title={c.display_name} note={c.role_title ?? undefined}>
              <p className="type-body-sm">
                <StatusTag status={c.work_status === "active" ? "active" : "retired"} label={c.work_status === "active" ? "Working with Mise" : "Inactive"} /> ·{" "}
                {c.recipes} recipes · {c.in_review} in review · {c.published} published
              </p>
              <p className="type-body-sm mt-2">
                Staff account: {c.user_name ?? <span className="text-muted">not linked</span>}
                {c.email ? ` (${c.email})` : ""}
              </p>
              <p className="type-body-sm mt-1">Specialities: {c.specialities.length ? c.specialities.join(", ") : <span className="text-muted">none recorded</span>}</p>
              {manage ? (
                <details className="mt-4">
                  <summary className="type-body-sm cursor-pointer font-semibold underline underline-offset-4">Edit team details</summary>
                  <div className="mt-3">
                    <KForm action={updateChef} submit="Save" pending="Saving…" label={`Edit ${c.display_name}`}>
                      <input type="hidden" name="id" value={c.id} />
                      {staff ? (
                        <Labelled label="Staff account">
                          <SelectInput name="user_id" defaultValue={c.user_id ?? ""}>
                            <option value="">Not linked</option>
                            {chefStaff.map((m) => (
                              <option key={m.id} value={m.id}>
                                {m.name || m.email}
                              </option>
                            ))}
                          </SelectInput>
                        </Labelled>
                      ) : (
                        <input type="hidden" name="user_id" value={c.user_id ?? ""} />
                      )}
                      <Labelled label="Specialities" hint="Separated by commas.">
                        <TextInput name="specialities" defaultValue={c.specialities.join(", ")} />
                      </Labelled>
                      <Labelled label="Status">
                        <SelectInput name="work_status" defaultValue={c.work_status}>
                          <option value="active">Working with Mise</option>
                          <option value="inactive">Inactive</option>
                        </SelectInput>
                      </Labelled>
                    </KForm>
                  </div>
                </details>
              ) : null}
            </Panel>
          ))}
        </div>
      ) : (
        <EmptyState>No chef cards yet. Create them in the catalogue first.</EmptyState>
      )}
    </>
  );
}
