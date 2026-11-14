import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { iso, ReportView, validDay } from "@/components/supply-ui";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Procurement report" };

/** Purchasing activity: runs, shortages, orders, deliveries, quotations. Every figure is defined. */
export default async function ProcurementReportPage({ searchParams }: { searchParams: Promise<{ from?: string; to?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "supplier_reports.read")) notFound();
  const sp = await searchParams;
  const today = new Date();
  const to = validDay(sp.to) ?? iso(today);
  const from = validDay(sp.from) ?? iso(new Date(today.getTime() - 29 * 86400000));
  const r = await api.supply.report(token, "procurement", from, to);
  return (
    <>
      <PageHeader title="Procurement report" description="Runs, shortages, orders approved and issued, deliveries, open and overdue orders, requests and quotations." />
      <ReportView r={r} note="Money is in TTD only: no exchange rate is configured, so orders in another currency are left out of value totals. No margins are shown: production costs are not complete." />
    </>
  );
}
