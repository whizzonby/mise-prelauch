import { Tag } from "@mise/ui";
import type { Metadata } from "next";

import { CheckboxField, Form, FormMessage, SubmitButton } from "@/components/forms";
import { shortDateTime } from "@/lib/greeting";
import { api, requireCustomer } from "@/lib/session";

import { saveNotifications } from "../actions";

export const metadata: Metadata = { title: "Email preferences" };

const descriptions: Record<string, string> = {
  meal_updates: "When new meals are added or a recipe you saved changes.",
  weekly_menu: "The menu for the coming week, once weekly ordering opens.",
  promotions: "Occasional offers from Mise.",
  referral_updates: "When someone you invited joins Mise.",
};

/**
 * Optional email, kept apart from account and security email, which is not
 * optional. Each change is recorded as a consent decision.
 */
export default async function NotificationsPage() {
  const { token } = await requireCustomer();
  const prefs = await api.account.notifications(token);
  return (
    <>
      <h1 className="type-h2">Email preferences</h1>
      <p className="type-body mt-3 text-muted">Choose what Mise may email you about. You can change this at any time.</p>

      <section aria-labelledby="security-mail" className="mt-8 border-y border-border py-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 id="security-mail" className="type-h4">
            Account and security
          </h2>
          <Tag>Always on</Tag>
        </div>
        <p className="type-body-sm mt-2 text-muted">
          Email confirmations, password and email-change notices, and other notices about your account. These keep your account safe, so
          they cannot be turned off. They are never marketing.
        </p>
      </section>

      <Form action={saveNotifications} aria-label="Email preferences" className="mt-8">
        <fieldset className="grid gap-6">
          <legend className="type-h4 mb-2">Optional email</legend>
          {prefs.topics.map((t) => (
            <div key={t.topic}>
              <input type="hidden" name="topic_keys" value={t.topic} />
              <CheckboxField name={`topic_${t.topic}`} label={<span className="font-semibold">{t.label}</span>} defaultChecked={t.enabled} />
              <p className="type-caption ml-9 mt-1 text-muted">
                {descriptions[t.topic]}
                {t.source === "waitlist_consent" ? " Turned on because you agreed to launch emails on the waitlist." : ""}
                {t.updated_at ? ` Changed ${shortDateTime(t.updated_at)}.` : ""}
              </p>
            </div>
          ))}
        </fieldset>
        <FormMessage />
        <div>
          <SubmitButton pending="Saving…">Save email preferences</SubmitButton>
        </div>
      </Form>

      {prefs.upcoming.length ? (
        <section aria-labelledby="upcoming" className="mt-12 border-t border-border pt-6">
          <h2 id="upcoming" className="type-h4">
            Coming later
          </h2>
          <p className="type-body-sm mt-2 text-muted">
            {prefs.upcoming.map((u) => u.label).join(" and ")} will be available once ordering and delivery open. You will
            be asked then.
          </p>
        </section>
      ) : null}
    </>
  );
}
