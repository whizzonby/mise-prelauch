"use client";

import { useEffect, useRef } from "react";

const QUERY = "(min-width: 1024px) and (min-height: 640px) and (prefers-reduced-motion: no-preference)";

/**
 * Upgrades the supply-chain story to a pinned sequence: the section holds
 * still while each photograph is uncovered over the last and its text takes
 * the previous text's place.
 *
 * GSAP is used here, and only here, because the sequence needs a scrubbed
 * timeline tied to a pinned element. It is fetched when the section nears
 * the viewport, so phones and reduced-motion visitors never download it.
 */
export function StoryMotion() {
  const anchor = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    const section = anchor.current?.closest<HTMLElement>("[data-story]");
    if (!section || !window.matchMedia(QUERY).matches) return;

    let cancelled = false;
    let cleanup: (() => void) | undefined;

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) return;
        observer.disconnect();
        void Promise.all([import("gsap"), import("gsap/ScrollTrigger")]).then(([{ gsap }, { ScrollTrigger }]) => {
          if (cancelled) return;
          gsap.registerPlugin(ScrollTrigger);
          const media = gsap.matchMedia();

          media.add(QUERY, () => {
            section.dataset.enhanced = "true";
            const stage = section.querySelector<HTMLElement>("[data-story-stage]")!;
            const texts = gsap.utils.toArray<HTMLElement>("[data-story-text]", section);
            const frames = gsap.utils.toArray<HTMLElement>("[data-story-media]", section);
            const markers = gsap.utils.toArray<HTMLElement>("[data-story-marker]", section);
            const steps = texts.length - 1;

            const setActive = (index: number) =>
              markers.forEach((marker, i) => marker.style.setProperty("opacity", i === index ? "1" : ""));
            setActive(0);

            gsap.set(texts.slice(1), { autoAlpha: 0, y: 28 });
            gsap.set(frames.slice(1), { clipPath: "inset(100% 0% 0% 0%)" });

            const timeline = gsap.timeline({
              defaults: { ease: "power2.inOut" },
              scrollTrigger: {
                trigger: stage,
                start: "top top",
                end: () => `+=${window.innerHeight * 0.9 * steps}`,
                pin: true,
                scrub: 0.6,
                invalidateOnRefresh: true,
                onUpdate: (self) => setActive(Math.round(self.progress * steps)),
              },
            });

            for (let i = 1; i <= steps; i++) {
              const at = i - 1;
              timeline
                .to(texts[i - 1]!, { autoAlpha: 0, y: -28, duration: 0.35 }, at + 0.1)
                .to(frames[i]!, { clipPath: "inset(0% 0% 0% 0%)", duration: 0.7 }, at + 0.1)
                .to(texts[i]!, { autoAlpha: 1, y: 0, duration: 0.4 }, at + 0.45);
            }
            // A beat of stillness on the last stage before the page moves on.
            timeline.to({}, { duration: 0.25 });

            return () => {
              delete section.dataset.enhanced;
              setActive(-1);
            };
          });

          cleanup = () => media.revert();
        });
      },
      { rootMargin: "100% 0px" },
    );
    observer.observe(section);

    return () => {
      cancelled = true;
      observer.disconnect();
      cleanup?.();
    };
  }, []);

  return <span ref={anchor} hidden />;
}
