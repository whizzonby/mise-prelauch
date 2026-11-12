import { buttonClasses } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader } from "@/components/admin-ui";
import { DataTable } from "@/components/commerce-ui";
import { formatDate, number } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Segments" };

export default async function SegmentsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "campaigns.manage")) notFound();
  const { items } = await api.admin.segments(token);
  return (
    <>
      <PageHeader
        title="Segments"
        description="Saved groups of customers, defined by rules. Membership is worked out each time, so a segment is always current."
        actions={
          <Link href="/segments/new" className={buttonClasses("primary")}>
            New segment
          </Link>
        }
      />
      {items.length ? (
        <DataTable head={["Segment", "Description", "Customers now", "Updated"]}>
          {items.map((s) => (
            <tr key={s.id}>
              <td className="p-3">
                <Link href={`/segments/${s.id}`} className="font-semibold underline underline-offset-4">
                  {s.name}
                </Link>
              </td>
              <td className="p-3">{s.description}</td>
              <td className="p-3">{s.customers === undefined ? "" : number.format(s.customers)}</td>
              <td className="p-3">{formatDate(s.updated_at)}</td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No segments yet.</EmptyState>
      )}
    </>
  );
}
