import type { Metadata } from "next";

import { Form, FormMessage, SubmitButton, TextField } from "@/components/forms";
import { shortDateTime } from "@/lib/greeting";
import { api, requireCustomer } from "@/lib/session";

import { changePassword, requestEmailChange, revokeSession, signOutEverywhereElse } from "../actions";

export const metadata: Metadata = { title: "Sign-in and security" };

export default async function SecurityPage() {
  const { token } = await requireCustomer();
  const [profile, { items: sessions }] = await Promise.all([api.account.profile(token), api.auth.sessions(token)]);
  return (
    <>
      <h1 className="type-h2">Sign-in and security</h1>

      <section aria-labelledby="email" className="mt-8">
        <h2 id="email" className="type-h3">
          Email address
        </h2>
        <p className="type-body-sm mt-2 text-muted">
          You sign in with <span className="font-semibold text-foreground">{profile.email}</span>. To change it, we send a
          link to the new address and a notice to this one; nothing changes until the link is used.
        </p>
        {profile.pending_email ? (
          <p className="type-body-sm mt-3 border-l-2 border-warning pl-3">Waiting for confirmation from {profile.pending_email}.</p>
        ) : null}
        <Form action={requestEmailChange} aria-label="Change email address" className="mt-6">
          <TextField name="new_email" label="New email address" type="email" autoComplete="email" required />
          <TextField name="current_password" id="email_current_password" label="Your current password" type="password" autoComplete="current-password" required />
          <FormMessage />
          <div>
            <SubmitButton pending="Sending…" variant="secondary">
              Send confirmation link
            </SubmitButton>
          </div>
        </Form>
      </section>

      <section aria-labelledby="password" className="mt-14 border-t border-border pt-8">
        <h2 id="password" className="type-h3">
          Password
        </h2>
        <p className="type-body-sm mt-2 text-muted">Changing it signs you out on every other device.</p>
        <Form action={changePassword} aria-label="Change password" className="mt-6">
          <TextField name="current_password" label="Current password" type="password" autoComplete="current-password" required />
          <TextField
            name="new_password"
            label="New password"
            type="password"
            autoComplete="new-password"
            required
            hint="At least 12 characters. A short sentence works well."
          />
          <FormMessage />
          <div>
            <SubmitButton pending="Changing…" variant="secondary">
              Change password
            </SubmitButton>
          </div>
        </Form>
      </section>

      <section aria-labelledby="sessions" className="mt-14 border-t border-border pt-8">
        <h2 id="sessions" className="type-h3">
          Where you are signed in
        </h2>
        <ul className="mt-5 divide-y divide-border border-y border-border">
          {sessions.map((s) => (
            <li key={s.id} className="flex flex-wrap items-center justify-between gap-3 py-3.5">
              <div>
                <p className="type-body-sm font-semibold">
                  {s.user_agent || "Unknown browser"}
                  {s.current ? <span className="font-normal text-muted"> (this device)</span> : null}
                </p>
                <p className="type-caption text-muted">Last used {shortDateTime(s.last_seen_at)}</p>
              </div>
              {s.current ? null : (
                <form action={revokeSession}>
                  <input type="hidden" name="id" value={s.id} />
                  <button type="submit" className="type-body-sm min-h-11 font-semibold underline underline-offset-4">
                    Sign out
                  </button>
                </form>
              )}
            </li>
          ))}
        </ul>
        {sessions.length > 1 ? (
          <form action={signOutEverywhereElse} className="mt-5">
            <button type="submit" className="type-body-sm min-h-11 font-semibold underline underline-offset-4">
              Sign out everywhere else
            </button>
          </form>
        ) : null}
      </section>
    </>
  );
}
