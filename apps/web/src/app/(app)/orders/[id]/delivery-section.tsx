import type { CustomerDelivery } from "@mise/api-client";
import { cx } from "@mise/ui";
import { Check } from "lucide-react";

import { Form, FormMessage, SubmitButton, TextField } from "@/components/forms";
import { shortDate } from "@/lib/commerce";

import { deliverySupport, requestReschedule } from "./actions";

const time = (iso: string) =>
  new Intl.DateTimeFormat("en-TT", {
    timeZone: "America/Port_of_Spain",
    weekday: "short",
    day: "numeric",
    month: "short",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(iso));

/**
 * The order's delivery, mobile first: where it stands in plain words, the
 * confirmed date, the code to give the courier while it is out, and only the
 * milestones that actually happened. Milestones, not live tracking.
 */
export function DeliverySection({ orderId, d }: { orderId: string; d: CustomerDelivery }) {
  return (
    <section aria-labelledby="delivery" className="border border-border bg-surface p-5">
      <h2 id="delivery" className="type-h3">
        {d.headline}
      </h2>
      <p className="type-body-sm mt-1 text-muted">{d.detail}</p>
      {d.service_date ? (
        <p className="type-body mt-3 font-semibold">
          {shortDate(d.service_date)}
          {d.window ? `, ${d.window}` : ""}
        </p>
      ) : null}
      {d.issue ? <p className="type-body-sm mt-3 border-l-2 border-accent pl-3">{d.issue}</p> : null}
      {d.confirmation_code ? (
        <div className="mt-4 border-2 border-foreground p-4">
          <p className="type-caption text-muted">Your delivery code</p>
          <p className="font-mono text-[2rem] font-bold tracking-[0.3em]">{d.confirmation_code}</p>
          <p className="type-caption mt-1">{d.code_explanation}</p>
        </div>
      ) : null}

      <ol className="mt-6 grid" aria-label="Delivery progress">
        {d.timeline.map((m, i) => (
          <li key={m.key} className="relative flex gap-3 pb-5 last:pb-0">
            {i < d.timeline.length - 1 ? (
              <span aria-hidden="true" className={cx("absolute left-[0.6875rem] top-6 h-full w-px", m.done ? "bg-foreground" : "bg-border")} />
            ) : null}
            <span
              aria-hidden="true"
              className={cx(
                "relative z-10 flex size-6 shrink-0 items-center justify-center rounded-full border-2",
                m.done ? "border-foreground bg-foreground text-surface" : "border-border bg-surface",
              )}
            >
              {m.done ? <Check className="size-3.5" strokeWidth={3} /> : null}
            </span>
            <span>
              <span className={cx("type-body-sm block", m.done ? "font-semibold" : "text-muted")}>
                {m.label}
                <span className="sr-only">{m.done ? " (done)" : " (not yet)"}</span>
              </span>
              {m.at ? <span className="type-caption block text-muted">{time(m.at)}</span> : null}
            </span>
          </li>
        ))}
      </ol>
      {d.history.some((h) => h.outcome !== "Delivered") ? (
        <ul className="type-caption mt-4 text-muted">
          {d.history.map((h, i) => (
            <li key={i}>
              Attempt {h.attempt}: {h.outcome}, {time(h.at)}
            </li>
          ))}
        </ul>
      ) : null}

      <div className="mt-6 grid gap-4 border-t border-border pt-4">
        {d.open_request ? (
          <p className="type-body-sm">
            You asked to change the date{d.open_request.preferred_date ? ` to ${shortDate(d.open_request.preferred_date)}` : ""}. We&apos;ll confirm before
            anything changes.
          </p>
        ) : d.can_reschedule ? (
          <details>
            <summary className="type-body-sm min-h-11 cursor-pointer py-2 font-semibold underline underline-offset-4">Ask to change the delivery date</summary>
            <p className="type-caption mt-1 text-muted">{d.reschedule_policy}</p>
            <Form action={requestReschedule} aria-label="Ask to change the delivery date" className="mt-3">
              <input type="hidden" name="order_id" value={orderId} />
              <TextField name="preferred_date" label="A date that suits you" type="date" optional />
              <TextField name="reason" label="Why" />
              <FormMessage />
              <SubmitButton pending="Sending…" variant="secondary">
                Send request
              </SubmitButton>
            </Form>
          </details>
        ) : null}
        <details>
          <summary className="type-body-sm min-h-11 cursor-pointer py-2 font-semibold underline underline-offset-4">Get help with this delivery</summary>
          <Form action={deliverySupport} aria-label="Get help with this delivery" className="mt-3">
            <input type="hidden" name="order_id" value={orderId} />
            <TextField name="message" label="What's wrong?" />
            <FormMessage />
            <SubmitButton pending="Sending…" variant="secondary">
              Send to Mise
            </SubmitButton>
          </Form>
        </details>
      </div>
    </section>
  );
}
