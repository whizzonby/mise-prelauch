"use client";

import { Checkbox, Choice, ChoiceGroup, cx, Field, FieldError, Input, MiseButton, Select, describedBy } from "@mise/ui";
import { Eye, EyeOff } from "lucide-react";
import { createContext, useActionState, useContext, useEffect, useRef, useState, type ComponentPropsWithoutRef, type ReactNode } from "react";
import { useFormStatus } from "react-dom";

import type { FormState } from "@/lib/messages";

/*
 * Forms post to server actions, so they work before JavaScript loads and the
 * session token never leaves the server. The API validates; its field errors
 * come back here and appear beside each field, announced to screen readers.
 */

const FormContext = createContext<FormState>({});

export function useFormState(): FormState {
  return useContext(FormContext);
}

/** For forms that manage their own action state but use these fields. */
export function FormStateProvider({ value, children }: { value: FormState; children: ReactNode }) {
  return <FormContext value={value}>{children}</FormContext>;
}

type Action = (previous: FormState, form: FormData) => Promise<FormState>;

export function Form({
  action,
  children,
  className,
  resetOnDone,
  "aria-label": ariaLabel,
}: {
  action: Action;
  children: ReactNode;
  className?: string;
  /** Clear the fields after a save that reports done (an "add another" form). */
  resetOnDone?: boolean;
  "aria-label"?: string;
}) {
  const [state, formAction] = useActionState<FormState, FormData>(action, {});
  const ref = useRef<HTMLFormElement>(null);
  useEffect(() => {
    if (resetOnDone && state.done) ref.current?.reset();
  }, [state, resetOnDone]);
  return (
    <FormContext value={state}>
      <form ref={ref} action={formAction} noValidate aria-label={ariaLabel} className={cx("grid gap-5", className)}>
        {children}
      </form>
    </FormContext>
  );
}

/** The form-level outcome: an error, or what was saved. Always in the DOM, so it is announced. */
export function FormMessage() {
  const { error, message } = useFormState();
  return (
    <div aria-live="polite">
      {error ? (
        <p role="alert" className="type-body-sm border-l-2 border-error pl-3 font-semibold text-error">
          {error}
        </p>
      ) : message ? (
        <p className="type-body-sm border-l-2 border-success pl-3">{message}</p>
      ) : null}
    </div>
  );
}

export function SubmitButton({ children, pending, variant = "primary", className }: { children: ReactNode; pending: string; variant?: "primary" | "secondary"; className?: string }) {
  const status = useFormStatus();
  return (
    <MiseButton type="submit" variant={variant} disabled={status.pending} aria-disabled={status.pending} className={className}>
      {status.pending ? pending : children}
    </MiseButton>
  );
}

type TextFieldProps = Omit<ComponentPropsWithoutRef<"input">, "name" | "id"> & {
  name: string;
  label: string;
  hint?: ReactNode;
  optional?: boolean;
  id?: string;
};

export function TextField({ name, label, hint, optional, id, className, type, ...input }: TextFieldProps) {
  const error = useFormState().fields?.[name];
  const fieldId = id ?? name;
  const control =
    type === "password" ? (
      <PasswordInput id={fieldId} name={name} error={error} hint={hint} {...input} />
    ) : (
      <Input
        id={fieldId}
        name={name}
        type={type ?? "text"}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy(fieldId, { hint, error })}
        {...input}
      />
    );
  return (
    <Field id={fieldId} label={label} hint={hint} optional={optional} error={error} className={className}>
      {control}
    </Field>
  );
}

/** A password input with a show/hide button. The button never submits the form. */
export function PasswordInput({
  id,
  error,
  hint,
  ...input
}: Omit<ComponentPropsWithoutRef<"input">, "type"> & { id: string; error?: string; hint?: ReactNode }) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="relative">
      <Input
        id={id}
        type={visible ? "text" : "password"}
        className="pr-14"
        autoCapitalize="none"
        spellCheck={false}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy(id, { hint, error })}
        {...input}
      />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        aria-pressed={visible}
        aria-controls={id}
        className="absolute right-1 top-1/2 inline-flex size-11 -translate-y-1/2 items-center justify-center rounded-md text-muted hover:text-foreground"
      >
        {visible ? <EyeOff aria-hidden="true" className="size-5" strokeWidth={1.75} /> : <Eye aria-hidden="true" className="size-5" strokeWidth={1.75} />}
        <span className="sr-only">{visible ? "Hide password" : "Show password"}</span>
      </button>
    </div>
  );
}

export function SelectField({
  id,
  name,
  label,
  hint,
  optional,
  options,
  defaultValue,
  placeholder,
}: {
  id?: string;
  name: string;
  label: string;
  hint?: ReactNode;
  optional?: boolean;
  options: { key: string; label: string }[];
  defaultValue?: string;
  placeholder?: string;
}) {
  const error = useFormState().fields?.[name];
  const fieldId = id ?? name;
  return (
    <Field id={fieldId} label={label} hint={hint} optional={optional} error={error}>
      <Select
        id={fieldId}
        name={name}
        defaultValue={defaultValue ?? ""}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy(fieldId, { hint, error })}
      >
        {placeholder !== undefined ? <option value="">{placeholder}</option> : null}
        {options.map((o) => (
          <option key={o.key} value={o.key}>
            {o.label}
          </option>
        ))}
      </Select>
    </Field>
  );
}

/** A question answered by choosing one or several tags. */
export function ChoiceField({
  name,
  legend,
  hint,
  optional,
  options,
  selected,
  type = "checkbox",
  errorKey,
}: {
  name: string;
  legend: ReactNode;
  hint?: ReactNode;
  optional?: boolean;
  options: { key: string; label: string }[];
  selected: string[];
  type?: "checkbox" | "radio";
  /** The API field whose error belongs here, if it differs from name. */
  errorKey?: string;
}) {
  const error = useFormState().fields?.[errorKey ?? name];
  return (
    <ChoiceGroup id={name} legend={legend} hint={hint} optional={optional} error={error}>
      {options.map((o) => (
        <Choice key={o.key} type={type} name={name} value={o.key} label={o.label} defaultChecked={selected.includes(o.key)} />
      ))}
    </ChoiceGroup>
  );
}

export function CheckboxField({
  id,
  name,
  label,
  defaultChecked,
  value,
}: {
  id?: string;
  name: string;
  label: ReactNode;
  defaultChecked?: boolean;
  value?: string;
}) {
  const error = useFormState().fields?.[name];
  return (
    <div>
      <Checkbox
        id={id ?? (value ? `${name}-${value}` : name)}
        name={name}
        value={value}
        label={label}
        defaultChecked={defaultChecked}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy(name, { error })}
      />
      <FieldError id={`${name}-error`} message={error} />
    </div>
  );
}
