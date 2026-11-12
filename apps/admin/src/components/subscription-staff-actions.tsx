import type { AdminSubscription } from "@mise/api-client";

import { subscriptionAction } from "@/app/(console)/crm-actions";
import { CatalogForm } from "@/components/catalog-form";
import { Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { humanize } from "@/lib/format";

const REASONS = ["customer_request", "payment_issue", "delivery_issue", "quality_issue", "duplicate_account", "other"];
const CANCEL_REASONS = ["too_expensive", "not_enough_time", "meals_not_for_me", "moving", "temporary", "other"];

function Reason() {
  return (
    <>
      <Labelled label="Reason">
        <SelectInput name="reason" defaultValue="customer_request">
          {REASONS.map((r) => (
            <option key={r} value={r}>
              {humanize(r)}
            </option>
          ))}
        </SelectInput>
      </Labelled>
      <Labelled label="Note" hint="Optional; recorded in the audit log with the reason.">
        <TextInput name="note" maxLength={300} />
      </Labelled>
    </>
  );
}

/**
 * Pause, resume or cancel on the customer's behalf. These run the same
 * rules as the customer's own buttons (cutoffs, paid weeks, refunds are
 * untouched), add a reason code, and send the customer the usual email.
 */
export function SubscriptionStaffActions({ s }: { s: AdminSubscription }) {
  const a = s.actions;
  if (!a.pause && !a.resume && !a.cancel) return null;
  return (
    <Panel title="Act for the customer" note="The same rules as the customer's own controls apply. The customer is emailed and the action is audited with your reason." className="mt-8">
      <div className="grid gap-6 lg:grid-cols-3">
        {a.pause ? (
          <section aria-label="Pause">
            <h3 className="type-body font-semibold mb-3">Pause</h3>
            <CatalogForm action={subscriptionAction} submit="Pause subscription" pending="Pausing…" label="Pause subscription">
              <input type="hidden" name="id" value={s.id} />
              <input type="hidden" name="action" value="pause" />
              <Labelled label="Weeks">
                <SelectInput name="weeks" defaultValue="1">
                  {Array.from({ length: a.max_pause_weeks }, (_, i) => i + 1).map((n) => (
                    <option key={n} value={n}>
                      {n} {n === 1 ? "week" : "weeks"}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Reason />
            </CatalogForm>
          </section>
        ) : null}
        {a.resume ? (
          <section aria-label="Resume">
            <h3 className="type-body font-semibold mb-3">Resume</h3>
            <CatalogForm action={subscriptionAction} submit="Resume subscription" pending="Resuming…" label="Resume subscription">
              <input type="hidden" name="id" value={s.id} />
              <input type="hidden" name="action" value="resume" />
              <Reason />
            </CatalogForm>
          </section>
        ) : null}
        {a.cancel ? (
          <section aria-label="Cancel">
            <h3 className="type-body font-semibold mb-3">Cancel</h3>
            <CatalogForm action={subscriptionAction} submit="Cancel subscription" pending="Cancelling…" label="Cancel subscription">
              <input type="hidden" name="id" value={s.id} />
              <input type="hidden" name="action" value="cancel" />
              <Labelled label="Customer's reason">
                <SelectInput name="cancel_reason" defaultValue="other">
                  {CANCEL_REASONS.map((r) => (
                    <option key={r} value={r}>
                      {humanize(r)}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Reason />
              <p className="type-caption text-muted">Weeks already paid for are still delivered. Refunds are a separate step on the payment.</p>
            </CatalogForm>
          </section>
        ) : null}
      </div>
    </Panel>
  );
}
