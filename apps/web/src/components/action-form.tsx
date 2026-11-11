"use client";

import { Checkbox, Field, FieldError, Input, MiseButton, describedBy } from "@mise/ui";
import { useActionState, type ReactNode } from "react";

import type { FormState } from "@/lib/messages";

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

/** A form wired to a server action, with per-field and whole-form errors. */
export function ActionForm({ action, fields, hidden, submit, pending: pendingLabel, doneMessage }: Props) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(action, {});
  if (state.done && doneMessage) return <div className="mt-5">{doneMessage}</div>;

  return (
    <form action={formAction} className="mt-6 grid gap-5" noValidate>
      {Object.entries(hidden ?? {}).map(([name, value]) => (
        <input key={name} type="hidden" name={name} value={value} />
      ))}
      {fields.map((f) => {
        const error = state.fields?.[f.name];
        if (f.type === "checkbox") {
          return (
            <div key={f.name}>
              <Checkbox
                name={f.name}
                id={f.name}
                label={f.label}
                aria-invalid={error ? true : undefined}
                aria-describedby={describedBy(f.name, { error })}
              />
              <FieldError id={`${f.name}-error`} message={error} />
            </div>
          );
        }
        return (
          <Field key={f.name} id={f.name} label={f.label} hint={f.hint} error={error}>
            <Input
              id={f.name}
              name={f.name}
              type={f.type ?? "text"}
              autoComplete={f.autoComplete}
              aria-invalid={error ? true : undefined}
              aria-describedby={describedBy(f.name, { hint: f.hint, error })}
            />
          </Field>
        );
      })}
      <p role="alert" className={state.error ? "type-body-sm font-semibold text-error" : "sr-only"}>
        {state.error}
      </p>
      <MiseButton type="submit" size="lg" disabled={pending}>
        {pending ? pendingLabel : submit}
      </MiseButton>
    </form>
  );
}
