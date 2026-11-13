"use client";

import { cx } from "@mise/ui";
import Link from "next/link";
import { usePathname } from "next/navigation";

export interface NavItem {
  href: string;
  label: string;
}

export interface NavGroup {
  label: string;
  items: NavItem[];
}

function isCurrent(pathname: string, href: string) {
  return href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(href + "/");
}

/**
 * The console's navigation, grouped by area. The layout passes only what
 * this person's permissions allow, and empty groups are left out. Modules
 * that do not exist yet (production, inventory, deliveries) are not listed.
 */
export function ConsoleNav({ groups }: { groups: NavGroup[] }) {
  const pathname = usePathname();
  // The most specific matching link is the current one ("/production/tasks"
  // rather than "/production").
  const best = groups
    .flatMap((g) => g.items)
    .filter((i) => isCurrent(pathname, i.href))
    .reduce<string | null>((a, i) => (a === null || i.href.length > a.length ? i.href : a), null);
  return (
    <nav aria-label="Admin" className="w-full">
      <ul className="flex gap-x-1 gap-y-3 overflow-x-auto lg:flex-col lg:overflow-visible">
        {groups
          .filter((g) => g.items.length)
          .map((g) => (
            <li key={g.label} className="shrink-0">
              <p className="type-caption hidden px-3 pb-1 font-semibold uppercase tracking-[0.08em] text-primary-foreground/60 lg:block">{g.label}</p>
              <ul className="flex gap-1 lg:flex-col lg:gap-0">
                {g.items.map((item) => {
                  const current = item.href === best;
                  return (
                    <li key={item.href} className="shrink-0">
                      <Link
                        href={item.href}
                        aria-current={current ? "page" : undefined}
                        className={cx(
                          "type-body-sm block whitespace-nowrap rounded-md px-3 py-1.5 font-semibold transition-colors duration-(--duration-fast)",
                          current ? "bg-primary-foreground text-primary" : "hover:bg-primary-foreground/15",
                        )}
                      >
                        {item.label}
                      </Link>
                    </li>
                  );
                })}
              </ul>
            </li>
          ))}
      </ul>
    </nav>
  );
}
