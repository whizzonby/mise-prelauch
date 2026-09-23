import { Container, Heading, Section, Text } from "@mise/ui";
import Image from "next/image";

import { story } from "@/content/home";

import { StoryMotion } from "./story-motion";

/**
 * Farm → Mise kitchen → meal kit → your kitchen → dinner.
 *
 * Rendered on the server as an ordinary ordered list, which is what phones,
 * reduced-motion users and search engines get. StoryMotion upgrades it to a
 * pinned, scroll-scrubbed sequence on wide screens.
 */
export function SupplyStory() {
  return (
    <Section id={story.id} tone="primary" spacing="flush" data-story aria-labelledby="story-title">
      <StoryMotion />
      <Container data-story-stage className="py-[clamp(4.5rem,9vw,8rem)]">
        <div>
          <Heading as="h2" id="story-title" className="max-w-[14ch]">
            {story.title}
          </Heading>

          <ol data-story-list className="mt-12 grid gap-14 md:mt-16">
            {story.stages.map((stage, index) => (
              <li
                key={stage.id}
                data-story-item
                className="grid gap-6 border-t border-primary-foreground/25 pt-6 md:grid-cols-12 md:gap-8"
              >
                <div data-story-text className="md:col-span-5">
                  <p aria-hidden="true" className="type-figures font-display text-[2.5rem] leading-none text-primary-foreground/60">
                    {String(index + 1).padStart(2, "0")}
                  </p>
                  <Heading as="h3" size="h1" className="mt-3">
                    {stage.name}
                  </Heading>
                  <Text size="lg" tone="muted" className="mt-5 max-w-[30ch]">
                    {stage.body}
                  </Text>
                </div>
                <figure data-story-media className="md:col-span-6 md:col-start-7">
                  <div className="relative aspect-[4/3] overflow-hidden rounded-lg bg-primary-foreground/10">
                    <Image
                      src={stage.src}
                      alt={stage.alt}
                      fill
                      placeholder="blur"
                      sizes="(min-width: 768px) 50vw, 100vw"
                      className="object-cover"
                    />
                  </div>
                </figure>
              </li>
            ))}
          </ol>

          {/* Position in the sequence. Decorative: the list above already carries the order. */}
          <ol data-story-progress aria-hidden="true" className="mt-10 hidden items-center gap-3">
            {story.stages.map((stage) => (
              <li key={stage.id} data-story-marker className="type-caption flex items-center gap-3 opacity-45 transition-opacity duration-(--duration-standard)">
                <span className="block h-px w-8 bg-current" />
                {stage.name}
              </li>
            ))}
          </ol>
        </div>
      </Container>
    </Section>
  );
}
