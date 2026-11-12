import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Roles and permissions" };

/**
 * Which role holds which permission, read from the database the API
 * enforces. Read-only: roles change through a reviewed migration.
 */
export default async function RolesPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "users.read")) notFound();
  const { roles, permissions } = await api.admin.roles(token);
  return (
    <>
      <PageHeader
        title="Roles and permissions"
        description="What each role allows. This page is read-only: role definitions change through a reviewed database migration, so every change is in version control."
      />
      <div className="overflow-x-auto border border-border bg-surface">
        <table className="type-body-sm w-full text-left" style={{ minWidth: `${16 + roles.length * 7}rem` }}>
          <caption className="sr-only">Permissions by role</caption>
          <thead className="border-b border-border">
            <tr>
              <th scope="col" className="sticky left-0 bg-surface p-3 font-semibold">
                Permission
              </th>
              {roles.map((r) => (
                <th key={r.key} scope="col" className="p-3 text-center font-semibold" title={r.description}>
                  {r.name}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {permissions.map((p) => (
              <tr key={p.key}>
                <th scope="row" className="sticky left-0 bg-surface p-3 font-normal">
                  <code className="text-[0.8125rem] font-semibold">{p.key}</code>
                  <span className="type-caption block text-muted">{p.description}</span>
                </th>
                {roles.map((r) => {
                  const has = r.permissions.includes(p.key);
                  return (
                    <td key={r.key} className="p-3 text-center">
                      {has ? <span aria-label="Yes">●</span> : <span aria-label="No" className="text-muted">·</span>}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <dl className="type-body-sm mt-6 grid gap-3 md:grid-cols-2">
        {roles.map((r) => (
          <div key={r.key}>
            <dt className="font-semibold">{r.name}</dt>
            <dd className="text-muted">{r.description}</dd>
          </div>
        ))}
      </dl>
    </>
  );
}
