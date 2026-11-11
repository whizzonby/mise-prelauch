"use client";

import { cx } from "@mise/ui";
import { Heart, Home, UtensilsCrossed, UserRound } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

/*
 * The customer app's navigation. Three destinations and the account. Future
 * destinations (subscription, orders, deliveries, billing, rewards) are not
 * listed until they exist: a link that leads nowhere is worse than none.
 */
const items = [
  { href: "/dashboard", label: "Home", icon: Home },
  { href: "/meals", label: "Explore meals", short: "Meals", icon: UtensilsCrossed },
  { href: "/favorites", label: "Saved", icon: Heart },
] as const;

function isCurrent(pathname: string, href: string) {
  return pathname === href || pathname.startsWith(href + "/");
}

export function DesktopNav() {
  const pathname = usePathname();
  return (
    <nav aria-label="Main" className="hidden md:block">
      <ul className="flex items-center gap-8">
        {items.map((item) => {
          const current = isCurrent(pathname, item.href);
          return (
            <li key={item.href}>
              <Link
                href={item.href}
                aria-current={current ? "page" : undefined}
                className={cx(
                  "type-body-sm inline-flex min-h-11 items-center border-b-2 font-semibold transition-colors duration-(--duration-fast)",
                  current ? "border-accent text-foreground" : "border-transparent text-muted hover:text-foreground",
                )}
              >
                {item.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

export function AccountLink({ initials }: { initials: string }) {
  const pathname = usePathname();
  const current = isCurrent(pathname, "/profile");
  return (
    <Link
      href="/profile"
      aria-current={current ? "page" : undefined}
      className={cx(
        "hidden items-center gap-2.5 md:inline-flex",
        "type-body-sm min-h-11 font-semibold",
        current ? "text-foreground" : "text-muted hover:text-foreground",
      )}
    >
      <span
        aria-hidden="true"
        className={cx(
          "inline-flex size-9 items-center justify-center rounded-full border font-display text-[0.95rem]",
          current ? "border-primary bg-primary text-primary-foreground" : "border-border-strong bg-surface text-primary",
        )}
      >
        {initials}
      </span>
      Account
    </Link>
  );
}

/** Thumb-reach navigation for phones, fixed to the bottom of the screen. */
export function MobileTabBar() {
  const pathname = usePathname();
  const all = [...items, { href: "/profile", label: "Account", short: "Account", icon: UserRound }];
  return (
    <nav
      aria-label="Main"
      className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-surface pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      <ul className="grid grid-cols-4">
        {all.map((item) => {
          const current = isCurrent(pathname, item.href);
          const Icon = item.icon;
          return (
            <li key={item.href}>
              <Link
                href={item.href}
                aria-current={current ? "page" : undefined}
                className={cx(
                  "flex min-h-16 flex-col items-center justify-center gap-1 text-[0.75rem] font-semibold",
                  current ? "text-primary" : "text-muted",
                )}
              >
                <Icon aria-hidden="true" className="size-[1.375rem]" strokeWidth={current ? 2.25 : 1.75} fill={current && item.href === "/favorites" ? "currentColor" : "none"} />
                {"short" in item ? item.short : item.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
