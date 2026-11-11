import { Heading, Text, Wordmark } from "@mise/ui";
import type { Metadata } from "next";

import { signOut, signOutEverywhereElse } from "@/app/actions";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Your account" };

// Instants are stored in UTC; people in Trinidad and Tobago read them in local time.
const when = new Intl.DateTimeFormat("en-TT", {
  dateStyle: "medium",
  timeStyle: "short",
  timeZone: "America/Port_of_Spain",
});

/**
 * The signed-in home. Sprint 1 stops at the account itself: menus,
 * subscriptions and orders arrive in later sprints, inside this shell.
 */
export default async function AccountHome({
  searchParams,
}: {
  searchParams: Promise<{ signed_out_elsewhere?: string }>;
}) {
  const { token, customer } = await requireCustomer();
  const { items: sessions } = await api.auth.sessions(token);
  const { signed_out_elsewhere } = await searchParams;

  return (
    <div className="min-h-svh">
      <header className="border-b border-border">
        <div className="mx-auto flex max-w-[64rem] items-center justify-between px-5 py-4">
          <span className="text-[1.75rem] text-primary">
            <Wordmark />
          </span>
          <form action={signOut}>
            <button type="submit" className="type-body-sm underline underline-offset-4 hover:decoration-2">
              Sign out
            </button>
          </form>
        </div>
      </header>

      <main id="main" className="mx-auto max-w-[64rem] px-5 py-12">
        <Heading as="h1" size="h2">
          Welcome, {customer.first_name}.
        </Heading>
        <Text size="lg" className="mt-4 max-w-[40rem]">
          Your Mise account is ready. Weekly menus and ordering open here soon; we will email {customer.email} when
          they do.
        </Text>

        <section aria-labelledby="sessions" className="mt-14 border-t border-foreground pt-6">
          <h2 id="sessions" className="type-h4">
            Where you are signed in
          </h2>
          {signed_out_elsewhere ? (
            <Text size="sm" className="mt-3 border-l-2 border-success pl-3">
              You are now signed out everywhere else.
            </Text>
          ) : null}
          <ul className="mt-4 divide-y divide-border border-y border-border">
            {sessions.map((s) => (
              <li key={s.id} className="flex flex-wrap items-baseline justify-between gap-2 py-3">
                <span className="type-body-sm">
                  {s.user_agent || "Unknown browser"}
                  {s.current ? " (this device)" : ""}
                </span>
                <span className="type-caption text-muted">Last used {when.format(new Date(s.last_seen_at))}</span>
              </li>
            ))}
          </ul>
          {sessions.length > 1 ? (
            <form action={signOutEverywhereElse} className="mt-4">
              <button type="submit" className="type-body-sm underline underline-offset-4 hover:decoration-2">
                Sign out everywhere else
              </button>
            </form>
          ) : null}
        </section>
      </main>
    </div>
  );
}
