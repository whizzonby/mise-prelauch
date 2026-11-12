import { cx } from "@mise/ui";
import { AlertTriangle, CircleAlert, Info } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";

import { EmptyState, PageHeader } from "@/components/admin-ui";
import { formatDateTime } from "@/lib/format";
import { api, requireAdmin } from "@/lib/session";

import { readAllAlerts } from "../crm-actions";

export const metadata: Metadata = { title: "Alerts" };

const icon = { critical: AlertTriangle, warning: CircleAlert, info: Info } as const;

const LINKS: Record<string, (id: string) => string> = {
  payment: (id) => `/payments/${id}`,
  subscription: (id) => `/subscriptions/${id}`,
  support_case: (id) => `/support/${id}`,
  order: (id) => `/orders/${id}`,
  customer: (id) => `/customers/${id}`,
  job: () => "/payments",
};

/** Alerts for this person: only those their permissions cover. */
export default async function AlertsPage() {
  const { token } = await requireAdmin();
  const { items, unread } = await api.admin.alerts(token);
  return (
    <>
      <PageHeader
        title="Alerts"
        description="Things that went wrong or need a person, raised by the system. Each alert is raised once and shown only to roles that can act on it."
        actions={
          unread ? (
            <form action={readAllAlerts}>
              <button type="submit" className="type-body-sm min-h-11 rounded-md border border-foreground px-4 font-semibold hover:bg-foreground hover:text-surface">
                Mark all {unread} as read
              </button>
            </form>
          ) : null
        }
      />
      {items.length ? (
        <ul className="grid gap-2">
          {items.map((a) => {
            const Icon = icon[a.severity];
            const href = a.resource_type && a.resource_id && LINKS[a.resource_type] ? LINKS[a.resource_type]!(a.resource_id) : null;
            return (
              <li key={a.id} className={cx("flex gap-3 border bg-surface p-4", a.read ? "border-border" : "border-foreground")}>
                <Icon
                  aria-hidden="true"
                  className={cx("mt-0.5 size-5 shrink-0", a.severity === "critical" ? "text-error" : a.severity === "warning" ? "text-warning" : "text-muted")}
                  strokeWidth={1.75}
                />
                <div className="min-w-0 flex-1">
                  <p className="type-body-sm font-semibold">
                    <span className="sr-only">{a.severity}: </span>
                    {a.title}
                    {a.read ? null : <span className="type-caption ml-2 font-semibold uppercase tracking-[0.06em] text-accent">New</span>}
                  </p>
                  {a.body ? <p className="type-body-sm text-muted">{a.body}</p> : null}
                  <p className="type-caption mt-1 text-muted">
                    {formatDateTime(a.created_at)}
                    {a.resolved_at ? ` · resolved ${formatDateTime(a.resolved_at)}` : ""}
                    {href ? (
                      <>
                        {" · "}
                        <Link href={href} className="font-semibold text-foreground underline underline-offset-4">
                          Open
                        </Link>
                      </>
                    ) : null}
                  </p>
                </div>
              </li>
            );
          })}
        </ul>
      ) : (
        <EmptyState>No alerts.</EmptyState>
      )}
    </>
  );
}
