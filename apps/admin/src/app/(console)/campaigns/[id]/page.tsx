import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { CampaignForm, campaignTone } from "@/components/campaign-form";
import { CatalogForm } from "@/components/catalog-form";
import { Panel } from "@/components/commerce-ui";
import { StatTile } from "@/components/panel";
import { humanize, number } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { campaignStatus } from "../../crm-actions";

export const metadata: Metadata = { title: "Campaign" };

const NEXT: Record<string, string[]> = {
  draft: ["scheduled", "active", "cancelled"],
  scheduled: ["draft", "active", "cancelled"],
  active: ["completed", "cancelled"],
  completed: [],
  cancelled: [],
};

export default async function CampaignPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "campaigns.manage")) notFound();
  const { id } = await params;
  const [{ campaign, attribution }, segments, promotions] = await Promise.all([
    api.admin.campaign(token, id).catch((e) => {
      if (e instanceof ApiError && e.status === 404) notFound();
      throw e;
    }),
    api.admin.segments(token).then((s) => s.items),
    can(admin, "promotions.manage") ? api.admin.promotions(token).then((p) => p.items) : Promise.resolve([]),
  ]);
  const editable = campaign.status === "draft" || campaign.status === "scheduled";
  return (
    <>
      <PageHeader
        title={campaign.name}
        back={{ href: "/campaigns", label: "All campaigns" }}
        description={
          <>
            <StatusTag status={campaignTone(campaign.status)} label={humanize(campaign.status)} /> · {humanize(campaign.objective)} · {humanize(campaign.channel)}
          </>
        }
      />
      <section aria-label="Results" className="mb-6">
        {campaign.utm_campaign ? (
          <>
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
              <StatTile label="Leads" value={number.format(attribution.leads)} />
              <StatTile label="Confirmed their email" value={number.format(attribution.verified_leads)} />
              <StatTile label="Became customers" value={number.format(attribution.customers)} />
              <StatTile label="Subscribed" value={number.format(attribution.subscribers)} />
              <StatTile label="Orders" value={number.format(attribution.orders)} />
            </div>
            <p className="type-caption mt-2 text-muted">{attribution.definition}</p>
          </>
        ) : (
          <p className="type-body-sm text-muted">Add a UTM tag to count the sign-ups this campaign brings in.</p>
        )}
      </section>
      <div className="grid gap-6 xl:grid-cols-[1fr_22rem]">
        <Panel title="Details" note={editable ? undefined : "Active and finished campaigns keep their details as they were."}>
          {editable ? (
            <CampaignForm campaign={campaign} segments={segments} promotions={promotions} />
          ) : (
            <p className="type-body-sm whitespace-pre-line">{campaign.notes || "No notes."}</p>
          )}
        </Panel>
        {NEXT[campaign.status]!.length ? (
          <Panel title="Status">
            <div className="grid gap-3">
              {NEXT[campaign.status]!.map((s) => (
                <CatalogForm key={s} action={campaignStatus} submit={`Mark as ${humanize(s).toLowerCase()}`} pending="Saving…" label={`Mark as ${s}`}>
                  <input type="hidden" name="id" value={campaign.id} />
                  <input type="hidden" name="status" value={s} />
                </CatalogForm>
              ))}
            </div>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
