import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { Labelled, Panel, TextArea, TextInput } from "@/components/commerce-ui";
import { PROOF_LABELS } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { saveDeliverySettings } from "../../delivery-actions";

export const metadata: Metadata = { title: "Delivery settings" };

/** Business policies. They ship unset; each one blocks something until an approved decision is recorded. */
export default async function DeliverySettingsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_zones.read")) notFound();
  const s = await api.delivery.settings(token);
  const manage = can(admin, "delivery_settings.manage");
  const box = (name: string, label: string, checked: boolean, hint?: string) => (
    <label className="flex min-h-11 items-start gap-3">
      <input type="checkbox" name={name} defaultChecked={checked} disabled={!manage} className="mt-1 size-5" />
      <span>
        {label}
        {hint ? <span className="type-caption block text-muted">{hint}</span> : null}
      </span>
    </label>
  );
  return (
    <>
      <PageHeader title="Delivery settings" description={`Last changed ${formatDateTime(s.updated_at)}. Changes are audited.`} />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[1fr_1.2fr]">
        <Notice tone="warning" title="Blocked until decided" items={s.blockers} />
        <Panel title="Policies">
          <KForm action={saveDeliverySettings} submit="Save settings" pending="Saving…" label="Delivery settings">
            <fieldset className="grid gap-1">
              <legend className="type-body-sm mb-1 font-semibold">Accepted proof of delivery</legend>
              {Object.entries(PROOF_LABELS).map(([k, v]) => (
                <label key={k} className="flex min-h-11 items-center gap-3">
                  <input type="checkbox" name="proof_methods" value={k} defaultChecked={s.proof_methods.includes(k)} disabled={!manage} className="size-5" />
                  {v}
                </label>
              ))}
              <p className="type-caption text-muted">Photo proof is unavailable: there is no secure evidence storage.</p>
            </fieldset>
            <Labelled label="Approved proof policy (document or decision)">
              <TextInput name="proof_policy_ref" defaultValue={s.proof_policy_ref} disabled={!manage} />
            </Labelled>
            <fieldset className="grid gap-3 sm:grid-cols-3">
              <legend className="type-body-sm mb-1 font-semibold sm:col-span-3">Which days may deliver an order&apos;s week</legend>
              <Labelled label="First day" hint="Days after the week start (Monday = 0).">
                <TextInput name="delivery_offset_min_days" type="number" min={0} max={27} defaultValue={s.delivery_offset_min_days ?? ""} disabled={!manage} />
              </Labelled>
              <Labelled label="Last day">
                <TextInput name="delivery_offset_max_days" type="number" min={0} max={27} defaultValue={s.delivery_offset_max_days ?? ""} disabled={!manage} />
              </Labelled>
              <Labelled label="Approved decision">
                <TextInput name="delivery_week_rule_ref" defaultValue={s.delivery_week_rule_ref} disabled={!manage} />
              </Labelled>
              <p className="type-caption text-muted sm:col-span-3">Empty: no rule, so nothing can be scheduled. Every scheduling, reschedule and assignment is checked against it.</p>
            </fieldset>
            <Labelled label="Customer reschedule cutoff (hours before the date)" hint="Empty: no policy. Requests are collected; no date is promised.">
              <TextInput name="reschedule_cutoff_hours" type="number" min={0} max={336} defaultValue={s.reschedule_cutoff_hours ?? ""} disabled={!manage} />
            </Labelled>
            <Labelled label="Failure reasons (one per line)">
              <TextArea name="failure_reasons" rows={5} defaultValue={s.failure_reasons.join("\n")} disabled={!manage} />
            </Labelled>
            {box("allow_unapproved_partners", "Allow unapproved partners by exception", s.allow_unapproved_partners, "Only with a documented exception policy; each use needs a reason.")}
            <Labelled label="Exception policy reference">
              <TextInput name="unapproved_partner_policy_ref" defaultValue={s.unapproved_partner_policy_ref} disabled={!manage} />
            </Labelled>
            {box("label_certified", "Package labels are approved for live use", s.label_certified)}
            {box("customer_emails_approved", "Customer delivery email wording is approved", s.customer_emails_approved, "Until then, delivery emails are recorded as skipped.")}
          </KForm>
        </Panel>
      </div>
    </>
  );
}
