import type { Metadata } from "next";
import Link from "next/link";

import { Form, FormMessage, SelectField, SubmitButton, TextField } from "@/components/forms";
import { reference } from "@/lib/data";
import { availabilityText } from "@/lib/format";
import { api, requireCustomer } from "@/lib/session";

import { updateProfile } from "./actions";

export const metadata: Metadata = { title: "Profile" };

export default async function ProfilePage() {
  const { token } = await requireCustomer();
  const [profile, ref] = await Promise.all([api.account.profile(token), reference()]);
  return (
    <>
      <h1 className="type-h2">Profile</h1>
      <p className="type-body mt-3 text-muted">How we address you, and how to reach you about your account.</p>

      <dl className="type-body-sm mt-8 border-y border-border py-4">
        <div className="flex flex-wrap justify-between gap-2">
          <dt className="font-semibold">Email</dt>
          <dd>
            {profile.email}{" "}
            <Link href="/profile/security" className="ml-2 underline underline-offset-4">
              Change
            </Link>
          </dd>
        </div>
        {profile.pending_email ? (
          <p className="type-caption mt-2 text-muted">Waiting for you to confirm {profile.pending_email} from its inbox.</p>
        ) : null}
        {profile.waitlist_linked ? (
          <p className="type-caption mt-2 text-muted">Linked to your place on the Mise waitlist, with your invitations.</p>
        ) : null}
      </dl>

      <Form action={updateProfile} aria-label="Profile" className="mt-8">
        <div className="grid gap-5 sm:grid-cols-2">
          <TextField name="first_name" label="First name" autoComplete="given-name" defaultValue={profile.first_name} required />
          <TextField name="last_name" label="Last name" autoComplete="family-name" defaultValue={profile.last_name} required />
        </div>
        <TextField
          name="phone"
          label="Phone number"
          type="tel"
          autoComplete="tel"
          optional
          defaultValue={profile.phone ?? ""}
          hint="Include the area code, for example +1 868 555 0123."
        />
        <SelectField
          name="contact_channel"
          label="How should we contact you about your account?"
          defaultValue={profile.contact_channel}
          options={[
            { key: "email", label: "Email" },
            { key: "sms", label: "Text message (SMS)" },
            { key: "whatsapp", label: "WhatsApp" },
          ]}
          hint="SMS and WhatsApp need a phone number. Security notices always come by email."
        />
        <SelectField
          name="delivery_region"
          label="Where would you like deliveries?"
          optional
          defaultValue={profile.delivery_region ?? ""}
          placeholder="Not set"
          options={ref.regions}
          hint={availabilityText(profile.delivery_availability)}
        />
        <p className="type-caption text-muted">Mise is available in English. More languages may follow.</p>
        <FormMessage />
        <div>
          <SubmitButton pending="Saving…">Save profile</SubmitButton>
        </div>
      </Form>
    </>
  );
}
