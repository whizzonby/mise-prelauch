import type { Customer } from "@mise/api-client";
import { buttonClasses, Container, Wordmark } from "@mise/ui";
import Link from "next/link";
import type { ReactNode } from "react";

import { AccountLink, DesktopNav, MobileTabBar } from "@/components/app-nav";
import { initials } from "@/lib/format";

const siteUrl = process.env.NEXT_PUBLIC_SITE_URL ?? "http://localhost:3100";

/**
 * The one frame around every customer-app page. A signed-in customer gets the
 * account navigation; a visitor browsing meals and plans gets a short header
 * with sign in and get started. Same type, spacing and footer either way, so
 * crossing from browsing to an account does not feel like a different site.
 */
export function SiteShell({ customer, children }: { customer: Customer | null; children: ReactNode }) {
  return (
    <div className="flex min-h-svh flex-col">
      <header className="sticky top-0 z-30 border-b border-border bg-background">
        <Container className="flex h-16 items-center justify-between gap-6 md:h-18">
          <Link href={customer ? "/dashboard" : "/meals"} aria-label="Mise home" className="text-[1.875rem] text-primary">
            <Wordmark />
          </Link>
          {customer ? (
            <>
              <DesktopNav />
              <AccountLink initials={initials(customer.first_name, customer.last_name)} />
              <Link href="/profile" className="type-body-sm font-semibold underline underline-offset-4 md:hidden">
                {customer.first_name}
              </Link>
            </>
          ) : (
            <>
              <nav aria-label="Main" className="hidden md:block">
                <ul className="flex items-center gap-7">
                  <li>
                    <Link href="/meals" className="type-body-sm inline-flex min-h-11 items-center font-semibold text-muted hover:text-foreground">
                      Meal kits
                    </Link>
                  </li>
                  <li>
                    <Link href="/plans" className="type-body-sm inline-flex min-h-11 items-center font-semibold text-muted hover:text-foreground">
                      Plans
                    </Link>
                  </li>
                  <li>
                    <a href={`${siteUrl}/how-it-works`} className="type-body-sm inline-flex min-h-11 items-center font-semibold text-muted hover:text-foreground">
                      How it works
                    </a>
                  </li>
                </ul>
              </nav>
              <div className="flex items-center gap-2">
                <Link href="/sign-in" className="type-body-sm inline-flex min-h-11 items-center px-2 font-semibold underline-offset-4 hover:underline">
                  Sign in
                </Link>
                <Link href="/plans" className={buttonClasses("primary", "md")}>
                  Get started
                </Link>
              </div>
            </>
          )}
        </Container>
      </header>

      <main id="main" className={customer ? "flex-1 pb-24 md:pb-0" : "flex-1"}>
        {children}
      </main>

      <footer className={customer ? "border-t border-border pb-24 md:pb-0" : "border-t border-border"}>
        <Container className="type-caption flex flex-wrap items-center justify-between gap-x-6 gap-y-2 py-6 text-muted">
          <span>Mise. Caribbean meal kits, cooked at home in Trinidad and Tobago.</span>
          <span className="flex flex-wrap gap-x-5 gap-y-1">
            <a href={`${siteUrl}/faq`} className="inline-flex min-h-11 items-center underline-offset-4 hover:underline">
              FAQ
            </a>
            <a href={`${siteUrl}/contact`} className="inline-flex min-h-11 items-center underline-offset-4 hover:underline">
              Contact
            </a>
            <a href={`${siteUrl}/privacy`} className="inline-flex min-h-11 items-center underline-offset-4 hover:underline">
              Privacy
            </a>
            <a href={`${siteUrl}/terms`} className="inline-flex min-h-11 items-center underline-offset-4 hover:underline">
              Terms
            </a>
          </span>
        </Container>
      </footer>

      {customer ? <MobileTabBar /> : null}
    </div>
  );
}
