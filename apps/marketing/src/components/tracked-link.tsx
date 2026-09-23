"use client";

import type { EventMetadata, EventType } from "@mise/analytics";
import Link from "next/link";
import type { ComponentPropsWithoutRef } from "react";

import { useTrack } from "./providers";

type TrackedLinkProps<T extends EventType> = ComponentPropsWithoutRef<typeof Link> & {
  event: T;
  metadata: EventMetadata[T];
};

/** A link that records an analytics event when it is followed. */
export function TrackedLink<T extends EventType>({ event, metadata, onClick, ...props }: TrackedLinkProps<T>) {
  const track = useTrack();
  return (
    <Link
      {...props}
      onClick={(e) => {
        track(event, metadata);
        onClick?.(e);
      }}
    />
  );
}
