import { buttonClasses, Container, Heading, Section, Text } from "@mise/ui";

import { finalCta } from "@/content/home";
import { env } from "@/lib/env";

/** The closing call to action once ordering is open: into the catalogue, or straight to plans. */
export function FinalCta() {
  return (
    <Section aria-labelledby="final-cta-title">
      <Container className="border-t-4 border-primary bg-surface p-8 sm:p-12">
        <Heading as="h2" id="final-cta-title" size="h1" className="max-w-[18ch] text-primary">
          {finalCta.title}
        </Heading>
        <Text size="lg" className="mt-5 max-w-[44ch]">
          {finalCta.body}
        </Text>
        <div className="mt-8 flex flex-wrap gap-4">
          <a href={`${env.appUrl}/meals`} className={buttonClasses("primary", "lg")}>
            {finalCta.primary}
          </a>
          <a href={`${env.appUrl}/plans`} className={buttonClasses("secondary", "lg")}>
            {finalCta.secondary}
          </a>
        </div>
      </Container>
    </Section>
  );
}
