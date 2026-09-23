import type { ComponentPropsWithoutRef, ReactNode } from "react";

import { cx } from "./cx";

const base =
  "inline-flex items-center justify-center gap-2 rounded-md font-semibold no-underline " +
  "transition-[background-color,color,border-color,transform] duration-(--duration-fast) ease-interaction " +
  "active:translate-y-px disabled:cursor-not-allowed disabled:opacity-55 aria-disabled:cursor-not-allowed aria-disabled:opacity-55";

const variants = {
  /** The one main action in a view. */
  primary:
    "bg-primary text-primary-foreground hover:bg-foreground [.on-dark_&]:bg-surface [.on-dark_&]:text-primary [.on-dark_&]:hover:bg-background",
  /** A second action that still needs weight. */
  secondary:
    "border border-foreground text-foreground hover:bg-foreground hover:text-surface " +
    "[.on-dark_&]:border-primary-foreground [.on-dark_&]:text-primary-foreground [.on-dark_&]:hover:bg-surface [.on-dark_&]:hover:text-primary",
  /** Low-emphasis action that reads as part of the text. */
  quiet: "underline decoration-1 underline-offset-[0.3em] hover:decoration-2 !px-0 !min-h-0",
} as const;

const sizes = {
  md: "min-h-11 px-5 text-[0.9375rem]",
  lg: "min-h-14 px-7 text-[1.0625rem]",
} as const;

export type ButtonVariant = keyof typeof variants;
export type ButtonSize = keyof typeof sizes;

/** Class names for a Mise button, for elements this package cannot render (e.g. a framework Link). */
export function buttonClasses(variant: ButtonVariant = "primary", size: ButtonSize = "md", className?: string) {
  return cx(base, variants[variant], sizes[size], className);
}

type Common = { variant?: ButtonVariant; size?: ButtonSize; className?: string; children: ReactNode };
type AsButton = Common & Omit<ComponentPropsWithoutRef<"button">, keyof Common> & { href?: undefined };
type AsLink = Common & Omit<ComponentPropsWithoutRef<"a">, keyof Common> & { href: string };

/** A button, or a link styled as one when `href` is given. */
export function MiseButton(props: AsButton | AsLink) {
  const { variant, size, className, children, ...rest } = props;
  const classes = buttonClasses(variant, size, className);
  if (rest.href !== undefined) {
    return (
      <a className={classes} {...(rest as ComponentPropsWithoutRef<"a">)}>
        {children}
      </a>
    );
  }
  const buttonProps = rest as ComponentPropsWithoutRef<"button">;
  return (
    <button type={buttonProps.type ?? "button"} className={classes} {...buttonProps}>
      {children}
    </button>
  );
}
