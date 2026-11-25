import { Heading, Text } from "@mise/ui";
import type { Metadata } from "next";

import { PageShell } from "@/components/page-shell";
import { site } from "@/content/site";
import { env } from "@/lib/env";

export const metadata: Metadata = {
  title: "Contact",
  description: "How to reach Mise.",
  alternates: { canonical: "/contact" },
};

/*
 * Only the contact route that exists today. A phone number, opening hours or
 * a postal address are added when the business confirms them.
 */
export default function ContactPage() {
  return (
    <PageShell width="prose">
      <Heading as="h1">Contact Mise</Heading>
      <Text size="lg" className="mt-6">
        Email us at{" "}
        <a href={`mailto:${site.contactEmail}`} className="font-semibold underline underline-offset-4">
          {site.contactEmail}
        </a>
        .
      </Text>
      {env.siteMode === "launch" ? (
        <Text className="mt-6" tone="muted">
          Writing about an order? Include the order number shown in your account, so we can find it quickly.
        </Text>
      ) : null}
    </PageShell>
  );
}
