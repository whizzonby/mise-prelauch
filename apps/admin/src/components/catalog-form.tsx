"use client";

import { MiseButton } from "@mise/ui";
import { useActionState, type ReactNode } from "react";
import { useFormStatus } from "react-dom";

import type { CatalogState } from "@/app/(console)/meals/actions";

/** A catalogue form with its outcome (errors by field, or what was saved) above the button. */
export function CatalogForm({
  action,
  submit,
  pending,
  children,
  label,
}: {
  action: (previous: CatalogState, form: FormData) => Promise<CatalogState>;
  submit: string;
  pending: string;
  children: ReactNode;
  label: string;
}) {
  const [state, formAction] = useActionState<CatalogState, FormData>(action, {});
  return (
    <form action={formAction} aria-label={label} noValidate className="grid gap-5">
      {children}
      <div aria-live="polite">
        {state.error ? (
          <div role="alert" className="border-l-2 border-error pl-3">
            <p className="type-body-sm font-semibold text-error">{state.error}</p>
            {state.fields?.length ? (
              <ul className="type-caption mt-1 list-disc pl-5">
                {state.fields.map((f) => (
                  <li key={f.field + f.message}>
                    <span className="font-semibold">{f.field.replace(/_/g, " ")}:</span> {f.message}
                  </li>
                ))}
              </ul>
            ) : null}
          </div>
        ) : state.message ? (
          <p className="type-body-sm border-l-2 border-success pl-3">{state.message}</p>
        ) : null}
      </div>
      <div>
        <Submit pending={pending}>{submit}</Submit>
      </div>
    </form>
  );
}

function Submit({ children, pending }: { children: ReactNode; pending: string }) {
  const status = useFormStatus();
  return (
    <MiseButton type="submit" disabled={status.pending}>
      {status.pending ? pending : children}
    </MiseButton>
  );
}
