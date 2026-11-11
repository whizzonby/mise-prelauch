import { Container } from "@mise/ui";

/** Shown while a page's data loads: the page's shape in quiet blocks, no spinner. */
export default function Loading() {
  return (
    <Container className="py-10 md:py-14" aria-busy="true" aria-live="polite">
      <span className="sr-only">Loading…</span>
      <div className="h-10 w-2/3 max-w-[28rem] rounded-sm bg-border/60 motion-safe:animate-pulse" />
      <div className="mt-4 h-5 w-1/2 max-w-[20rem] rounded-sm bg-border/50 motion-safe:animate-pulse" />
      <div className="mt-10 grid gap-8 sm:grid-cols-2 lg:grid-cols-3">
        {[0, 1, 2].map((i) => (
          <div key={i}>
            <div className="aspect-[4/3] rounded-lg bg-border/50 motion-safe:animate-pulse" />
            <div className="mt-4 h-6 w-3/4 rounded-sm bg-border/50 motion-safe:animate-pulse" />
          </div>
        ))}
      </div>
    </Container>
  );
}
