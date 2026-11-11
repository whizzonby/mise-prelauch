"use client";

import { cx } from "@mise/ui";
import Link from "next/link";
import { usePathname } from "next/navigation";

const tabs = [
  { href: "/subscription", label: "Subscription" },
  { href: "/orders", label: "Orders" },
  { href: "/billing", label: "Billing" },
];

/** Subscription, orders and billing: three views of the same arrangement. */
export function AccountTabs() {
  const pathname = usePathname();
  return (
    <nav aria-label="Your subscription" className="-mx-(--page-gutter) overflow-x-auto px-(--page-gutter)">
      <ul className="flex gap-1 border-b border-border">
        {tabs.map((t) => {
          const current = pathname === t.href || pathname.startsWith(t.href + "/");
          return (
            <li key={t.href} className="shrink-0">
              <Link
                href={t.href}
                aria-current={current ? "page" : undefined}
                className={cx(
                  "type-body-sm flex min-h-11 items-center border-b-2 px-3 font-semibold transition-colors duration-(--duration-fast)",
                  current ? "border-accent text-foreground" : "border-transparent text-muted hover:text-foreground",
                )}
              >
                {t.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
