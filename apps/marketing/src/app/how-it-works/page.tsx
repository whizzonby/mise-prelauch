import { Container, Heading, Section, Text } from "@mise/ui";
import type { Metadata } from "next";

import { HowItWorks } from "@/components/how-it-works";
import { SiteFooter } from "@/components/site-footer";
import { SiteHeader } from "@/components/site-header";
import { SupplyStory } from "@/components/supply-story";
import { primaryCta } from "@/content/site";
import { subscriptionInfo } from "@/lib/storefront";

export const metadata: Metadata = {
  title: "How it works",
  description: "How a Mise meal kit gets from the menu to your plate: choose, we prep, we deliver, you cook.",
  alternates: { canonical: "/how-it-works" },
};

export const revalidate = 300;

/** The steps, and the weekly rules exactly as the system applies them. */
export default async function HowItWorksPage() {
  const info = await subscriptionInfo();
  return (
    <>
      <SiteHeader />
      <main id="main">
        <Section aria-labelledby="page-title" className="pb-0">
          <Container>
            <h1 id="page-title" className="type-display max-w-[14ch] text-primary">
              How Mise works
            </h1>
          </Container>
        </Section>
        <HowItWorks />
        {info ? (
          <Section aria-labelledby="rules-title" tone="surface">
            <Container className="grid gap-x-10 gap-y-6 lg:grid-cols-12">
              <Heading as="h2" id="rules-title" className="lg:col-span-4">
                Your week, in practice
              </Heading>
              <ul className="type-body grid gap-4 lg:col-span-7 lg:col-start-6">
                <li className="border-b border-border pb-4">{info.cutoff_rule}</li>
                <li className="border-b border-border pb-4">Each week is paid before its menu closes. A week that is not paid is not delivered, and nothing is owed for it.</li>
                <li className="border-b border-border pb-4">
                  Pause for up to {info.max_pause_weeks} weeks, change plan or cancel from your account. Weeks already paid are still delivered.
                </li>
              </ul>
            </Container>
          </Section>
        ) : null}
        <SupplyStory />
        <Section>
          <Container>
            <Text size="lg" className="max-w-[40ch]">
              Ready to look at the menu?
            </Text>
            <a href={primaryCta.href} className="type-body mt-4 inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              {primaryCta.label}
            </a>
          </Container>
        </Section>
      </main>
      <SiteFooter />
    </>
  );
}
