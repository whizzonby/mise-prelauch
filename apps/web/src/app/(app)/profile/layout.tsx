import { Container } from "@mise/ui";
import type { ReactNode } from "react";

import { signOut } from "@/app/actions";
import { ProfileNav } from "@/components/profile-nav";

/** The account area: its own navigation beside (or above) each settings page. */
export default function ProfileLayout({ children }: { children: ReactNode }) {
  return (
    <Container className="py-10 md:py-14">
      <p className="type-h3 font-display">Your account</p>
      <div className="mt-6 grid gap-8 lg:mt-10 lg:grid-cols-[14rem_1fr] lg:gap-16">
        <div>
          <ProfileNav />
          <form action={signOut} className="mt-6 hidden border-t border-border pt-4 lg:block">
            <button type="submit" className="type-body-sm min-h-11 font-semibold text-muted underline underline-offset-4 hover:text-foreground">
              Sign out
            </button>
          </form>
        </div>
        <div className="min-w-0 max-w-[44rem]">
          {children}
          <form action={signOut} className="mt-14 border-t border-border pt-4 lg:hidden">
            <button type="submit" className="type-body-sm min-h-11 font-semibold underline underline-offset-4">
              Sign out
            </button>
          </form>
        </div>
      </div>
    </Container>
  );
}
