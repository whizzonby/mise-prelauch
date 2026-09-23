import { Container, Heading, ImageReveal, Section, Text } from "@mise/ui";
import Image from "next/image";

import { chefs } from "@/content/home";

/** The rotating guest-chef idea, laid out like a magazine feature opener. */
export function ChefFeature() {
  return (
    <Section id={chefs.id} tone="primary" aria-labelledby="chefs-title">
      <Container className="grid gap-x-10 gap-y-12 lg:grid-cols-12">
        <div className="relative lg:col-span-6">
          <ImageReveal from="left">
            <div className="relative aspect-[4/5] overflow-hidden rounded-lg bg-primary-foreground/10 sm:aspect-[5/4] lg:aspect-[4/5]">
              <Image
                src={chefs.photos.main.src}
                alt={chefs.photos.main.alt}
                fill
                placeholder="blur"
                sizes="(min-width: 1024px) 48vw, 92vw"
                className="object-cover"
              />
            </div>
          </ImageReveal>
          {/* A second, smaller frame overlapping the first, as on a magazine spread. */}
          <div className="absolute -bottom-10 right-4 hidden w-[38%] rounded-xl border-8 border-primary sm:block lg:-right-10">
            <div className="relative aspect-[4/5] overflow-hidden rounded-lg">
              <Image
                src={chefs.photos.detail.src}
                alt={chefs.photos.detail.alt}
                fill
                placeholder="blur"
                sizes="(min-width: 1024px) 18vw, 34vw"
                className="object-cover"
              />
            </div>
          </div>
        </div>

        <div className="lg:col-span-5 lg:col-start-8 lg:self-center">
          <Heading as="h2" id="chefs-title">
            {chefs.title}
          </Heading>
          <Text size="lg" tone="muted" className="mt-7 max-w-[40ch]">
            {chefs.body}
          </Text>

          <dl className="mt-10 border-t border-primary-foreground/25">
            {chefs.points.map((point) => (
              <div key={point.title} className="grid gap-x-6 gap-y-1 border-b border-primary-foreground/25 py-4 sm:grid-cols-[11rem_1fr]">
                <dt className="type-label">{point.title}</dt>
                <dd className="type-body-sm text-primary-foreground/75">{point.body}</dd>
              </div>
            ))}
          </dl>

          <Text size="sm" tone="muted" className="mt-6">
            {chefs.note}
          </Text>
        </div>
      </Container>
    </Section>
  );
}
