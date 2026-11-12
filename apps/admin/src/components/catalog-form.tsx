"use client";

import { MiseButton } from "@mise/ui";
import { startTransition, useActionState, useEffect, useRef, type FormEvent, type ReactNode } from "react";

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
  const [state, formAction, isPending] = useActionState<CatalogState, FormData>(action, {});
  const ref = useRef<HTMLFormElement>(null);
  // React resets a form after its action runs. Submitting through a
  // transition instead keeps what was typed when the save fails; the form
  // is reset only after a save that worked. Without JavaScript the plain
  // form action still works.
  useEffect(() => {
    if (state.message && !state.error) ref.current?.reset();
  }, [state]);
  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget, (event.nativeEvent as SubmitEvent).submitter);
    startTransition(() => formAction(data));
  }
  return (
    <form ref={ref} action={formAction} onSubmit={onSubmit} aria-label={label} noValidate className="grid gap-5">
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
        <Submit busy={isPending} pending={pending}>
          {submit}
        </Submit>
      </div>
    </form>
  );
}

function Submit({ children, busy, pending }: { children: ReactNode; busy: boolean; pending: string }) {
  return (
    <MiseButton type="submit" disabled={busy}>
      {busy ? pending : children}
    </MiseButton>
  );
}
