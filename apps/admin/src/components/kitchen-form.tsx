"use client";

import { cx, MiseButton, type ButtonSize, type ButtonVariant } from "@mise/ui";
import { startTransition, useActionState, useEffect, useRef, type FormEvent, type ReactNode } from "react";

import type { CatalogState } from "@/app/(console)/meals/actions";

/**
 * A form with a visible save state: what failed (by field) or what was
 * saved, announced politely. Sized for kitchen tablets when size="lg".
 * Values typed stay in place when a save fails.
 */
export function KForm({
  action,
  submit,
  pending,
  label,
  children,
  variant = "primary",
  size = "md",
  inline = false,
  reset = false,
  secondary,
}: {
  action: (previous: CatalogState, form: FormData) => Promise<CatalogState>;
  submit: string;
  pending: string;
  label: string;
  children?: ReactNode;
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Fields and button on one row (a one-button action). */
  inline?: boolean;
  /** Clear the form after a successful save (new-entry forms). */
  reset?: boolean;
  /** A second submit button: [name=value, label]. */
  secondary?: { name: string; value: string; label: string };
}) {
  const [state, formAction, busy] = useActionState<CatalogState, FormData>(action, {});
  const ref = useRef<HTMLFormElement>(null);
  useEffect(() => {
    if (reset && state.message && !state.error) ref.current?.reset();
  }, [state, reset]);
  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget, (event.nativeEvent as SubmitEvent).submitter);
    startTransition(() => formAction(data));
  }
  return (
    <form ref={ref} action={formAction} onSubmit={onSubmit} aria-label={label} noValidate className={cx(inline ? "flex flex-wrap items-end gap-3" : "grid gap-4")}>
      {children}
      <div className={cx("flex flex-wrap gap-3", inline && "items-end")}>
        <MiseButton type="submit" variant={variant} size={size} disabled={busy}>
          {busy ? pending : submit}
        </MiseButton>
        {secondary ? (
          <MiseButton type="submit" name={secondary.name} value={secondary.value} variant="secondary" size={size} disabled={busy}>
            {secondary.label}
          </MiseButton>
        ) : null}
      </div>
      <div aria-live="polite" className={cx(inline && "basis-full")}>
        {state.error ? (
          <div role="alert" className="border-l-2 border-error pl-3">
            <p className="type-body-sm font-semibold text-error">{state.error}</p>
            {state.fields?.length ? (
              <ul className="type-caption mt-1 list-disc pl-5">
                {state.fields.map((f) => (
                  <li key={f.field + f.message}>
                    <span className="font-semibold">{f.field.replace(/_/g, " ").replace(/\[(\d+)\]/, (_, n) => ` line ${Number(n) + 1}`)}:</span> {f.message}
                  </li>
                ))}
              </ul>
            ) : null}
          </div>
        ) : state.message ? (
          <p className="type-body-sm border-l-2 border-success pl-3">{state.message}</p>
        ) : null}
      </div>
    </form>
  );
}
