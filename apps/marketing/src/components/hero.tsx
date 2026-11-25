import { buttonClasses, Container, Text } from "@mise/ui";
import Image from "next/image";
import type { CSSProperties } from "react";

import { hero } from "@/content/home";
import { primaryCta } from "@/content/site";
import { env } from "@/lib/env";

import { TrackedLink } from "./tracked-link";

/*
 * The hero: one photograph and the words beside it. About 45 % text and 55 %
 * photograph on a wide screen; on a phone the photograph comes first, short
 * enough that the headline and the main action are still on the first screen.
 * The photograph is the largest element on the page, so it is preloaded,
 * has fixed proportions (no layout shift) and never animates.
 */
export function Hero() {
  const launch = env.siteMode === "launch";
  const secondary = launch
    ? { label: hero.secondaryCtaLaunch.label, href: `${env.appUrl}${hero.secondaryCtaLaunch.path}` }
    : hero.secondaryCtaWaitlist;
  return (
    <section aria-labelledby="hero-title" className="pb-[clamp(3rem,6vw,6rem)] pt-[clamp(0.75rem,3vw,2.5rem)]">
      <Container className="grid items-center gap-x-[clamp(2rem,5vw,5.5rem)] gap-y-8 lg:grid-cols-[minmax(0,45fr)_minmax(0,55fr)]">
        <figure className="relative lg:order-2">
          <div className="relative aspect-[16/9] overflow-hidden rounded-lg bg-border sm:aspect-[3/2] lg:aspect-[11/10] lg:max-h-[78vh] lg:w-full">
            <Image
              src={hero.image.src}
              alt={hero.image.alt}
              fill
              preload
              placeholder="blur"
              sizes="(min-width: 1440px) 760px, (min-width: 1024px) 52vw, 100vw"
              // The plate sits right of centre; keep it in frame on narrow and tall crops.
              className="object-cover object-[58%_50%]"
            />
          </div>
          <figcaption className="type-caption mt-2 text-muted">{hero.image.caption}</figcaption>
        </figure>

        <div className="lg:order-1">
          <p className="type-caption font-semibold uppercase tracking-[0.14em] text-accent">{hero.eyebrow}</p>
          <h1 id="hero-title" className="type-display mt-4 text-[clamp(3rem,0.75rem+4.1vw,5.5rem)] leading-[0.98] text-primary">
            {/* Each line rises from behind its own baseline (still with reduced motion). */}
            {hero.headline.map((line, index) => (
              <span key={line} className="headline-line" style={{ "--order": index } as CSSProperties}>
                <span>{line} </span>
              </span>
            ))}
          </h1>

          {/* On a phone the actions come straight after the headline, so they are on the first screen. */}
          <div className="hero-quiet flex flex-col">
            <Text size="lg" className="order-2 mt-6 max-w-[38ch] md:mt-8 lg:order-1">
              {hero.body}
            </Text>

            <div className="order-1 mt-6 flex flex-wrap items-center gap-x-6 gap-y-4 lg:order-2 lg:mt-8">
              <TrackedLink href={primaryCta.href} event="hero_cta_clicked" metadata={{ cta: "primary" }} className={buttonClasses("primary", "lg")}>
                {primaryCta.label}
              </TrackedLink>
              <TrackedLink href={secondary.href} event="hero_cta_clicked" metadata={{ cta: "secondary" }} className={buttonClasses("quiet", "lg")}>
                {secondary.label}
              </TrackedLink>
            </div>

            <Text size="sm" tone="muted" className="order-3 mt-6 max-w-[40ch]">
              {launch ? hero.noteLaunch : hero.note}
            </Text>
          </div>
        </div>
      </Container>
    </section>
  );
}
