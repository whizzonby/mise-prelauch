import { buttonClasses } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { campaignTone } from "@/components/campaign-form";
import { DataTable } from "@/components/commerce-ui";
import { formatDate, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Campaigns" };


export default async function CampaignsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "campaigns.manage")) notFound();
  const { items } = await api.admin.campaigns(token);
  return (
    <>
      <PageHeader
        title="Campaigns"
        description="Records of marketing campaigns and what they brought in, matched by UTM tag. No email provider is connected, so Mise does not send campaigns itself."
        actions={
          <Link href="/campaigns/new" className={buttonClasses("primary")}>
            New campaign
          </Link>
        }
      />
      {items.length ? (
        <DataTable head={["Campaign", "Status", "Objective", "Channel", "UTM tag", "Runs"]} minWidth="52rem">
          {items.map((c) => (
            <tr key={c.id}>
              <td className="p-3">
                <Link href={`/campaigns/${c.id}`} className="font-semibold underline underline-offset-4">
                  {c.name}
                </Link>
              </td>
              <td className="p-3">
                <StatusTag status={campaignTone(c.status)} label={humanize(c.status)} />
              </td>
              <td className="p-3">{humanize(c.objective)}</td>
              <td className="p-3">{humanize(c.channel)}</td>
              <td className="p-3 font-mono text-[0.8125rem]">{c.utm_campaign ?? ""}</td>
              <td className="p-3">
                {c.starts_at ? formatDate(c.starts_at) : "Not set"}
                {c.ends_at ? ` – ${formatDate(c.ends_at)}` : ""}
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No campaigns yet.</EmptyState>
      )}
    </>
  );
}
