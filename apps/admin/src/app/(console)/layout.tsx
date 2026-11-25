import type { Permission } from "@mise/api-client";
import { Wordmark } from "@mise/ui";
import { Bell } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

import { logout } from "@/app/actions";
import { ConsoleNav, type NavGroup } from "@/components/console-nav";
import { humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

/** One entry: shown only to staff holding the permission (checked again by the API). */
function item(admin: Parameters<typeof can>[0], perm: Permission, href: string, label: string) {
  return can(admin, perm) ? [{ href, label }] : [];
}

export default async function ConsoleLayout({ children }: { children: ReactNode }) {
  const { admin, token } = await requireAdmin();
  const alerts = await api.admin.alerts(token, true).catch(() => ({ unread: 0 }));

  const groups: NavGroup[] = [
    { label: "Overview", items: [{ href: "/", label: "Overview" }] },
    {
      label: "Customers",
      items: [
        ...item(admin, "customers.read", "/customers", "All customers"),
        ...item(admin, "campaigns.manage", "/segments", "Segments"),
        ...item(admin, "customers.read", "/retention", "Retention"),
      ],
    },
    {
      label: "CRM",
      items: [
        ...item(admin, "leads.read", "/leads", "Leads"),
        ...item(admin, "support_chat.read", "/support/chat", "Support chat"),
        ...item(admin, "cases.read", "/support", "Support cases"),
        ...item(admin, "help_centre.edit", "/help-centre", "Help centre"),
        ...item(admin, "campaigns.manage", "/campaigns", "Campaigns"),
        ...item(admin, "leads.read", "/referrals", "Referrals"),
      ],
    },
    {
      label: "Commerce",
      items: [
        ...item(admin, "subscriptions.read", "/subscriptions", "Subscriptions"),
        ...item(admin, "orders.read", "/orders", "Orders"),
        ...item(admin, "payments.read", "/payments", "Payments"),
        ...item(admin, "payment_providers.read", "/payments/providers", "Payment providers"),
        ...item(admin, "payments.read", "/invoices", "Invoices"),
        ...item(admin, "promotions.manage", "/promotions", "Promotions"),
      ],
    },
    {
      label: "Kitchen & Production",
      items: [
        ...item(admin, "production.read", "/production", "Production overview"),
        ...item(admin, "production.read", "/production/tasks", "Kitchen tasks"),
        ...item(admin, "production.read", "/production/calendar", "Calendar"),
        ...item(admin, "production.read", "/production/plans", "Production plans"),
        ...item(admin, "production.read", "/production/batches", "Batches"),
        ...item(admin, "quality.read", "/production/quality", "Quality control"),
        ...item(admin, "packaging.manage", "/production/packaging", "Packaging"),
        ...item(admin, "production.read", "/production/waste", "Waste & yield"),
        ...item(admin, "production.configure", "/production/settings", "Kitchen settings"),
      ],
    },
    {
      label: "Delivery & fulfillment",
      items: [
        ...item(admin, "delivery_reports.read", "/delivery", "Overview"),
        ...item(admin, "fulfillment.read", "/fulfillment", "Fulfillment queue"),
        ...item(admin, "delivery_zones.read", "/delivery/calendar", "Delivery calendar"),
        ...item(admin, "delivery.read", "/delivery/planning", "Dispatch planning"),
        ...(can(admin, "delivery.read") || can(admin, "delivery.read_assigned")
          ? [{ href: "/delivery/runs", label: can(admin, "delivery.read") ? "Dispatch runs" : "My runs" }]
          : []),
        ...item(admin, "delivery_partners.read", "/delivery/partners", "Delivery partners"),
        ...item(admin, "delivery_partners.read", "/delivery/couriers", "Couriers"),
        ...item(admin, "delivery_zones.read", "/delivery/zones", "Delivery zones"),
        ...item(admin, "delivery_exceptions.read", "/delivery/exceptions", "Tracking & exceptions"),
        ...item(admin, "delivery_exceptions.read", "/delivery/returns", "Returns"),
        ...item(admin, "delivery.read", "/delivery/reconciliation", "Reconciliation"),
        ...item(admin, "delivery_reports.read", "/reports/delivery", "Delivery reports"),
        ...item(admin, "delivery_zones.read", "/delivery/settings", "Delivery settings"),
      ],
    },
    {
      label: "Supply chain",
      items: [
        ...(can(admin, "procurement.read") || can(admin, "inventory.read") ? [{ href: "/supply", label: "Supply overview" }] : []),
        ...item(admin, "procurement.read", "/procurement", "Procurement planning"),
        ...item(admin, "receiving.read", "/purchase-orders", "Purchase orders"),
        ...item(admin, "procurement.read", "/quotations", "Quotations"),
        ...item(admin, "receiving.read", "/receiving", "Goods receiving"),
        ...item(admin, "inventory.read", "/inventory", "Inventory"),
        ...item(admin, "inventory.read", "/inventory/lots", "Ingredient lots"),
        ...item(admin, "inventory.read", "/inventory/movements", "Stock movements"),
        ...item(admin, "inventory.read", "/inventory/counts", "Stock counts"),
        ...item(admin, "traceability.read", "/traceability", "Traceability"),
        ...(can(admin, "procurement.read") || can(admin, "inventory.configure") ? [{ href: "/supply/settings", label: "Supply settings" }] : []),
      ],
    },
    {
      label: "Suppliers",
      items: [
        ...item(admin, "suppliers.read", "/suppliers", "Supplier directory"),
        ...item(admin, "suppliers.read", "/suppliers?tab=farmers", "Farmers"),
        ...item(admin, "supplier_products.read", "/supplier-products", "Supplier products"),
        ...item(admin, "supplier_products.read", "/supplier-availability", "Availability"),
        ...item(admin, "supplier_reports.read", "/reports/suppliers", "Supplier performance"),
      ],
    },
    {
      label: "Culinary",
      items: [
        ...item(admin, "recipes.read", "/recipes", "Recipes"),
        ...item(admin, "ingredients.read", "/ingredients", "Ingredients"),
        ...item(admin, "recipes.read", "/chefs", "Chefs"),
        ...item(admin, "nutrition.read", "/nutrition", "Nutrition reviews"),
        ...item(admin, "recipes.review", "/recipe-approvals", "Recipe approvals"),
      ],
    },
    {
      label: "Catalog",
      items: [
        ...item(admin, "catalog.read", "/meals", "Meals"),
        ...item(admin, "menus.manage", "/menus", "Weekly menus"),
        ...item(admin, "subscriptions.read", "/plans", "Plans"),
      ],
    },
    {
      label: "Intelligence",
      items: [
        ...(["analytics_executive.read", "analytics_revenue.read", "analytics_customers.read", "analytics_production.read", "analytics_procurement.read",
          "analytics_inventory.read", "analytics_delivery.read", "support_chat.analytics"] as Permission[]).some((p) => can(admin, p))
          ? [{ href: "/intelligence", label: "Insights" }, { href: "/intelligence/metrics", label: "Metric catalogue" }]
          : [],
        ...item(admin, "analytics_executive.read", "/intelligence/quality", "Data quality"),
        ...item(admin, "forecasting.read", "/forecasting", "Forecasting"),
      ],
    },
    {
      label: "Automation",
      items: [
        ...item(admin, "automation.read", "/automation", "Rules"),
        ...item(admin, "automation.read", "/automation/executions", "Executions"),
        { href: "/automation/tasks", label: "My tasks" },
        ...item(admin, "automation.read", "/automation/reports", "Scheduled reports"),
      ],
    },
    {
      label: "Reports",
      items: [
        ...item(admin, "reports.read", "/reports", "Reports"),
        ...item(admin, "production_reports.read", "/reports/production", "Production"),
        ...item(admin, "supplier_reports.read", "/reports/procurement", "Procurement"),
        ...item(admin, "supplier_reports.read", "/reports/inventory", "Inventory"),
        ...item(admin, "production.read", "/production/requirements", "Ingredient requirements"),
        ...item(admin, "analytics.view", "/reports/waitlist", "Waitlist"),
        { href: "/exports", label: "Exports" },
      ],
    },
    {
      label: "Administration",
      items: [
        ...item(admin, "users.read", "/staff", "Staff"),
        ...item(admin, "users.read", "/roles", "Roles and permissions"),
        ...item(admin, "audit.read", "/audit", "Audit log"),
        ...item(admin, "launch.read", "/launch", "Launch readiness"),
        ...item(admin, "launch.read", "/system", "System status"),
        { href: "/settings", label: "Settings" },
      ],
    },
  ];

  return (
    <div className="min-h-svh lg:grid lg:grid-cols-[16rem_1fr]">
      <header className="on-dark flex flex-col gap-3 bg-primary print:hidden px-5 py-3 text-primary-foreground lg:sticky lg:top-0 lg:h-svh lg:gap-6 lg:overflow-y-auto lg:px-4 lg:py-6">
        <div className="flex items-center justify-between gap-4">
          <Link href="/" className="text-[1.75rem]" aria-label="Mise admin home">
            <Wordmark />
          </Link>
          <Link
            href="/alerts"
            className="relative inline-flex size-10 items-center justify-center rounded-md hover:bg-primary-foreground/15"
            aria-label={alerts.unread ? `Alerts, ${alerts.unread} unread` : "Alerts"}
          >
            <Bell aria-hidden="true" className="size-5" strokeWidth={1.75} />
            {alerts.unread ? (
              <span className="absolute -right-0.5 -top-0.5 min-w-5 rounded-full bg-accent px-1 text-center text-[0.6875rem] font-bold leading-5 text-accent-foreground">
                {alerts.unread > 99 ? "99+" : alerts.unread}
              </span>
            ) : null}
          </Link>
        </div>

        <ConsoleNav groups={groups} />

        <div className="hidden items-center gap-4 lg:mt-auto lg:block lg:border-t lg:border-primary-foreground/25 lg:pt-5">
          <p>
            <span className="block font-semibold">{admin.name}</span>
            <span className="type-caption block text-primary-foreground/75">{admin.roles.map(humanize).join(", ")}</span>
          </p>
          <form action={logout} className="mt-3">
            <button type="submit" className="type-body-sm underline underline-offset-4 hover:decoration-2">
              Sign out
            </button>
          </form>
        </div>
      </header>

      <main id="main" className="min-w-0 px-5 py-8 lg:px-10 lg:py-10">
        <div className="mx-auto max-w-[80rem]">{children}</div>
        <form action={logout} className="mt-10 border-t border-border pt-4 lg:hidden">
          <button type="submit" className="type-body-sm underline underline-offset-4">
            Sign out {admin.name}
          </button>
        </form>
      </main>
    </div>
  );
}
