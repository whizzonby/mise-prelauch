"use client";

import { Field, Input, MiseButton } from "@mise/ui";
import { useActionState } from "react";

import { acceptInvitation, type FormState } from "@/app/actions";

export function AcceptInviteForm({ token }: { token: string }) {
  const [state, action, pending] = useActionState<FormState, FormData>(acceptInvitation, {});
  return (
    <form action={action} className="mt-6 grid gap-5">
      <input type="hidden" name="token" value={token} />
      <Field id="password" label="Choose a password" hint="At least 12 characters. Avoid passwords you use elsewhere.">
        <Input id="password" name="password" type="password" autoComplete="new-password" minLength={12} required autoFocus />
      </Field>
      <Field id="confirm" label="Type it again">
        <Input id="confirm" name="confirm" type="password" autoComplete="new-password" minLength={12} required />
      </Field>
      <p role="alert" className={state.error ? "type-body-sm font-semibold text-error" : "sr-only"}>
        {state.error}
      </p>
      <MiseButton type="submit" size="lg" disabled={pending}>
        {pending ? "Setting up…" : "Set password"}
      </MiseButton>
    </form>
  );
}
