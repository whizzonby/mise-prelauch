import { cx, Heading } from "@mise/ui";
import type { ReactNode } from "react";

/** A titled block of the admin page. */
export function Panel({ title, note, className, children }: { title: string; note?: ReactNode; className?: string; children: ReactNode }) {
  return (
    <section className={cx("border border-border bg-surface p-5 sm:p-6", className)}>
      <div className="mb-4 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <Heading as="h2" size="h4">
          {title}
        </Heading>
        {note ? <p className="type-caption text-muted">{note}</p> : null}
      </div>
      {children}
    </section>
  );
}

/** One headline number with its label and an optional line of context. */
export function StatTile({ label, value, context }: { label: string; value: string; context?: string }) {
  return (
    <div className="border-t-2 border-primary bg-surface p-5">
      <p className="type-body-sm text-muted">{label}</p>
      <p className="type-figures mt-1 font-display text-[2.5rem] leading-none text-primary">{value}</p>
      {context ? <p className="type-caption mt-2 text-muted">{context}</p> : null}
    </div>
  );
}

/** Rows of term and value, for a lead's details. */
export function DetailList({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[minmax(7rem,auto)_1fr] gap-x-5 gap-y-2.5">
      {items.map(([term, value]) => (
        <div key={term} className="contents">
          <dt className="type-body-sm text-muted">{term}</dt>
          <dd className="type-body-sm min-w-0 break-words">{value ?? <span className="text-muted">Not given</span>}</dd>
        </div>
      ))}
    </dl>
  );
}
