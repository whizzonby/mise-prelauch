import { Container, Heading, ImageReveal, Section, Text } from "@mise/ui";
import Image from "next/image";

import { waitlistSection } from "@/content/home";

import { WaitlistForm } from "./waitlist-form";

export function WaitlistSection() {
  return (
    <Section id={waitlistSection.id} aria-labelledby="waitlist-title">
      <Container className="grid gap-x-10 gap-y-12 lg:grid-cols-12">
        <div className="lg:col-span-5">
          <Heading as="h2" id="waitlist-title" size="h1" className="text-primary">
            {waitlistSection.title}
          </Heading>
          <Text size="lg" className="mt-6 max-w-[32ch]">
            {waitlistSection.body}
          </Text>
          <ul className="mt-8 max-w-[26rem] border-t border-foreground">
            {waitlistSection.assurances.map((line) => (
              <li key={line} className="type-body-sm border-b border-border py-3">
                {line}
              </li>
            ))}
          </ul>
          <ImageReveal className="mt-10 hidden lg:block">
            <div className="relative aspect-[3/2] overflow-hidden rounded-lg bg-border">
              <Image
                src={waitlistSection.photo.src}
                alt={waitlistSection.photo.alt}
                fill
                placeholder="blur"
                sizes="(min-width: 1024px) 38vw, 0px"
                className="object-cover"
              />
            </div>
          </ImageReveal>
        </div>

        {/* The form sits on its own sheet, like an order slip on the counter. */}
        <div className="border-t-4 border-primary bg-surface p-6 sm:p-10 lg:col-span-6 lg:col-start-7">
          <WaitlistForm placement="home" />
        </div>
      </Container>
    </Section>
  );
}
