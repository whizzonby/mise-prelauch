"use client";

import { Container, FAQItem, FAQList, Heading, Section } from "@mise/ui";

import { faq } from "@/content/home";

import { useTrack } from "./providers";

export function FAQ() {
  const track = useTrack();
  return (
    <Section id={faq.id} tone="surface" aria-labelledby="faq-title">
      <Container className="grid gap-x-10 gap-y-10 lg:grid-cols-12">
        <Heading as="h2" id="faq-title" className="max-w-[10ch] lg:col-span-4">
          {faq.title}
        </Heading>
        <FAQList className="lg:col-span-7 lg:col-start-6" onOpen={(question) => track("faq_opened", { question })}>
          {faq.items.map((item) => (
            <FAQItem key={item.id} value={item.id} question={item.question}>
              {item.answer}
            </FAQItem>
          ))}
        </FAQList>
      </Container>
    </Section>
  );
}
