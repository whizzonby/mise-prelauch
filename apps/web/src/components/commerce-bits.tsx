import type { Amounts, PaymentOption } from "@mise/api-client";
import { cx } from "@mise/ui";
import { FlaskConical } from "lucide-react";

import { formatMoney } from "@/lib/commerce";

/** Shown wherever money would move while only the test provider exists. */
export function TestModeNotice({ className }: { className?: string }) {
  return (
    <p role="note" className={cx("type-body-sm flex items-start gap-3 border border-warning bg-surface p-4", className)}>
      <FlaskConical aria-hidden="true" className="mt-0.5 size-5 shrink-0 text-warning" strokeWidth={1.75} />
      <span>
        <strong>Test mode.</strong> Payments are not live yet, so checkout uses a test page. No real card is charged and no real money
        moves.
      </span>
    </p>
  );
}

/**
 * How the customer pays: the server's list of methods open now for this
 * currency. One method is chosen silently; several are offered as a choice.
 * The API checks the choice again, so this list is only a convenience.
 */
export function PaymentChoice({ options, className }: { options: PaymentOption[]; className?: string }) {
  if (options.length === 0) {
    return (
      <p role="alert" className={cx("type-body-sm border-l-4 border-warning pl-4", className)}>
        Online payment is not available right now. Please try again later.
      </p>
    );
  }
  if (options.length === 1) {
    const only = options[0]!;
    return (
      <div className={className}>
        <input type="hidden" name="provider" value={only.provider} />
        {only.recurring ? null : <ManualRenewalNote />}
      </div>
    );
  }
  return (
    <fieldset className={className}>
      <legend className="type-h4">Pay with</legend>
      <div className="mt-3 grid gap-2">
        {options.map((o, i) => (
          <label
            key={o.provider}
            className="flex min-h-11 cursor-pointer items-start gap-3 border border-border bg-background p-3 has-[:checked]:border-primary has-[:checked]:ring-1 has-[:checked]:ring-primary"
          >
            <input type="radio" name="provider" value={o.provider} defaultChecked={i === 0} required className="mt-0.5 size-5 accent-[var(--color-primary)]" />
            <span>
              <span className="type-body-sm block font-semibold">
                {o.label}
                {o.test ? <span className="type-caption ml-2 font-normal text-muted">(test)</span> : null}
              </span>
              <span className="type-caption block text-muted">
                {o.recurring ? "Saved securely by the provider for your weekly payments." : "You'll pay each week yourself."}
              </span>
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}

function ManualRenewalNote() {
  return <p className="type-caption text-muted">This method can&apos;t be charged automatically: you&apos;ll pay each week from your subscription page.</p>;
}

/** The breakdown of what is charged, every line from the server. */
export function AmountTable({ amounts, caption, totalLabel = "Total" }: { amounts: Amounts; caption?: string; totalLabel?: string }) {
  const cur = amounts.currency;
  const rows: [string, string][] = [["Plan, one week", formatMoney(amounts.subtotal_minor, cur)]];
  if (amounts.discount_minor > 0) rows.push([`Discount${amounts.discount ? ` (${amounts.discount.code})` : ""}`, formatMoney(-amounts.discount_minor, cur)]);
  rows.push(["Delivery", amounts.delivery_fee_minor > 0 ? formatMoney(amounts.delivery_fee_minor, cur) : formatMoney(0, cur)]);
  if (amounts.tax.mode === "exclusive") rows.push([amounts.tax.label || "Tax", formatMoney(amounts.tax_minor, cur)]);
  return (
    <table className="type-body-sm type-figures w-full">
      {caption ? <caption className="type-caption mb-2 text-left font-semibold uppercase tracking-[0.08em] text-muted">{caption}</caption> : null}
      <tbody>
        {rows.map(([label, value]) => (
          <tr key={label} className="border-b border-border">
            <th scope="row" className="py-2.5 text-left font-normal text-muted">
              {label}
            </th>
            <td className="py-2.5 text-right">{value}</td>
          </tr>
        ))}
        <tr>
          <th scope="row" className="pt-3 text-left font-semibold">
            {totalLabel}
          </th>
          <td className="pt-3 text-right font-display text-[1.5rem]">{formatMoney(amounts.total_minor, cur)}</td>
        </tr>
      </tbody>
    </table>
  );
}

/** Where the customer is in subscribing. */
export function Steps({ current }: { current: 1 | 2 | 3 }) {
  const steps = ["Plan and address", "Review", "Payment"];
  return (
    <ol className="type-caption flex flex-wrap gap-x-6 gap-y-2 font-semibold uppercase tracking-[0.08em]" aria-label="Steps">
      {steps.map((s, i) => (
        <li key={s} aria-current={i + 1 === current ? "step" : undefined} className={i + 1 === current ? "text-accent" : "text-muted"}>
          {i + 1}. {s}
        </li>
      ))}
    </ol>
  );
}
