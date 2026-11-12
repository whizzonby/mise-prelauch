import type { Plan, Segment } from "@mise/api-client";

import { CatalogForm } from "@/components/catalog-form";
import { Labelled, TextInput } from "@/components/commerce-ui";
import { humanize } from "@/lib/format";

import { saveSegment } from "@/app/(console)/crm-actions";

const STATUSES = ["active", "pause_scheduled", "paused", "past_due", "cancel_scheduled", "cancelled", "expired"];
const LIFECYCLE = ["prospect", "subscriber", "lapsed"];
const TOPICS = ["meal_updates", "weekly_menu", "promotions", "referral_updates"];

function Checks({ name, legend, values, chosen, label = humanize }: { name: string; legend: string; values: string[]; chosen: string[]; label?: (v: string) => string }) {
  return (
    <fieldset className="grid gap-1.5">
      <legend className="type-label mb-1">{legend}</legend>
      <div className="flex flex-wrap gap-x-4 gap-y-2">
        {values.map((v) => (
          <label key={v} className="type-body-sm flex min-h-8 items-center gap-2">
            <input type="checkbox" name={name} value={v} defaultChecked={chosen.includes(v)} className="size-4" />
            {label(v)}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

/**
 * Segment rules. All rules combine with AND; values inside one rule with
 * OR. Membership is computed when the segment is read, never stored.
 */
export function SegmentForm({ segment, plans }: { segment?: Segment; plans: Plan[] }) {
  const r = segment?.rules ?? {};
  const planName = (id: string) => plans.find((p) => p.id === id)?.name ?? id;
  return (
    <CatalogForm action={saveSegment} submit={segment ? "Save segment" : "Create segment"} pending="Saving…" label="Segment">
      {segment ? <input type="hidden" name="id" value={segment.id} /> : null}
      <div className="grid gap-4 sm:grid-cols-2">
        <Labelled label="Name">
          <TextInput name="name" required maxLength={80} defaultValue={segment?.name} />
        </Labelled>
        <Labelled label="Description">
          <TextInput name="description" maxLength={300} defaultValue={segment?.description} />
        </Labelled>
      </div>
      <p className="type-caption text-muted">A customer must match every rule you fill in. Inside one rule, any of the ticked values matches.</p>
      <Checks name="lifecycle" legend="Lifecycle" values={LIFECYCLE} chosen={r.lifecycle ?? []} />
      {plans.length ? <Checks name="plan_ids" legend="Plan" values={plans.map((p) => p.id)} chosen={r.plan_ids ?? []} label={planName} /> : null}
      <Checks name="subscription_statuses" legend="Subscription status" values={STATUSES} chosen={r.subscription_statuses ?? []} />
      <div className="grid gap-4 sm:grid-cols-2">
        <Labelled label="Joined from">
          <TextInput type="date" name="joined_from" defaultValue={r.joined_from ?? ""} />
        </Labelled>
        <Labelled label="Joined to">
          <TextInput type="date" name="joined_to" defaultValue={r.joined_to ?? ""} />
        </Labelled>
        <Labelled label="Regions" hint="Region keys, separated by commas.">
          <TextInput name="regions" defaultValue={(r.regions ?? []).join(", ")} />
        </Labelled>
        <Labelled label="Dietary preferences" hint="For example: vegetarian, pescatarian">
          <TextInput name="dietary" defaultValue={(r.dietary ?? []).join(", ")} />
        </Labelled>
        <Labelled label="Acquisition sources" hint="First-touch source, for example: instagram, referral">
          <TextInput name="sources" defaultValue={(r.sources ?? []).join(", ")} />
        </Labelled>
        <Labelled label="Agreed to emails about" hint="Only customers who opted in to this topic.">
          <select name="consent_topic" defaultValue={r.consent_topic ?? ""} className="min-h-11 w-full rounded-md border border-border-strong bg-surface px-3">
            <option value="">Any (no consent rule)</option>
            {TOPICS.map((t) => (
              <option key={t} value={t}>
                {humanize(t)}
              </option>
            ))}
          </select>
        </Labelled>
        <Labelled label="At least this many orders">
          <TextInput type="number" min={0} name="min_orders" defaultValue={r.min_orders ?? ""} />
        </Labelled>
        <Labelled label="Subscribed for at least (weeks)">
          <TextInput type="number" min={0} name="min_tenure_weeks" defaultValue={r.min_tenure_weeks ?? ""} />
        </Labelled>
      </div>
    </CatalogForm>
  );
}
