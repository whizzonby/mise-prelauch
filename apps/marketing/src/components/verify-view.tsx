"use client";

import { ApiError } from "@mise/api-client";
import { buttonClasses, Heading, Text } from "@mise/ui";
import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useRef } from "react";

import { api } from "@/lib/api";
import { saveProfileToken } from "@/lib/lead-session";

/** Confirms an email address from the link in the verification email. */
export function VerifyView() {
  const sent = useRef(false);
  const mutation = useMutation({
    mutationFn: (token: string) => api.verifyLead(token),
    onSuccess: (result) => saveProfileToken(result.profile_token),
  });
  const { mutate } = mutation;

  useEffect(() => {
    // Strict Mode runs effects twice in development; verify once.
    if (sent.current) return;
    sent.current = true;
    const url = new URL(window.location.href);
    const token = url.searchParams.get("token") ?? "";
    window.history.replaceState(null, "", url.pathname);
    mutate(token);
  }, [mutate]);

  if (mutation.isSuccess) {
    return (
      <div className="max-w-[42rem]">
        <Heading as="h1" className="text-primary">
          You&apos;re on the Mise list, {mutation.data.lead.first_name}
        </Heading>
        <Text size="lg" className="mt-6 max-w-[36ch]">
          Your email is confirmed and your place is saved. We will write when Mise opens in your area.
        </Text>
        <Link href="/welcome" className={buttonClasses("primary", "lg", "mt-8")}>
          Open my Mise page
        </Link>
      </div>
    );
  }

  if (mutation.isError) {
    const error = mutation.error;
    const expired = error instanceof ApiError && error.code === "token_expired";
    const known = error instanceof ApiError && error.status < 500;
    return (
      <div className="max-w-[42rem]">
        <Heading as="h1" size="h2">
          {expired ? "This link has expired" : known ? "This link did not work" : "We could not confirm your email"}
        </Heading>
        <Text size="lg" className="mt-6 max-w-[40ch]">
          {expired
            ? "Confirmation links last 7 days. Join the list again with the same address and we will send a new one."
            : known
              ? "Use the most recent email we sent you. If it still fails, join the list again and we will send a new link."
              : "Check your connection and open the link from your email again."}
        </Text>
        <Link href="/#waitlist" className={buttonClasses("primary", "lg", "mt-8")}>
          Join the waitlist
        </Link>
      </div>
    );
  }

  return (
    <p role="status" className="type-body text-muted">
      Confirming your email…
    </p>
  );
}
