import type { ComponentPropsWithoutRef, ReactNode } from "react";

import { cx } from "./cx";

const control =
  "w-full rounded-md border border-border-strong bg-surface px-3.5 text-foreground min-h-12 " +
  "placeholder:text-muted/80 transition-[border-color,box-shadow] duration-(--duration-fast) ease-interaction " +
  "hover:border-foreground focus-visible:border-foreground focus-visible:outline-offset-0 " +
  "aria-invalid:border-error aria-invalid:shadow-[inset_0_0_0_1px_var(--color-error)]";

type FieldProps = {
  /** The id of the control inside. The label, hint and error are wired to it. */
  id: string;
  label: ReactNode;
  /** Shown beside the label for fields that may be left blank. */
  optional?: boolean;
  hint?: ReactNode;
  error?: string;
  className?: string;
  children: ReactNode;
};

/** IDs a control should reference so assistive technology reads its hint and error. */
export function describedBy(id: string, { hint, error }: { hint?: unknown; error?: unknown }) {
  return cx(hint ? `${id}-hint` : undefined, error ? `${id}-error` : undefined) || undefined;
}

/** Label, hint and error message around one form control. */
export function Field({ id, label, optional, hint, error, className, children }: FieldProps) {
  return (
    <div className={className}>
      <label htmlFor={id} className="type-label mb-1.5 flex items-baseline justify-between gap-3">
        <span>{label}</span>
        {optional ? <span className="type-caption font-normal text-muted">Optional</span> : null}
      </label>
      {children}
      {hint ? (
        <p id={`${id}-hint`} className="type-caption mt-1.5 text-muted">
          {hint}
        </p>
      ) : null}
      <FieldError id={`${id}-error`} message={error} />
    </div>
  );
}

/** An error line. It is always in the DOM so screen readers announce it when it fills. */
export function FieldError({ id, message }: { id: string; message?: string }) {
  return (
    <p id={id} role="alert" className={cx("type-caption font-semibold text-error", message ? "mt-1.5" : "sr-only")}>
      {message}
    </p>
  );
}

export function Input({ className, ...props }: ComponentPropsWithoutRef<"input">) {
  return <input className={cx(control, className)} {...props} />;
}

export function Select({ className, children, ...props }: ComponentPropsWithoutRef<"select">) {
  return (
    <div className="relative">
      <select className={cx(control, "appearance-none pr-10", className)} {...props}>
        {children}
      </select>
      <svg
        aria-hidden="true"
        viewBox="0 0 12 8"
        className="pointer-events-none absolute right-4 top-1/2 h-2 w-3 -translate-y-1/2 fill-none stroke-foreground"
      >
        <path d="M1 1.5 6 6.5l5-5" strokeWidth="1.5" />
      </svg>
    </div>
  );
}

const box =
  "peer size-6 shrink-0 appearance-none rounded-sm border border-border-strong bg-surface " +
  "transition-colors duration-(--duration-fast) ease-interaction hover:border-foreground " +
  "checked:border-primary checked:bg-primary aria-invalid:border-error";

type CheckboxProps = Omit<ComponentPropsWithoutRef<"input">, "type"> & { label: ReactNode };

export function Checkbox({ label, className, id, ...props }: CheckboxProps) {
  return (
    <div className={cx("flex items-start gap-3", className)}>
      <span className="relative mt-0.5 inline-flex">
        <input type="checkbox" id={id} className={box} {...props} />
        <svg
          aria-hidden="true"
          viewBox="0 0 16 16"
          className="pointer-events-none absolute inset-0 m-auto size-4 fill-none stroke-surface opacity-0 peer-checked:opacity-100"
        >
          <path d="m3 8.5 3.2 3.2L13 4.8" strokeWidth="2" />
        </svg>
      </span>
      <label htmlFor={id} className="type-body-sm">
        {label}
      </label>
    </div>
  );
}

type ChoiceProps = Omit<ComponentPropsWithoutRef<"input">, "type"> & {
  /** `checkbox` allows several choices in a group; `radio` allows one. */
  type?: "checkbox" | "radio";
  label: ReactNode;
};

/**
 * One selectable option drawn as a label-tag. It is a real checkbox or radio
 * underneath, so keyboard and screen-reader behaviour come for free.
 */
export function Choice({ type = "checkbox", label, className, ...props }: ChoiceProps) {
  return (
    <label
      className={cx(
        "type-body-sm inline-flex min-h-11 cursor-pointer select-none items-center gap-2 rounded-md border border-border-strong bg-surface px-3.5",
        "transition-colors duration-(--duration-fast) ease-interaction hover:border-foreground",
        "has-checked:border-primary has-checked:bg-primary has-checked:text-primary-foreground",
        "has-focus-visible:outline-2 has-focus-visible:outline-offset-3 has-focus-visible:outline-(--focus-color)",
        className,
      )}
    >
      <input type={type} className="sr-only" {...props} />
      {label}
    </label>
  );
}

type ChoiceGroupProps = {
  legend: ReactNode;
  optional?: boolean;
  hint?: ReactNode;
  error?: string;
  id: string;
  className?: string;
  children: ReactNode;
};

/** A set of Choice options with a shared question. */
export function ChoiceGroup({ legend, optional, hint, error, id, className, children }: ChoiceGroupProps) {
  return (
    <fieldset className={className} aria-describedby={describedBy(id, { hint, error })}>
      <legend className="type-label mb-1.5 flex w-full items-baseline justify-between gap-3">
        <span>{legend}</span>
        {optional ? <span className="type-caption font-normal text-muted">Optional</span> : null}
      </legend>
      {hint ? (
        <p id={`${id}-hint`} className="type-caption mb-2.5 text-muted">
          {hint}
        </p>
      ) : null}
      <div className="flex flex-wrap gap-2">{children}</div>
      <FieldError id={`${id}-error`} message={error} />
    </fieldset>
  );
}
