"use client";

import { Heading, Text } from "@mise/ui";
import { useEffect, useRef } from "react";

import type { Plan } from "@/content/plans";

import { useTrack } from "./providers";

export function PlanRow({ plan, pricePending }: { plan: Plan; pricePending: string }) {
  const ref = useRef<HTMLLIElement>(null);
  const track = useTrack();

  useEffect(() => {
    const node = ref.current;
    if (!node || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          track("plan_viewed", { plan: plan.id });
          observer.disconnect();
        }
      },
      { threshold: 0.8 },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [plan.id, track]);

  return (
    <li ref={ref} className="grid gap-x-8 gap-y-2 border-b border-border py-6 sm:grid-cols-[minmax(0,13rem)_1fr]">
      <Heading as="h3">{plan.name}</Heading>
      <div>
        <Text>{plan.summary}</Text>
        <Text size="sm" tone="muted" className="mt-1.5">
          For: {plan.audience.charAt(0).toLowerCase() + plan.audience.slice(1)}
        </Text>
        <Text size="sm" className="type-figures mt-3 font-semibold">
          {plan.price ?? pricePending}
        </Text>
      </div>
    </li>
  );
}
