import { buttonClasses, Container, cx, Text } from "@mise/ui";
import Image from "next/image";
import type { CSSProperties } from "react";

import { hero } from "@/content/home";

import { TrackedLink } from "./tracked-link";

/*
 * Where each counter tile sits on the six-column grid, and where it starts
 * before it settles. The offsets are small on purpose: the idea is things
 * being nudged square, not flying in.
 */
const placement = [
  { area: "col-span-4 row-span-2", stretch: true, from: { x: "-18px", y: "14px", r: "-2.2deg" } },
  { area: "col-span-2", stretch: false, from: { x: "16px", y: "-12px", r: "2.6deg" } },
  { area: "col-span-2", stretch: false, from: { x: "20px", y: "10px", r: "-1.8deg" } },
  { area: "col-span-3", stretch: false, from: { x: "-12px", y: "18px", r: "1.9deg" } },
  { area: "col-span-3", stretch: false, from: { x: "14px", y: "16px", r: "-2.4deg" } },
] as const;

export function Hero() {
  return (
    <section className="overflow-hidden pb-[clamp(3.5rem,7vw,7rem)] pt-[clamp(1.5rem,4vw,3.5rem)]">
      <Container className="grid items-center gap-x-10 gap-y-10 lg:grid-cols-12">
        <div className="lg:col-span-7">
          <h1 className="type-display text-primary">
            {/* Each line rises from behind its own baseline. */}
            {hero.headline.map((line, index) => (
              <span key={line} className="headline-line" style={{ "--order": index } as CSSProperties}>
                <span>{line} </span>
              </span>
            ))}
          </h1>

          <div className="hero-quiet">
            <Text size="lg" className="mt-8 max-w-[34ch] md:mt-10">
              {hero.body}
            </Text>

            <div className="mt-8 flex flex-wrap items-center gap-x-7 gap-y-4">
              <TrackedLink
                href="#waitlist"
                event="hero_cta_clicked"
                metadata={{ cta: "primary" }}
                className={buttonClasses("primary", "lg")}
              >
                {hero.primaryCta}
              </TrackedLink>
              <TrackedLink
                href={hero.secondaryCta.href}
                event="hero_cta_clicked"
                metadata={{ cta: "secondary" }}
                className={buttonClasses("quiet", "lg")}
              >
                {hero.secondaryCta.label}
              </TrackedLink>
            </div>

            <Text size="sm" tone="muted" className="mt-6 max-w-[36ch]">
              {hero.note}
            </Text>
          </div>
        </div>

        {/* The counter. A list, because it is one: the ingredients, each with its prep. */}
        <ul aria-label="Ingredients, prepped" className="grid grid-cols-6 gap-x-2 gap-y-3 sm:gap-x-3 lg:col-span-5">
          {hero.counter.map((item, index) => {
            const spot = placement[index % placement.length]!;
            return (
              <li
                key={item.id}
                className={cx("counter-tile", spot.area)}
                style={
                  {
                    "--order": index,
                    "--from-x": spot.from.x,
                    "--from-y": spot.from.y,
                    "--from-rotate": spot.from.r,
                  } as CSSProperties
                }
              >
                <figure className="flex h-full flex-col">
                  <div className={cx("relative overflow-hidden bg-border", spot.stretch ? "min-h-0 flex-1" : "aspect-[3/2]")}>
                    <Image
                      src={item.src}
                      alt={item.alt}
                      fill
                      priority={index < 3}
                      placeholder="blur"
                      sizes={spot.stretch ? "(min-width: 1024px) 33vw, 66vw" : "(min-width: 1024px) 25vw, 50vw"}
                      className="object-cover"
                    />
                  </div>
                  <figcaption className="type-caption mt-1.5 flex flex-wrap gap-x-1.5">
                    <span className="font-semibold">{item.name}</span>
                    <span className="text-muted">{item.prep}</span>
                  </figcaption>
                </figure>
              </li>
            );
          })}
        </ul>
      </Container>
    </section>
  );
}
