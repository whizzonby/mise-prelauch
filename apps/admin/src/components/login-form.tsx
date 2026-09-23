"use client";

import { Field, Input, MiseButton } from "@mise/ui";
import { useActionState } from "react";

import { login, type FormState } from "@/app/actions";

export function LoginForm() {
  const [state, action, pending] = useActionState<FormState, FormData>(login, {});
  return (
    <form action={action} className="mt-6 grid gap-5">
      <Field id="email" label="Email">
        <Input id="email" name="email" type="email" autoComplete="username" required autoFocus />
      </Field>
      <Field id="password" label="Password">
        <Input id="password" name="password" type="password" autoComplete="current-password" required />
      </Field>
      <p role="alert" className={state.error ? "type-body-sm font-semibold text-error" : "sr-only"}>
        {state.error}
      </p>
      <MiseButton type="submit" size="lg" disabled={pending}>
        {pending ? "Signing in…" : "Sign in"}
      </MiseButton>
    </form>
  );
}
