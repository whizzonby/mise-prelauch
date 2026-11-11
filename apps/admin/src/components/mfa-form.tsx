"use client";

import { Field, Input, MiseButton } from "@mise/ui";
import { useActionState } from "react";

import { verifyCode, type FormState } from "@/app/actions";

export function MfaForm({ mode }: { mode: "enrol" | "verify" }) {
  const [state, action, pending] = useActionState<FormState, FormData>(verifyCode, {});
  return (
    <form action={action} className="mt-6 grid gap-5">
      <input type="hidden" name="mode" value={mode} />
      <Field id="code" label="6-digit code" error={state.error}>
        <Input
          id="code"
          name="code"
          inputMode="numeric"
          autoComplete="one-time-code"
          pattern="[0-9 ]{6,7}"
          maxLength={7}
          required
          autoFocus
          aria-invalid={state.error ? true : undefined}
          aria-describedby={state.error ? "code-error" : undefined}
          className="type-figures tracking-[0.3em]"
        />
      </Field>
      <MiseButton type="submit" size="lg" disabled={pending}>
        {pending ? "Checking…" : mode === "enrol" ? "Turn on and continue" : "Continue"}
      </MiseButton>
    </form>
  );
}
