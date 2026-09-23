import { ApiError } from "@mise/api-client";
import { Heading, Text } from "@mise/ui";
import type { Metadata } from "next";

import { PageShell } from "@/components/page-shell";
import { RememberReferral } from "@/components/remember-referral";
import { WaitlistForm } from "@/components/waitlist-form";
import { waitlistSection } from "@/content/home";
import { serverApi } from "@/lib/api";

export const metadata: Metadata = {
  title: "You have been invited to Mise",
  description: "A friend has invited you to join the waitlist for Mise, a Caribbean meal kit launching in Trinidad & Tobago.",
  // Every invitation URL is the same page; none should be indexed separately.
  robots: { index: false, follow: true },
  alternates: { canonical: "/" },
};

const CODE = /^[A-Z0-9]{6,12}$/;

/** Returns the referrer's first name, or null when the code is not valid. */
async function lookup(code: string): Promise<string | null> {
  if (!CODE.test(code)) return null;
  try {
    const referral = await serverApi().lookupReferral(code);
    return referral.referrer_first_name;
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    // If the API is unreachable the code is still passed to signup, which
    // checks it again; the page just cannot greet by name.
    return "";
  }
}

export default async function JoinPage({ params }: { params: Promise<{ code: string }> }) {
  const code = (await params).code.toUpperCase();
  const referrer = await lookup(code);
  const valid = referrer !== null;

  return (
    <PageShell>
      {valid ? <RememberReferral code={code} /> : null}
      <div className="grid gap-x-10 gap-y-12 lg:grid-cols-12">
        <div className="lg:col-span-5">
          <Heading as="h1" className="text-primary">
            {referrer ? `${referrer} saved you a place at the table` : "Save your place at the table"}
          </Heading>
          <Text size="lg" className="mt-6 max-w-[34ch]">
            Mise is a Caribbean meal kit launching in Trinidad & Tobago: fresh local ingredients, seasoned, chopped and
            portioned, ready for you to cook. {waitlistSection.body}
          </Text>
          {valid ? null : (
            <Text size="sm" tone="muted" className="mt-6 max-w-[40ch] border-l-2 border-accent pl-4">
              This invitation link is not valid, but you can still join the list below.
            </Text>
          )}
        </div>
        <div className="border-t-4 border-primary bg-surface p-6 sm:p-10 lg:col-span-6 lg:col-start-7">
          <WaitlistForm placement="invitation" referralCode={valid ? code : undefined} />
        </div>
      </div>
    </PageShell>
  );
}
