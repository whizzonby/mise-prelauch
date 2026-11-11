"use client";

import { cx } from "@mise/ui";
import Link from "next/link";
import { usePathname } from "next/navigation";

const sections = [
  { href: "/subscription", label: "Subscription" },
  { href: "/orders", label: "Orders" },
  { href: "/billing", label: "Billing" },
  { href: "/profile", label: "Profile" },
  { href: "/profile/preferences", label: "Food and allergies" },
  { href: "/profile/household", label: "Household" },
  { href: "/profile/addresses", label: "Addresses" },
  { href: "/profile/notifications", label: "Email preferences" },
  { href: "/profile/security", label: "Sign-in and security" },
];

/** Sideways-scrolling tabs on a phone, a column on a large screen. */
export function ProfileNav() {
  const pathname = usePathname();
  return (
    <nav aria-label="Account" className="-mx-(--page-gutter) overflow-x-auto px-(--page-gutter) lg:mx-0 lg:overflow-visible lg:px-0">
      <ul className="flex gap-1 border-b border-border lg:flex-col lg:gap-0 lg:border-b-0 lg:border-l">
        {sections.map((s) => {
          const current = pathname === s.href;
          return (
            <li key={s.href} className="shrink-0">
              <Link
                href={s.href}
                aria-current={current ? "page" : undefined}
                className={cx(
                  "type-body-sm flex min-h-11 items-center whitespace-nowrap px-3 font-semibold transition-colors duration-(--duration-fast)",
                  "border-b-2 lg:-ml-px lg:border-b-0 lg:border-l-2 lg:px-4",
                  current ? "border-accent text-foreground" : "border-transparent text-muted hover:text-foreground",
                )}
              >
                {s.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
