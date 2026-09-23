"use client";

import { ApiError } from "@mise/api-client";
import { buttonClasses, Heading, Text } from "@mise/ui";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect } from "react";

import { welcome } from "@/content/waitlist";
import { api } from "@/lib/api";
import { clearProfileToken, readProfileToken, saveProfileToken } from "@/lib/lead-session";
import { useClientValue } from "@/lib/use-client-value";

import { PreferencesForm } from "./preferences-form";
import { ReferralPanel } from "./referral-panel";

/**
 * The personalised page a lead sees after joining, and again whenever they
 * follow the link in a Mise email. It is identified by the profile token in
 * storage, or by `?token=` from an email link.
 */
export function WelcomeView() {
  // undefined: not looked yet (server render). null: looked, none found.
  const token = useClientValue(() => new URLSearchParams(window.location.search).get("token") ?? readProfileToken() ?? null);

  useEffect(() => {
    const url = new URL(window.location.href);
    const fromLink = url.searchParams.get("token");
    if (!fromLink) return;
    saveProfileToken(fromLink);
    // Take the token out of the address bar and history.
    url.searchParams.delete("token");
    window.history.replaceState(null, "", url.pathname + url.search);
  }, []);

  const query = useQuery({
    queryKey: ["lead", "me", token],
    queryFn: () => api.getMe(token!),
    enabled: Boolean(token),
    retry: (count, error) => !(error instanceof ApiError) && count < 2,
  });

  const sessionInvalid = query.error instanceof ApiError && (query.error.status === 401 || query.error.status === 403);
  useEffect(() => {
    if (sessionInvalid) clearProfileToken();
  }, [sessionInvalid]);

  if (token === undefined || (token && query.isPending)) {
    return (
      <p role="status" className="type-body text-muted">
        Loading your Mise page…
      </p>
    );
  }

  if (token === null || sessionInvalid) {
    return (
      <div className="max-w-[40rem]">
        <Heading as="h1" size="h2">
          {welcome.noSession.title}
        </Heading>
        <Text size="lg" className="mt-6">
          {welcome.noSession.body}
        </Text>
        <Link href="/#waitlist" className={buttonClasses("primary", "lg", "mt-8")}>
          {welcome.noSession.cta}
        </Link>
      </div>
    );
  }

  if (query.isError || !query.data) {
    return (
      <div className="max-w-[40rem]">
        <Heading as="h1" size="h2">
          We could not load your page
        </Heading>
        <Text size="lg" className="mt-6">
          Check your connection, then try again.
        </Text>
        <button type="button" onClick={() => query.refetch()} className={buttonClasses("primary", "lg", "mt-8")}>
          Try again
        </button>
      </div>
    );
  }

  const lead = query.data;
  const copy = lead.verification_required ? welcome.pending : welcome.verified;

  return (
    <div className="grid gap-x-10 gap-y-12 lg:grid-cols-12">
      <div className="lg:col-span-5">
        <Heading as="h1" className="text-primary">
          {copy.title}, {lead.first_name}
        </Heading>
        <Text size="lg" className="mt-6 max-w-[34ch]">
          {copy.body}
        </Text>
        {lead.verification_required ? (
          <Text size="sm" tone="muted" className="mt-4 max-w-[40ch] border-l-2 border-accent pl-4">
            Nothing in your inbox after a few minutes? Check spam, or join again with the same address and we will send a
            fresh link.
          </Text>
        ) : null}
      </div>

      <div className="grid gap-12 lg:col-span-6 lg:col-start-7">
        <ReferralPanel lead={lead} />
        {lead.profile_completed ? null : <PreferencesForm token={token} />}
      </div>
    </div>
  );
}
