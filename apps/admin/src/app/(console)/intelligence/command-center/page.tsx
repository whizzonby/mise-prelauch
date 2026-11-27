import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, Tabs } from "@/components/admin-ui";
import { can, requireAdmin } from "@/lib/session";

import { BriefingTab, FinanceTab, OutlookTab, OverviewTab } from "./tabs-a";
import { CustomersTab, MealsTab, OperationsTab, OpportunitiesTab, RecommendationsTab, RisksTab } from "./tabs-b";
import { AskTab, HistoryTab, ReadinessTab, SavedTab, SettingsTab } from "./tabs-c";

export const metadata: Metadata = { title: "Command center" };

export type SP = Record<string, string | string[] | undefined>;
export type TabProps = { token: string; admin: Awaited<ReturnType<typeof requireAdmin>>["admin"]; sp: SP };

/**
 * Mise Intelligence: the executive command center. Admin only. Every number
 * is computed by the platform from authoritative records; AI (when switched
 * on) only explains those numbers. Each tab is shown only to roles whose
 * permissions allow it, and the API checks again.
 */
export default async function CommandCenterPage({ searchParams }: { searchParams: Promise<SP> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "intelligence.read")) notFound();
  const sp = await searchParams;
  const tabs = [
    { key: "overview", label: "Overview" },
    { key: "briefing", label: "Briefing", hidden: !can(admin, "intelligence_executive.read") },
    { key: "finance", label: "Finance", hidden: !can(admin, "intelligence_financial.read") || !can(admin, "finance_expenses.read") },
    { key: "outlook", label: "Outlook", hidden: !can(admin, "intelligence_scenario.run") },
    { key: "customers", label: "Customers", hidden: !can(admin, "intelligence_customer.read") },
    { key: "meals", label: "Meals", hidden: !can(admin, "intelligence_operational.read") },
    { key: "operations", label: "Operations", hidden: !can(admin, "intelligence_operational.read") },
    { key: "risks", label: "Risks" },
    { key: "opportunities", label: "Opportunities" },
    { key: "recommendations", label: "Recommendations" },
    { key: "ask", label: "Ask Mise AI" },
    { key: "saved", label: "Saved" },
    { key: "history", label: "History" },
    { key: "readiness", label: "Data readiness" },
    { key: "settings", label: "Settings", hidden: !can(admin, "intelligence.configure") },
  ];
  const visible = tabs.filter((t) => !t.hidden);
  const requested = typeof sp.tab === "string" ? sp.tab : "overview";
  const current = visible.some((t) => t.key === requested) ? requested : "overview";
  const props: TabProps = { token, admin, sp };
  const view = {
    overview: OverviewTab,
    briefing: BriefingTab,
    finance: FinanceTab,
    outlook: OutlookTab,
    customers: CustomersTab,
    meals: MealsTab,
    operations: OperationsTab,
    risks: RisksTab,
    opportunities: OpportunitiesTab,
    recommendations: RecommendationsTab,
    ask: AskTab,
    saved: SavedTab,
    history: HistoryTab,
    readiness: ReadinessTab,
    settings: SettingsTab,
  }[current as "overview"];
  const View = view ?? OverviewTab;
  return (
    <>
      <PageHeader
        title="Command center"
        description="Mise Intelligence for management. Figures come from the platform's own records and are calculated without AI; AI, when switched on, only explains them. Everything here is advisory: actions go through the existing workflows."
      />
      <Tabs tabs={visible} current={current} base="/intelligence/command-center" />
      <div className="mt-6">
        <View {...props} />
      </div>
    </>
  );
}
