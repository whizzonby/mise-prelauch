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
        ...item(admin, "cases.read", "/support", "Support cases"),
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
      label: "Reports",
      items: [
        ...item(admin, "reports.read", "/reports", "Reports"),
        ...item(admin, "production_reports.read", "/reports/production", "Production"),
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
        { href: "/settings", label: "Settings" },
      ],
    },
  ];

  return (
    <div className="min-h-svh lg:grid lg:grid-cols-[16rem_1fr]">
      <header className="on-dark flex flex-col gap-3 bg-primary px-5 py-3 text-primary-foreground lg:sticky lg:top-0 lg:h-svh lg:gap-6 lg:overflow-y-auto lg:px-4 lg:py-6">
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
