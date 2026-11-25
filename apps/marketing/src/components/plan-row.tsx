"use client";

import { Heading, Text } from "@mise/ui";
import { useEffect, useRef } from "react";

import { useTrack } from "./providers";

interface Props {
  id: string;
  name: string;
  summary: string;
  detail: string;
  price: string;
  href?: string;
}

export function PlanRow({ id, name, summary, detail, price, href }: Props) {
  const ref = useRef<HTMLLIElement>(null);
  const track = useTrack();

  useEffect(() => {
    const node = ref.current;
    if (!node || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          track("plan_viewed", { plan: id });
          observer.disconnect();
        }
      },
      { threshold: 0.8 },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [id, track]);

  return (
    <li ref={ref} className="relative grid gap-x-8 gap-y-2 border-b border-border py-6 sm:grid-cols-[minmax(0,13rem)_1fr]">
      <Heading as="h3">
        {href ? (
          <a href={href} className="after:absolute after:inset-0 after:content-[''] hover:underline hover:underline-offset-4">
            {name}
          </a>
        ) : (
          name
        )}
      </Heading>
      <div>
        <Text>{summary}</Text>
        <Text size="sm" tone="muted" className="mt-1.5">
          {detail}
        </Text>
        <Text size="sm" className="type-figures mt-3 font-semibold">
          {price}
        </Text>
      </div>
    </li>
  );
}
