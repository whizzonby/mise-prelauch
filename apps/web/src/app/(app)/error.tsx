"use client";

import { Container, Heading, MiseButton, Text } from "@mise/ui";

/** A page failed to load. Nothing here reveals what went wrong on the server. */
export default function AppError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <Container className="py-16 md:py-24">
      <Heading as="h1" size="h2">
        This page did not load.
      </Heading>
      <Text className="mt-4 max-w-[40rem]" tone="muted">
        Something went wrong on our side, or the connection dropped. Your account and anything you saved are safe.
      </Text>
      <div className="mt-8 flex flex-wrap gap-3">
        <MiseButton onClick={reset}>Try again</MiseButton>
        <MiseButton variant="secondary" href="/dashboard">
          Go home
        </MiseButton>
      </div>
    </Container>
  );
}
