import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag, Timeline } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { CRITERIA_LABELS, Hidden } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { partnerCriterion, partnerStatus, savePartner } from "../../../delivery-actions";

export const metadata: Metadata = { title: "Delivery partner" };

const NEXT: Record<string, string[]> = {
  draft: ["pending_approval", "retired"],
  pending_approval: ["approved", "draft", "retired"],
  approved: ["suspended", "retired"],
  suspended: ["pending_approval", "retired"],
  retired: [],
};

export default async function PartnerPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_partners.read")) notFound();
  const { id } = await params;
  const [p, zones] = await Promise.all([
    api.delivery.partner(token, id).catch((e) => {
      if (e instanceof ApiError && e.status === 404) notFound();
      throw e;
    }),
    api.delivery.zones(token),
  ]);
  const manage = can(admin, "delivery_partners.manage");
  const missing = [
    ...(p.agreement_status !== "signed" ? ["A signed agreement is not recorded."] : []),
    ...(p.criteria ?? []).filter((c) => !c.met).map((c) => `Not met: ${CRITERIA_LABELS[c.criterion] ?? c.criterion}.`),
  ];
  return (
    <>
      <PageHeader
        back={{ href: "/delivery/partners", label: "Delivery partners" }}
        title={p.name}
        description={
          <>
            {humanize(p.integration_mode)} · agreement {humanize(p.agreement_status)} · <StatusTag status={p.status} />
            {p.demo ? " · local demo record" : ""} · adapter supports: {p.adapter_supports.length ? p.adapter_supports.map(humanize).join(", ") : "nothing yet (no adapter for this mode)"}
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          {p.status !== "approved" ? <Notice tone="warning" title="Cannot take live deliveries yet" items={missing.length ? missing : ["Waiting for an approval decision."]} /> : null}
          <Panel title="Qualification" note="Evidence for each criterion. These are what must be shown, not contract terms.">
            <ul className="grid gap-3">
              {(p.criteria ?? []).map((c) => (
                <li key={c.criterion} className="border border-border p-3">
                  <p className="type-body-sm font-semibold">
                    {CRITERIA_LABELS[c.criterion] ?? c.criterion}:{" "}
                    {c.met === null ? <span className="text-muted">not recorded</span> : c.met ? <span className="text-success">met</span> : <span className="text-error">not met</span>}
                  </p>
                  {c.evidence ? <p className="type-caption text-muted">{c.evidence}</p> : null}
                  {manage ? (
                    <div className="mt-2">
                      <KForm action={partnerCriterion} submit="Record" pending="…" label={`Record ${c.criterion}`} inline>
                        <Hidden values={{ id: p.id, criterion: c.criterion }} />
                        <Labelled label="Met?">
                          <SelectInput name="met" defaultValue={c.met ? "yes" : "no"}>
                            <option value="yes">Yes</option>
                            <option value="no">No</option>
                          </SelectInput>
                        </Labelled>
                        <Labelled label="Evidence">
                          <TextInput name="evidence" defaultValue={c.evidence} />
                        </Labelled>
                      </KForm>
                    </div>
                  ) : null}
                </li>
              ))}
            </ul>
          </Panel>
          {can(admin, "delivery_partners.approve") && NEXT[p.status]?.length ? (
            <Panel title="Approval decision">
              <KForm action={partnerStatus} submit="Apply" pending="Saving…" label="Change partner status">
                <Hidden values={{ id: p.id }} />
                <Labelled label="Move to">
                  <SelectInput name="status">
                    {NEXT[p.status]!.map((s) => (
                      <option key={s} value={s}>
                        {humanize(s)}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Reason">
                  <TextInput name="reason" />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}
          <Panel title="Approval history">
            <Timeline items={(p.history ?? []).map((h) => ({ at: h.at, title: humanize(h.to), detail: [h.reason, h.actor_name].filter(Boolean).join(" · ") }))} />
          </Panel>
        </div>
        <Panel title="Details" note={p.approved_at ? `Approved ${formatDateTime(p.approved_at)}` : undefined}>
          {manage ? (
            <KForm action={savePartner} submit="Save details" pending="Saving…" label="Edit partner">
              <Hidden values={{ id: p.id, version: p.version }} />
              <div className="grid gap-3 sm:grid-cols-2">
                <Labelled label="Name">
                  <TextInput name="name" defaultValue={p.name} />
                </Labelled>
                <Labelled label="Integration">
                  <SelectInput name="integration_mode" defaultValue={p.integration_mode}>
                    {["manual", "internal_fleet", "file", "api"].map((m) => (
                      <option key={m} value={m}>
                        {humanize(m)}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Contact">
                  <TextInput name="contact_name" defaultValue={p.contact_name} />
                </Labelled>
                <Labelled label="Phone">
                  <TextInput name="phone" defaultValue={p.phone} />
                </Labelled>
                <Labelled label="Email">
                  <TextInput name="email" defaultValue={p.email} />
                </Labelled>
                <Labelled label="Escalation contact">
                  <TextInput name="escalation_contact" defaultValue={p.escalation_contact} />
                </Labelled>
                <Labelled label="Agreement">
                  <SelectInput name="agreement_status" defaultValue={p.agreement_status}>
                    {["none", "negotiating", "signed", "expired", "terminated"].map((m) => (
                      <option key={m} value={m}>
                        {humanize(m)}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Agreement reference">
                  <TextInput name="agreement_ref" defaultValue={p.agreement_ref} />
                </Labelled>
                <Labelled label="Supported windows">
                  <TextInput name="supported_windows" defaultValue={p.supported_windows} />
                </Labelled>
                <Labelled label="Capacity commitment">
                  <TextInput name="capacity_note" defaultValue={p.capacity_note} />
                </Labelled>
                <Labelled label="Billing reference">
                  <TextInput name="billing_reference" defaultValue={p.billing_reference} />
                </Labelled>
              </div>
              <fieldset className="grid gap-1">
                <legend className="type-body-sm mb-1 font-semibold">Zones covered</legend>
                {zones.items.map((z) => (
                  <label key={z.id} className="flex min-h-11 items-center gap-3">
                    <input type="checkbox" name="zone_ids" value={z.id} defaultChecked={p.zone_ids.includes(z.id)} className="size-5" />
                    {z.code} · {z.name}
                  </label>
                ))}
              </fieldset>
              <Labelled label="Notes">
                <TextArea name="notes" defaultValue={p.notes} rows={3} />
              </Labelled>
            </KForm>
          ) : (
            <dl className="type-body-sm grid gap-1">
              <dt className="font-semibold">Contact</dt>
              <dd>{[p.contact_name, p.phone, p.email].filter(Boolean).join(" · ") || "—"}</dd>
            </dl>
          )}
        </Panel>
      </div>
    </>
  );
}
