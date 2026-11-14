import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { iso, ReportView, validDay } from "@/components/supply-ui";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Inventory report" };

/** Stock activity from the ledger. Every figure is defined. */
export default async function InventoryReportPage({ searchParams }: { searchParams: Promise<{ from?: string; to?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "supplier_reports.read")) notFound();
  const sp = await searchParams;
  const today = new Date();
  const to = validDay(sp.to) ?? iso(today);
  const from = validDay(sp.from) ?? iso(new Date(today.getTime() - 29 * 86400000));
  const r = await api.supply.report(token, "inventory", from, to);
  return (
    <>
      <PageHeader title="Inventory report" description="Lots received and rejected, issues and returns, adjustments, store waste, counts, and stock now." />
      <ReportView r={r} note="Quantities are in each ingredient's base unit; production waste is in the production report and is not counted again here." />
    </>
  );
}
