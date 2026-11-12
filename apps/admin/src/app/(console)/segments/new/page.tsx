import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { Panel } from "@/components/commerce-ui";
import { SegmentForm } from "@/components/segment-form";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "New segment" };

export default async function NewSegmentPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "campaigns.manage")) notFound();
  const plans = can(admin, "subscriptions.read") ? (await api.admin.plans(token)).items : [];
  return (
    <>
      <PageHeader title="New segment" back={{ href: "/segments", label: "All segments" }} />
      <Panel className="max-w-[56rem]">
        <SegmentForm plans={plans} />
      </Panel>
    </>
  );
}
