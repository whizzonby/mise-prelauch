"use client";

import { cx } from "@mise/ui";
import Link from "next/link";
import { usePathname } from "next/navigation";

const items = [
  { href: "/", label: "Dashboard" },
  { href: "/leads", label: "Leads" },
];

export function ConsoleNav() {
  const pathname = usePathname();
  return (
    <nav aria-label="Admin">
      <ul className="flex gap-1 lg:flex-col">
        {items.map((item) => {
          const current = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
          return (
            <li key={item.href}>
              <Link
                href={item.href}
                aria-current={current ? "page" : undefined}
                className={cx(
                  "block rounded-md px-3 py-2 font-semibold transition-colors duration-(--duration-fast)",
                  current ? "bg-primary-foreground text-primary" : "hover:bg-primary-foreground/15",
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
