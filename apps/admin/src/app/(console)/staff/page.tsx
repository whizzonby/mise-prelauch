import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { CatalogForm } from "@/components/catalog-form";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { formatDate, formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { invite, revokeInvitation, staffAction } from "../crm-actions";

export const metadata: Metadata = { title: "Staff" };

const PRIVILEGED = ["super_admin", "admin"];

/**
 * Staff accounts and invitations. Every change needs a reason and lands in
 * the audit log. Nobody can change their own access; only a super admin can
 * grant or remove the admin roles; the last super admin cannot be removed.
 */
export default async function StaffPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "users.read")) notFound();
  const manage = can(admin, "users.manage");
  const [{ items, invitations }, { roles }] = await Promise.all([api.admin.staff(token), api.admin.roles(token)]);
  const grantable = roles.filter((r) => !PRIVILEGED.includes(r.key) || admin.roles.includes("super_admin"));

  return (
    <>
      <PageHeader title="Staff" description="People with access to this console. Roles decide what each person can see and do; the API enforces them on every request." />
      <DataTable head={["Person", "Roles", "Status", "Two-step sign-in", "Last sign-in", ...(manage ? ["Change access"] : [])]} minWidth="64rem">
        {items.map((m) => {
          const self = m.id === admin.id;
          return (
            <tr key={m.id} className="align-top">
              <td className="p-3">
                <span className="font-semibold">{m.name || m.email}</span>
                {self ? <span className="type-caption ml-2 text-muted">(you)</span> : null}
                <span className="type-caption block text-muted">{m.email}</span>
              </td>
              <td className="p-3">
                <span className="flex flex-wrap gap-1">
                  {m.roles.map((r) => (
                    <Tag key={r} tone={PRIVILEGED.includes(r) ? "accent" : "neutral"}>
                      {humanize(r)}
                    </Tag>
                  ))}
                </span>
              </td>
              <td className="p-3">
                <StatusTag status={m.status === "active" ? "active" : "cancelled"} label={humanize(m.status)} />
              </td>
              <td className="p-3">{m.mfa_enrolled ? "Set up" : "Not set up"}</td>
              <td className="p-3">{m.last_login_at ? formatDateTime(m.last_login_at) : "Never"}</td>
              {manage ? (
                <td className="p-3">
                  {self ? (
                    <span className="type-caption text-muted">Ask another administrator to change your access.</span>
                  ) : (
                    <details>
                      <summary className="type-body-sm cursor-pointer font-semibold underline underline-offset-4">Change</summary>
                      <div className="mt-3 grid w-72 gap-3">
                        <CatalogForm action={staffAction} submit="Apply" pending="Applying…" label={`Change access for ${m.email}`}>
                          <input type="hidden" name="id" value={m.id} />
                          <Labelled label="Action">
                            <SelectInput name="action" defaultValue="roles">
                              <option value="roles">Add a role</option>
                              <option value="roles/revoke">Remove a role</option>
                              {m.status === "active" ? <option value="disable">Disable the account</option> : <option value="enable">Enable the account</option>}
                              <option value="signout">Sign out everywhere</option>
                            </SelectInput>
                          </Labelled>
                          <Labelled label="Role" hint="For adding or removing a role.">
                            <SelectInput name="role" defaultValue="">
                              <option value="">Choose…</option>
                              {grantable.map((r) => (
                                <option key={r.key} value={r.key}>
                                  {r.name}
                                </option>
                              ))}
                            </SelectInput>
                          </Labelled>
                          <Labelled label="Reason" hint="Recorded in the audit log.">
                            <TextInput name="reason" required minLength={3} maxLength={300} />
                          </Labelled>
                        </CatalogForm>
                      </div>
                    </details>
                  )}
                </td>
              ) : null}
            </tr>
          );
        })}
      </DataTable>

      <div className="mt-8 grid gap-6 xl:grid-cols-2">
        <Panel title="Open invitations">
          {invitations.length ? (
            <ul className="grid gap-3">
              {invitations.map((i) => (
                <li key={i.id} className="flex flex-wrap items-start justify-between gap-3 border-b border-border pb-3 last:border-0">
                  <span className="type-body-sm">
                    <strong>{i.name || i.email}</strong> as {humanize(i.role)}
                    <span className="type-caption block text-muted">
                      {i.email} · sent {formatDate(i.created_at)} · expires {formatDate(i.expires_at)}
                    </span>
                  </span>
                  {manage ? (
                    <CatalogForm action={revokeInvitation} submit="Revoke" pending="Revoking…" label={`Revoke invitation for ${i.email}`}>
                      <input type="hidden" name="id" value={i.id} />
                    </CatalogForm>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState>No open invitations.</EmptyState>
          )}
        </Panel>
        {manage ? (
          <Panel title="Invite someone" note="They get an email with a link that works once, for 7 days, to set their password.">
            <CatalogForm action={invite} submit="Send invitation" pending="Sending…" label="Invite staff">
              <Labelled label="Name">
                <TextInput name="name" required maxLength={100} />
              </Labelled>
              <Labelled label="Email">
                <TextInput type="email" name="email" required maxLength={254} />
              </Labelled>
              <Labelled label="Role">
                <SelectInput name="role" defaultValue="support">
                  {grantable.map((r) => (
                    <option key={r.key} value={r.key}>
                      {r.name}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
            </CatalogForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
