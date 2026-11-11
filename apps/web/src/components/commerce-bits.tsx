import type { Amounts } from "@mise/api-client";
import { cx } from "@mise/ui";
import { FlaskConical } from "lucide-react";

import { formatMoney } from "@/lib/commerce";

/** Shown wherever money would move while only the test provider exists. */
export function TestModeNotice({ className }: { className?: string }) {
  return (
    <p role="note" className={cx("type-body-sm flex items-start gap-3 border border-warning bg-surface p-4", className)}>
      <FlaskConical aria-hidden="true" className="mt-0.5 size-5 shrink-0 text-warning" strokeWidth={1.75} />
      <span>
        <strong>Test mode.</strong> Mise has not chosen its payment provider yet, so checkout uses a test page. No card is asked for and
        no real money moves.
      </span>
    </p>
  );
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
