import { cx } from "@mise/ui";
import type { ComponentPropsWithoutRef, ReactNode } from "react";

/** Small building blocks for the commerce screens, in the console's style. */

const control = "min-h-11 w-full rounded-md border border-border-strong bg-surface px-3";

export function Labelled({ label, hint, children, className }: { label: string; hint?: string; children: ReactNode; className?: string }) {
  return (
    <label className={cx("grid gap-1.5", className)}>
      <span className="type-label">{label}</span>
      {children}
      {hint ? <span className="type-caption text-muted">{hint}</span> : null}
    </label>
  );
}

export function TextInput(props: ComponentPropsWithoutRef<"input">) {
  return <input {...props} className={cx(control, props.className)} />;
}

export function TextArea(props: ComponentPropsWithoutRef<"textarea">) {
  return <textarea {...props} className={cx(control, "py-2", props.className)} />;
}

export function SelectInput(props: ComponentPropsWithoutRef<"select">) {
  return <select {...props} className={cx(control, props.className)} />;
}

export function Panel({ title, note, children, className }: { title?: string; note?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cx("border border-border bg-surface p-5 sm:p-6", className)}>
      {title ? <h2 className={cx("type-h4", note ? "mb-1" : "mb-4")}>{title}</h2> : null}
      {note ? <p className="type-caption mb-4 text-muted">{note}</p> : null}
      {children}
    </section>
  );
}

/** A table that scrolls sideways on narrow screens; the scroll area is focusable so it can be scrolled from the keyboard. */
export function DataTable({ head, children, minWidth = "44rem", label }: { head: string[]; children: ReactNode; minWidth?: string; label?: string }) {
  return (
    <div
      className="overflow-x-auto border border-border bg-surface"
      tabIndex={0}
      role="region"
      aria-label={label ?? (head.filter(Boolean).join(", ") || "Table")}
    >
      <table className="w-full text-left" style={{ minWidth }}>
        <thead className="type-caption border-b border-border text-muted">
          <tr>
            {head.map((h) => (
              <th key={h} scope="col" className="p-3 font-semibold">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="type-body-sm divide-y divide-border">{children}</tbody>
      </table>
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="type-body-sm border border-dashed border-border-strong p-6 text-muted">{children}</p>;
}
