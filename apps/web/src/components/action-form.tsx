"use client";

import { useActionState, type ReactNode } from "react";

import type { FormState } from "@/lib/messages";

import { CheckboxField, FormMessage, FormStateProvider, SubmitButton, TextField } from "./forms";

export interface FieldSpec {
  name: string;
  label: string;
  type?: "text" | "email" | "password" | "checkbox";
  autoComplete?: string;
  hint?: string;
}

interface Props {
  action: (previous: FormState, form: FormData) => Promise<FormState>;
  fields: FieldSpec[];
  /** Values sent but not shown, such as the token from an email link. */
  hidden?: Record<string, string>;
  submit: string;
  pending: string;
  /** Shown instead of the form once the action reports done. */
  doneMessage?: ReactNode;
}

/** A signed-out form (sign in, register, reset) wired to a server action. */
export function ActionForm({ action, fields, hidden, submit, pending, doneMessage }: Props) {
  const [state, formAction] = useActionState<FormState, FormData>(action, {});
  if (state.done && doneMessage) return <div className="mt-5">{doneMessage}</div>;

  return (
    <FormStateProvider value={state}>
      <form action={formAction} className="mt-6 grid gap-5" noValidate>
        {Object.entries(hidden ?? {}).map(([name, value]) => (
          <input key={name} type="hidden" name={name} value={value} />
        ))}
        {fields.map((f) =>
          f.type === "checkbox" ? (
            <CheckboxField key={f.name} name={f.name} label={f.label} />
          ) : (
            <TextField key={f.name} name={f.name} label={f.label} type={f.type} autoComplete={f.autoComplete} hint={f.hint} />
          ),
        )}
        <FormMessage />
        <SubmitButton pending={pending} className="min-h-14 text-[1.0625rem]">
          {submit}
        </SubmitButton>
      </form>
    </FormStateProvider>
  );
}
