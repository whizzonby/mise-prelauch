import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { CatalogForm } from "@/components/catalog-form";
import { DataTable, Panel } from "@/components/commerce-ui";
import { SegmentForm } from "@/components/segment-form";
import { formatDate, number } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { deleteSegment } from "../../crm-actions";

export const metadata: Metadata = { title: "Segment" };

export default async function SegmentPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "campaigns.manage")) notFound();
  const { id } = await params;
  const [{ segment, preview }, plans] = await Promise.all([
    api.admin.segment(token, id).catch((e) => {
      if (e instanceof ApiError && e.status === 404) notFound();
      throw e;
    }),
    can(admin, "subscriptions.read") ? api.admin.plans(token).then((p) => p.items) : Promise.resolve([]),
  ]);
  return (
    <>
      <PageHeader
        title={segment.name}
        back={{ href: "/segments", label: "All segments" }}
        description={`${number.format(preview.total)} ${preview.total === 1 ? "customer matches" : "customers match"} right now.`}
        actions={
          can(admin, "customers.read") ? (
            <Link href={`/customers?segment_id=${segment.id}`} className="type-body-sm font-semibold underline underline-offset-4">
              Open in the customer list
            </Link>
          ) : null
        }
      />
      <div className="grid gap-6 xl:grid-cols-2">
        <Panel title="Rules">
          <SegmentForm segment={segment} plans={plans} />
        </Panel>
        <div className="grid content-start gap-6">
          <Panel title="Who matches" note="The first 25, newest first.">
            {preview.items.length ? (
              <DataTable head={["Customer", "Subscription", "Joined"]} minWidth="28rem">
                {preview.items.map((c) => (
                  <tr key={c.id}>
                    <td className="p-3">
                      {can(admin, "customers.read") ? (
                        <Link href={`/customers/${c.id}`} className="underline underline-offset-4">
                          {c.name || c.email}
                        </Link>
                      ) : (
                        c.name || c.email
                      )}
                    </td>
                    <td className="p-3">{c.subscription_status ? <StatusTag status={c.subscription_status} /> : "None"}</td>
                    <td className="p-3">{formatDate(c.joined_at)}</td>
                  </tr>
                ))}
              </DataTable>
            ) : (
              <EmptyState>Nobody matches these rules.</EmptyState>
            )}
          </Panel>
          <Panel title="Delete this segment" note="Campaigns that used it keep their history but lose the link.">
            <CatalogForm action={deleteSegment} submit="Delete segment" pending="Deleting…" label="Delete segment">
              <input type="hidden" name="id" value={segment.id} />
            </CatalogForm>
          </Panel>
        </div>
      </div>
    </>
  );
}
