import { buttonClasses, Container, Heading, Section, Text } from "@mise/ui";

import { finalCta } from "@/content/home";
import { env } from "@/lib/env";

/** The closing call to action once ordering is open: into the catalogue, or straight to plans. */
export function FinalCta() {
  return (
    <Section aria-labelledby="final-cta-title" tone="surface">
      <Container>
        <div className="grid items-end gap-8 border-t border-border pt-[clamp(2.5rem,5vw,4rem)] lg:grid-cols-[minmax(0,1fr)_auto]">
          <div>
            <Heading as="h2" id="final-cta-title" size="h1" className="max-w-[18ch] text-primary">
              {finalCta.title}
            </Heading>
            <Text size="lg" className="mt-5 max-w-[44ch]">
              {finalCta.body}
            </Text>
          </div>
          <div className="flex flex-wrap gap-4">
            <a href={`${env.appUrl}/meals`} className={buttonClasses("primary", "lg")}>
              {finalCta.primary}
            </a>
            <a href={`${env.appUrl}/plans`} className={buttonClasses("secondary", "lg")}>
              {finalCta.secondary}
            </a>
          </div>
        </div>
      </Container>
    </Section>
  );
}
