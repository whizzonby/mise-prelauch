"use client";

import { ApiError } from "@mise/api-client";
import { Heading, MiseButton, Text } from "@mise/ui";
import { useMutation } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { useClientValue } from "@/lib/use-client-value";

/**
 * Unsubscribes on a button press rather than on page load, so that mail
 * scanners that open links automatically cannot unsubscribe someone by
 * accident.
 */
export function UnsubscribeView() {
  const token = useClientValue(() => new URLSearchParams(window.location.search).get("token") ?? "");
  const mutation = useMutation({ mutationFn: (value: string) => api.unsubscribe(value) });

  if (mutation.isSuccess) {
    return (
      <div className="max-w-[42rem]">
        <Heading as="h1" size="h2">
          You are unsubscribed
        </Heading>
        <Text size="lg" className="mt-6 max-w-[40ch]">
          We will not email you again. If you would also like your details deleted, write to us and we will remove them.
        </Text>
      </div>
    );
  }

  const invalid = token === "" || (mutation.error instanceof ApiError && mutation.error.status < 500);

  return (
    <div className="max-w-[42rem]">
      <Heading as="h1" size="h2">
        Stop emails from Mise
      </Heading>
      {invalid ? (
        <Text size="lg" className="mt-6 max-w-[40ch]">
          This unsubscribe link is not valid. Use the link at the bottom of the most recent email from Mise.
        </Text>
      ) : (
        <>
          <Text size="lg" className="mt-6 max-w-[40ch]">
            Press the button and we will stop emailing this address about the Mise launch.
          </Text>
          <MiseButton size="lg" className="mt-8" disabled={!token || mutation.isPending} onClick={() => token && mutation.mutate(token)}>
            {mutation.isPending ? "Unsubscribing…" : "Unsubscribe me"}
          </MiseButton>
          <p role="alert" className={mutation.isError ? "type-body-sm mt-4 font-semibold text-error" : "sr-only"}>
            {mutation.isError ? "We could not reach Mise. Check your connection and try again." : ""}
          </p>
        </>
      )}
    </div>
  );
}
