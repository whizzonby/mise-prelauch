import { Wordmark } from "@mise/ui";
import Link from "next/link";
import type { ReactNode } from "react";

import { logout } from "@/app/actions";
import { ConsoleNav } from "@/components/console-nav";
import { humanize } from "@/lib/format";
import { requireAdmin } from "@/lib/session";

export default async function ConsoleLayout({ children }: { children: ReactNode }) {
  const { admin } = await requireAdmin();

  return (
    <div className="min-h-svh lg:grid lg:grid-cols-[15rem_1fr]">
      <header className="on-dark flex items-center justify-between gap-4 bg-primary px-5 py-3 text-primary-foreground lg:sticky lg:top-0 lg:h-svh lg:flex-col lg:items-stretch lg:justify-start lg:gap-8 lg:px-5 lg:py-7">
        <Link href="/" className="text-[1.75rem]" aria-label="Mise admin home">
          <Wordmark />
        </Link>

        <ConsoleNav />

        <div className="flex items-center gap-4 lg:mt-auto lg:block lg:border-t lg:border-primary-foreground/25 lg:pt-5">
          <p className="hidden lg:block">
            <span className="block font-semibold">{admin.name}</span>
            <span className="type-caption block text-primary-foreground/75">{humanize(admin.role)}</span>
          </p>
          <form action={logout} className="lg:mt-4">
            <button type="submit" className="type-body-sm underline underline-offset-4 hover:decoration-2">
              Sign out
            </button>
          </form>
        </div>
      </header>

      <main id="main" className="min-w-0 px-5 py-8 lg:px-10 lg:py-10">
        <div className="mx-auto max-w-[76rem]">{children}</div>
      </main>
    </div>
  );
}
