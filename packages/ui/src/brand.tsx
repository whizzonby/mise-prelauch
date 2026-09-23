import type { ReactNode } from "react";

import { cx } from "./cx";

/** The Mise wordmark: the name set in the display face, closed with a roucou full stop. */
export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cx("font-display leading-none tracking-[-0.02em]", className)}>
      Mise<span className="text-accent [.on-dark_&]:text-primary-foreground">.</span>
    </span>
  );
}

type ProcessStepProps = {
  /** Position in the sequence, starting at 1. */
  number: number;
  title: string;
  className?: string;
  children: ReactNode;
};

/** One step of a real sequence: a large numeral, a title, a sentence or two. */
export function ProcessStep({ number, title, className, children }: ProcessStepProps) {
  return (
    <li className={cx("flex flex-col", className)}>
      <span aria-hidden="true" className="type-figures font-display text-[clamp(3.5rem,6vw,5.5rem)] leading-none text-accent">
        {String(number).padStart(2, "0")}
      </span>
      <h3 className="type-h3 mt-5">
        <span className="sr-only">Step {number}: </span>
        {title}
      </h3>
      <p className="type-body mt-3 max-w-[34ch] text-muted">{children}</p>
    </li>
  );
}

/** Marks a short piece of state: a lead status, a dietary indicator. */
export function Tag({ tone = "neutral", children }: { tone?: "neutral" | "success" | "warning" | "error" | "accent"; children: ReactNode }) {
  const tones = {
    neutral: "border-border-strong text-foreground",
    success: "border-success text-success",
    warning: "border-warning text-warning",
    error: "border-error text-error",
    accent: "border-accent text-accent",
  } as const;
  return (
    <span className={cx("type-caption inline-flex items-center rounded-sm border px-1.5 py-0.5 font-semibold", tones[tone])}>
      {children}
    </span>
  );
}
