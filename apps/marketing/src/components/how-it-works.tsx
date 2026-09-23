import { Container, Heading, ProcessStep, Section } from "@mise/ui";

import { howItWorks } from "@/content/home";

export function HowItWorks() {
  return (
    <Section id={howItWorks.id} aria-labelledby="how-title">
      <Container>
        <Heading as="h2" id="how-title" className="max-w-[16ch]">
          {howItWorks.title}
        </Heading>

        {/* Four columns ruled off like the method on a recipe card. */}
        <ol className="mt-12 grid gap-x-8 gap-y-12 sm:grid-cols-2 md:mt-16 xl:grid-cols-4">
          {howItWorks.steps.map((step, index) => (
            <ProcessStep key={step.title} number={index + 1} title={step.title} className="border-t border-foreground pt-6">
              {step.body}
            </ProcessStep>
          ))}
        </ol>
      </Container>
    </Section>
  );
}
