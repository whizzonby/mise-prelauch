"use client";

import type { LeadSummary } from "@mise/api-client";
import { buttonClasses, Heading, MiseButton, Text } from "@mise/ui";
import { useEffect, useState } from "react";

import { welcome } from "@/content/waitlist";
import { useClientValue } from "@/lib/use-client-value";

import { useTrack } from "./providers";

/** A lead's invitation link, ways to share it, and how many friends it has brought in. */
export function ReferralPanel({ lead }: { lead: LeadSummary }) {
  const track = useTrack();
  const [copied, setCopied] = useState(false);
  const canShare = useClientValue(() => typeof navigator.share === "function");
  const copy = welcome.referral;
  const message = `${copy.shareText} ${lead.referral_url}`;

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2500);
    return () => clearTimeout(timer);
  }, [copied]);

  async function copyLink() {
    try {
      await navigator.clipboard.writeText(lead.referral_url);
      setCopied(true);
      track("referral_link_copied", {});
    } catch {
      // Clipboard access was refused; the link is on screen to select by hand.
    }
  }

  async function nativeShare() {
    try {
      await navigator.share({ title: "Mise", text: copy.shareText, url: lead.referral_url });
      track("referral_shared", { channel: "native" });
    } catch {
      // The share sheet was dismissed.
    }
  }

  const { converted, pending } = lead.referrals;

  return (
    <section aria-labelledby="referral-title" className="border-t-4 border-primary bg-surface p-6 sm:p-10">
      <Heading as="h2" id="referral-title" size="h3">
        {copy.title}
      </Heading>
      <Text className="mt-3 max-w-[46ch]" tone="muted">
        {copy.body}
      </Text>

      <div className="mt-6 flex flex-col gap-3 sm:flex-row">
        <label htmlFor="referral-link" className="sr-only">
          Your invitation link
        </label>
        <input
          id="referral-link"
          readOnly
          value={lead.referral_url}
          onFocus={(event) => event.currentTarget.select()}
          className="type-figures min-h-12 w-full min-w-0 flex-1 rounded-md border border-border-strong bg-background px-3.5"
        />
        <MiseButton onClick={copyLink} className="shrink-0 sm:min-w-36">
          {copied ? copy.copied : copy.copy}
        </MiseButton>
      </div>
      <p role="status" className="sr-only">
        {copied ? copy.copied : ""}
      </p>

      <ul className="mt-4 flex flex-wrap gap-2">
        <li>
          <a
            href={`https://wa.me/?text=${encodeURIComponent(message)}`}
            target="_blank"
            rel="noopener noreferrer"
            onClick={() => track("referral_shared", { channel: "whatsapp" })}
            className={buttonClasses("secondary", "md")}
          >
            Share on WhatsApp
          </a>
        </li>
        <li>
          <a
            href={`mailto:?subject=${encodeURIComponent("Join me on the Mise waitlist")}&body=${encodeURIComponent(message)}`}
            onClick={() => track("referral_shared", { channel: "email" })}
            className={buttonClasses("secondary", "md")}
          >
            Share by email
          </a>
        </li>
        {canShare ? (
          <li>
            <MiseButton variant="secondary" onClick={nativeShare}>
              More ways to share
            </MiseButton>
          </li>
        ) : null}
      </ul>

      <dl className="mt-8 grid grid-cols-2 gap-6 border-t border-border pt-6">
        <div>
          <dt className="type-body-sm text-muted">Friends who have joined</dt>
          <dd className="type-figures font-display text-[3.5rem] leading-none text-primary">{converted}</dd>
        </div>
        <div>
          <dt className="type-body-sm text-muted">Waiting to confirm their email</dt>
          <dd className="type-figures font-display text-[3.5rem] leading-none text-primary">{pending}</dd>
        </div>
      </dl>

      <Text size="caption" tone="muted" className="mt-6 max-w-[52ch]">
        {copy.perks}
      </Text>
    </section>
  );
}
