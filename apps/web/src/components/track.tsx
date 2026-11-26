"use client";

import { useEffect, useRef } from "react";

import { track, type BehaviorEvents, type BehaviorName } from "@/lib/behavior";

/**
 * Records one behaviour event when the page is shown (see lib/behavior.ts).
 * The ref keeps it to once per page view, even when React runs the effect
 * twice (development StrictMode) or the page re-renders.
 */
export function Track<N extends BehaviorName>({ name, properties }: { name: N; properties: BehaviorEvents[N] }) {
  const key = name + JSON.stringify(properties);
  const sent = useRef<string | null>(null);
  useEffect(() => {
    if (sent.current === key) return;
    sent.current = key;
    track(name, JSON.parse(key.slice(name.length)) as BehaviorEvents[N]);
  }, [name, key]);
  return null;
}
