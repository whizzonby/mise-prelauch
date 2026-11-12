import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, Pager, Tabs } from "@/components/admin-ui";
import { DataTable } from "@/components/commerce-ui";
import { formatDate, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Referrals" };

const FLAGS: Record<string, string> = {
  same_network: "Same network as the referrer",
  burst: "Many sign-ups in a short time",
  blocked: "Blocked",
  flagged_at_signup: "Flagged at sign-up",
};

/**
 * Referral oversight: who invited whom, whether it led to a customer, and
 * the patterns worth a second look. Flags are signals for a person to
 * review, not verdicts; nothing here blocks anyone automatically.
 */
export default async function ReferralsPage({ searchParams }: { searchParams: Promise<{ tab?: string; page?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "leads.read")) notFound();
  const { tab = "all", page: rawPage } = await searchParams;
  const page = Math.max(Number(rawPage) || 1, 1);
  const list = await api.admin.referralsReview(token, tab === "flagged", page);
  return (
    <>
      <PageHeader title="Referrals" description="Invitations between waitlist members. Flags are prompts to look closer, not proof of abuse." />
      <Tabs
        base="/referrals"
        current={tab}
        tabs={[
          { key: "all", label: "All referrals" },
          { key: "flagged", label: "Flagged" },
        ]}
      />
      <div className="mt-5">
        {list.items.length ? (
          <DataTable head={["Invited", "By", "Status", "Customer", "Flags", "Invited on"]} minWidth="56rem">
            {list.items.map((r) => (
              <tr key={r.id}>
                <td className="p-3">
                  <Link href={`/leads/${r.referred_lead_id}`} className="font-semibold underline underline-offset-4">
                    {r.referred_name}
                  </Link>
                  <span className="type-caption block text-muted">{r.referred_email}</span>
                </td>
                <td className="p-3">
                  <Link href={`/leads/${r.referrer_lead_id}`} className="underline underline-offset-4">
                    {r.referrer_name}
                  </Link>
                </td>
                <td className="p-3">{humanize(r.status.toLowerCase())}</td>
                <td className="p-3">{r.referred_became_customer ? "Yes" : "No"}</td>
                <td className="p-3">
                  <span className="flex flex-wrap gap-1">
                    {r.flags.map((f) => (
                      <Tag key={f} tone={f === "blocked" ? "error" : "warning"}>
                        {FLAGS[f] ?? f}
                      </Tag>
                    ))}
                  </span>
                </td>
                <td className="p-3">{formatDate(r.created_at)}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <EmptyState>{tab === "flagged" ? "No flagged referrals." : "No referrals yet."}</EmptyState>
        )}
        <Pager page={page} size={50} total={list.total} params={{ tab: tab === "all" ? undefined : tab }} />
      </div>
    </>
  );
}
