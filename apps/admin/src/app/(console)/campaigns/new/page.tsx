import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { CampaignForm } from "@/components/campaign-form";
import { Panel } from "@/components/commerce-ui";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "New campaign" };

export default async function NewCampaignPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "campaigns.manage")) notFound();
  const [segments, promotions] = await Promise.all([
    api.admin.segments(token).then((s) => s.items),
    can(admin, "promotions.manage") ? api.admin.promotions(token).then((p) => p.items) : Promise.resolve([]),
  ]);
  return (
    <>
      <PageHeader title="New campaign" back={{ href: "/campaigns", label: "All campaigns" }} />
      <Panel className="max-w-[56rem]">
        <CampaignForm segments={segments} promotions={promotions} />
      </Panel>
    </>
  );
}
