import type { Campaign, Promotion, Segment } from "@mise/api-client";

import { saveCampaign } from "@/app/(console)/crm-actions";
import { CatalogForm } from "@/components/catalog-form";
import { Labelled, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { humanize, localInputValue } from "@/lib/format";

const OBJECTIVES = ["acquisition", "activation", "retention", "win_back", "referral", "awareness"];
const CHANNELS = ["email", "social", "paid_social", "search", "partnership", "events", "other"];

/**
 * A campaign record: what it is for, where it runs, and the UTM tag that
 * ties sign-ups back to it. Mise does not send campaign email itself yet;
 * no email provider is connected.
 */
export function CampaignForm({ campaign, segments, promotions }: { campaign?: Campaign; segments: Segment[]; promotions: Promotion[] }) {
  return (
    <CatalogForm action={saveCampaign} submit={campaign ? "Save campaign" : "Create campaign"} pending="Saving…" label="Campaign">
      {campaign ? <input type="hidden" name="id" value={campaign.id} /> : null}
      <Labelled label="Name">
        <TextInput name="name" required maxLength={120} defaultValue={campaign?.name} />
      </Labelled>
      <div className="grid gap-4 sm:grid-cols-2">
        <Labelled label="Objective">
          <SelectInput name="objective" defaultValue={campaign?.objective ?? "acquisition"}>
            {OBJECTIVES.map((o) => (
              <option key={o} value={o}>
                {humanize(o)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <Labelled label="Channel">
          <SelectInput name="channel" defaultValue={campaign?.channel ?? "email"}>
            {CHANNELS.map((o) => (
              <option key={o} value={o}>
                {humanize(o)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <Labelled label="UTM campaign tag" hint="Lowercase letters, numbers, - and _. Links tagged utm_campaign=this are counted as this campaign.">
          <TextInput name="utm_campaign" maxLength={60} defaultValue={campaign?.utm_campaign ?? ""} pattern="[a-z0-9][a-z0-9_\-]{1,59}" />
        </Labelled>
        <Labelled label="Audience segment">
          <SelectInput name="segment_id" defaultValue={campaign?.segment_id ?? ""}>
            <option value="">No segment</option>
            {segments.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <Labelled label="Promotion code">
          <SelectInput name="promotion_id" defaultValue={campaign?.promotion_id ?? ""}>
            <option value="">No promotion</option>
            {promotions.map((p) => (
              <option key={p.id} value={p.id}>
                {p.code}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <div />
        <Labelled label="Starts (Trinidad and Tobago time)">
          <TextInput type="datetime-local" name="starts_at" defaultValue={localInputValue(campaign?.starts_at)} />
        </Labelled>
        <Labelled label="Ends">
          <TextInput type="datetime-local" name="ends_at" defaultValue={localInputValue(campaign?.ends_at)} />
        </Labelled>
      </div>
      <Labelled label="Notes">
        <TextArea name="notes" rows={3} maxLength={2000} defaultValue={campaign?.notes ?? ""} />
      </Labelled>
    </CatalogForm>
  );
}

/** Campaign statuses mapped onto the console's status tones. */
export const campaignTone = (s: string) =>
  s === "active" ? "active" : s === "scheduled" ? "pending" : s === "cancelled" ? "cancelled" : s === "completed" ? "paid" : "draft";
