import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { iso, ReportView, validDay } from "@/components/supply-ui";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Supplier performance" };

/** Factual figures per supplier. No ratings or scores. */
export default async function SupplierReportPage({ searchParams }: { searchParams: Promise<{ from?: string; to?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "supplier_reports.read")) notFound();
  const sp = await searchParams;
  const today = new Date();
  const to = validDay(sp.to) ?? iso(today);
  const from = validDay(sp.from) ?? iso(new Date(today.getTime() - 29 * 86400000));
  const r = await api.supply.report(token, "suppliers", from, to);
  return (
    <>
      <PageHeader title="Supplier performance" description="Orders placed and delivered, on-time first deliveries, partial deliveries, rejected packs, lead time, purchase value and price changes, per supplier." />
      <ReportView r={r} note="Facts only, each defined: there are no ratings, scores or predictions." />
    </>
  );
}
