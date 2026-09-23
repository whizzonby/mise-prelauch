"use client";

import {
  captureAttribution,
  createTracker,
  type EventMetadata,
  type EventType,
  type Tracker,
} from "@mise/analytics";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { usePathname } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";

import { api } from "@/lib/api";
import { readProfileToken } from "@/lib/lead-session";

const TrackerContext = createContext<Tracker | null>(null);

/** Records a first-party analytics event. Safe to call anywhere; a no-op when tracking is declined. */
export function useTrack() {
  const tracker = useContext(TrackerContext);
  return useCallback(
    <T extends EventType>(type: T, metadata: EventMetadata[T]) => tracker?.track(type, metadata),
    [tracker],
  );
}

/** The visitor's anonymous id, so a signup can be joined to the events before it. */
export function useAnonymousId() {
  const tracker = useContext(TrackerContext);
  return useCallback(() => tracker?.anonymousId() || undefined, [tracker]);
}

function PageViews() {
  const pathname = usePathname();
  const track = useTrack();
  useEffect(() => {
    // The path only: query strings can hold tokens.
    track("page_view", { path: pathname });
  }, [pathname, track]);
  return null;
}

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } } }),
  );
  const [tracker, setTracker] = useState<Tracker | null>(null);
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    captureAttribution();
    setTracker(createTracker({ send: (batch) => api.sendEvents(batch, readProfileToken()) }));
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      <TrackerContext.Provider value={tracker}>
        {tracker ? <PageViews /> : null}
        {children}
      </TrackerContext.Provider>
    </QueryClientProvider>
  );
}

/**
 * Reports `section_viewed` once, the first time half of the wrapped section
 * is on screen.
 */
export function SectionView({ section, children }: { section: string; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const track = useTrack();

  useEffect(() => {
    const node = ref.current;
    if (!node || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          track("section_viewed", { section });
          observer.disconnect();
        }
      },
      { threshold: 0.3 },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [section, track]);

  return <div ref={ref}>{children}</div>;
}
