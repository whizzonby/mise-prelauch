import { buttonClasses, Container, Heading, Section, Text } from "@mise/ui";
import Link from "next/link";

import { plans, plansSection } from "@/content/plans";

import { PlanRow } from "./plan-row";

/** Plan previews, set as a price list rather than a row of cards. Nothing here can be bought yet. */
export function Plans() {
  return (
    <Section id={plansSection.id} aria-labelledby="plans-title">
      <Container className="grid gap-x-10 gap-y-10 lg:grid-cols-12">
        <div className="lg:col-span-4">
          <Heading as="h2" id="plans-title" className="max-w-[12ch]">
            {plansSection.title}
          </Heading>
          <Text className="mt-6 max-w-[34ch]" tone="muted">
            {plansSection.body}
          </Text>
          <Link href={plansSection.cta.href} className={buttonClasses("primary", "lg", "mt-8")}>
            {plansSection.cta.label}
          </Link>
        </div>

        <ul className="border-t border-foreground lg:col-span-7 lg:col-start-6">
          {plans.map((plan) => (
            <PlanRow key={plan.id} plan={plan} pricePending={plansSection.pricePending} />
          ))}
        </ul>
      </Container>
    </Section>
  );
}
