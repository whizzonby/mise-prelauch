import type { Plan } from "@mise/api-client";
import { buttonClasses, Container, Heading, Section, Text } from "@mise/ui";
import Link from "next/link";

import { plansSection } from "@/content/home";
import { env } from "@/lib/env";
import { formatMoney } from "@/lib/storefront";

import { PlanRow } from "./plan-row";

/**
 * The configured plans, set as a price list rather than a row of cards. Names,
 * meal counts and weekly prices come from the API; a plan without a current
 * price says so instead of showing a guess.
 */
export function Plans({ plans }: { plans: Plan[] | null }) {
  const launch = env.siteMode === "launch";
  const cta = launch ? { label: plansSection.ctaLaunch, href: `${env.appUrl}/plans` } : { label: plansSection.ctaWaitlist, href: "#waitlist" };
  return (
    <Section id={plansSection.id} aria-labelledby="plans-title">
      <Container className="grid gap-x-10 gap-y-10 lg:grid-cols-12">
        <div className="lg:col-span-4">
          <Heading as="h2" id="plans-title" className="max-w-[12ch]">
            {plansSection.title}
          </Heading>
          <Text className="mt-6 max-w-[34ch]" tone="muted">
            {launch ? plansSection.bodyLaunch : plansSection.bodyWaitlist}
          </Text>
          <Link href={cta.href} className={buttonClasses("primary", "lg", "mt-8")}>
            {cta.label}
          </Link>
        </div>

        {plans?.length ? (
          <ul className="border-t border-foreground lg:col-span-7 lg:col-start-6">
            {plans.map((plan) => (
              <PlanRow
                key={plan.id}
                id={plan.slug}
                name={plan.name}
                summary={plan.summary}
                detail={`${plan.meals_per_week} meal kits a week${plan.is_demo ? " · demo plan" : ""}`}
                price={plan.price ? `${formatMoney(plan.price.amount_minor, plan.price.currency)} a week` : plansSection.pricePending}
                href={launch ? `${env.appUrl}/plans/${plan.slug}` : undefined}
              />
            ))}
          </ul>
        ) : (
          <Text size="lg" className="lg:col-span-7 lg:col-start-6">
            {plans === null ? plansSection.unavailable : plansSection.empty}
          </Text>
        )}
      </Container>
    </Section>
  );
}
