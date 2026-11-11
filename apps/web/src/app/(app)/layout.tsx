import { Container, Wordmark } from "@mise/ui";
import Link from "next/link";
import type { ReactNode } from "react";

import { AccountLink, DesktopNav, MobileTabBar } from "@/components/app-nav";
import { initials } from "@/lib/format";
import { requireCustomer } from "@/lib/session";

const siteUrl = process.env.NEXT_PUBLIC_SITE_URL ?? "http://localhost:3100";

/** The signed-in customer app: one header, the page, a quiet footer. */
export default async function AppLayout({ children }: { children: ReactNode }) {
  const { customer } = await requireCustomer();
  return (
    <div className="flex min-h-svh flex-col">
      <header className="sticky top-0 z-30 border-b border-border bg-background">
        <Container className="flex h-16 items-center justify-between gap-6 md:h-18">
          <Link href="/dashboard" aria-label="Mise home" className="text-[1.875rem] text-primary">
            <Wordmark />
          </Link>
          <DesktopNav />
          <AccountLink initials={initials(customer.first_name, customer.last_name)} />
          <Link href="/profile" className="type-body-sm font-semibold underline underline-offset-4 md:hidden">
            {customer.first_name}
          </Link>
        </Container>
      </header>

      <main id="main" className="flex-1 pb-24 md:pb-0">
        {children}
      </main>

      <footer className="border-t border-border pb-24 md:pb-0">
        <Container className="type-caption flex flex-wrap items-center justify-between gap-x-6 gap-y-2 py-6 text-muted">
          <span>Mise. Caribbean meal kits, cooked at home in Trinidad and Tobago.</span>
          <span className="flex gap-5">
            <a href={`${siteUrl}/privacy`} className="underline-offset-4 hover:underline">
              Privacy
            </a>
            <a href={`${siteUrl}/terms`} className="underline-offset-4 hover:underline">
              Terms
            </a>
            <a href={siteUrl} className="underline-offset-4 hover:underline">
              mise.tt
            </a>
          </span>
        </Container>
      </footer>

      <MobileTabBar />
    </div>
  );
}
