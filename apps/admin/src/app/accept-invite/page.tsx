import { Heading, Text, Wordmark } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { AcceptInviteForm } from "@/components/accept-invite-form";

export const metadata: Metadata = { title: "Accept invitation", robots: { index: false } };

/** Where an invitation email lands. The token is checked when the form is sent. */
export default async function AcceptInvitePage({ searchParams }: { searchParams: Promise<{ token?: string }> }) {
  const { token = "" } = await searchParams;
  return (
    <main className="grid min-h-svh place-items-center px-5 py-12">
      <div className="w-full max-w-[26rem]">
        <p className="text-[2.5rem] text-primary">
          <Wordmark />
        </p>
        <div className="mt-8 border-t-4 border-primary bg-surface p-7 sm:p-9">
          <Heading as="h1" size="h3">
            Join the Mise team
          </Heading>
          {token ? (
            <>
              <Text size="sm" className="mt-3">
                Set a password for your staff account. You may be asked to set up two-step sign-in when you first sign in.
              </Text>
              <AcceptInviteForm token={token} />
            </>
          ) : (
            <Text size="sm" className="mt-3">
              This link is incomplete. Open the link from your invitation email again, or ask for a new invitation.
            </Text>
          )}
        </div>
        <Text size="caption" tone="muted" className="mt-5">
          Already set up? <Link href="/login" className="underline underline-offset-4">Sign in</Link>
        </Text>
      </div>
    </main>
  );
}
