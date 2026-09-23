import { Container, Heading, ImageReveal, Section, Text } from "@mise/ui";
import Image from "next/image";

import { sustainability } from "@/content/home";

/** Local sourcing and the reduced-waste idea: where the ingredients come from and where they end up. */
export function IngredientStory() {
  return (
    <Section id={sustainability.id} aria-labelledby="sustainability-title">
      <Container>
        <div className="grid gap-x-10 gap-y-10 lg:grid-cols-12">
          <div className="lg:col-span-5">
            <Heading as="h2" id="sustainability-title">
              {sustainability.title}
            </Heading>
            <Text size="lg" className="mt-7 max-w-[38ch]">
              {sustainability.body}
            </Text>
            <Text size="sm" tone="muted" className="mt-6 max-w-[44ch] border-l-2 border-accent pl-4">
              {sustainability.note}
            </Text>
          </div>

          <div className="grid grid-cols-5 items-end gap-3 lg:col-span-6 lg:col-start-7">
            <ImageReveal className="col-span-3">
              <div className="relative aspect-[4/5] overflow-hidden bg-border">
                <Image
                  src={sustainability.photos.main.src}
                  alt={sustainability.photos.main.alt}
                  fill
                  placeholder="blur"
                  sizes="(min-width: 1024px) 30vw, 56vw"
                  className="object-cover"
                />
              </div>
            </ImageReveal>
            <ImageReveal className="col-span-2" delay={0.15}>
              <div className="relative aspect-[3/4] overflow-hidden bg-border">
                <Image
                  src={sustainability.photos.detail.src}
                  alt={sustainability.photos.detail.alt}
                  fill
                  placeholder="blur"
                  sizes="(min-width: 1024px) 20vw, 38vw"
                  className="object-cover"
                />
              </div>
            </ImageReveal>
          </div>
        </div>

        {/* The chain is a true sequence, so it is numbered and joined by a line. */}
        <ol className="mt-16 grid gap-y-0 md:mt-24 md:grid-cols-5 md:gap-x-6">
          {sustainability.chain.map((link, index) => (
            <li
              key={link.title}
              className="relative border-l border-foreground pb-8 pl-6 last:pb-0 md:border-l-0 md:border-t md:pb-0 md:pl-0 md:pt-6"
            >
              <span
                aria-hidden="true"
                className="absolute -left-[5px] top-0 size-[9px] rounded-full bg-accent md:-top-[5px] md:left-0"
              />
              <p aria-hidden="true" className="type-caption type-figures text-muted">
                {String(index + 1).padStart(2, "0")}
              </p>
              <Heading as="h3" size="h4" className="mt-1">
                {link.title}
              </Heading>
              <Text size="sm" tone="muted" className="mt-1.5 max-w-[26ch]">
                {link.body}
              </Text>
            </li>
          ))}
        </ol>
      </Container>
    </Section>
  );
}
