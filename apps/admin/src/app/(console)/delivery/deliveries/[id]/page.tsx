import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag, Timeline } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { AddressCard, DELIVERY_LABELS, DeliveryStatus, Hidden, OnlineOnly, PROOF_LABELS, RequestKey } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { correctAddress, deliveryUpdate } from "../../../delivery-actions";

export const metadata: Metadata = { title: "Delivery" };

const SOURCES: Record<string, string> = { staff: "Recorded by staff", partner: "Reported by partner", customer: "From the customer", system: "System" };

/**
 * One delivery: what a courier needs on a phone first (address, packages,
 * next step, proof), then the full history for staff and support.
 */
export default async function DeliveryPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery.read") && !can(admin, "delivery.read_assigned")) notFound();
  const { id } = await params;
  const d = await api.delivery.delivery(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const settings = await api.delivery.settings(token).catch(() => null);
  const out = ["picked_up", "in_transit", "arriving"].includes(d.status);
  const update = can(admin, "delivery.update_status");
  const confirm = can(admin, "delivery.confirm");
  const proof = can(admin, "delivery_proof.create");
  const methods = settings?.proof_methods ?? [];
  const hasProof = d.has_proof;
  return (
    <>
      <PageHeader
        back={{ href: can(admin, "delivery.read") ? "/delivery/planning" : "/delivery/runs", label: can(admin, "delivery.read") ? "Dispatch planning" : "My runs" }}
        title={`Delivery ${d.number}`}
        description={
          <>
            Order {d.order_number} · attempt {d.attempt} · {d.service_date ? formatDay(d.service_date) : "no date yet"}
            {d.window_label ? ` · ${d.window_label}` : ""} · {d.zone_code ?? "no zone"} · <DeliveryStatus status={d.status} />
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[1fr_1fr]">
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          {d.blocking_exceptions ? <Notice tone="error" title="Blocked" items={[`${d.blocking_exceptions} open blocking exception(s). Resolve them before dispatch.`]} /> : null}
          <Panel title="Deliver to">
            <AddressCard address={d.address} validation={d.address_validation} />
          </Panel>
          <Panel title="Packages">
            <ul className="type-body-sm grid gap-1">
              {(d.packages ?? []).map((p) => (
                <li key={p.id}>
                  <span className="font-mono font-semibold">{p.code}</span> · {p.contents}
                </li>
              ))}
            </ul>
          </Panel>

          {out && update ? (
            <Panel title="Update progress" note="Milestones only. A location is kept only if you confirm it was verified.">
              <KForm action={deliveryUpdate} submit="Save update" pending="Saving…" label="Record a status update" size="lg">
                <Hidden values={{ id: d.id, action: "status" }} />
                <RequestKey />
                <Labelled label="Update">
                  <SelectInput name="status" defaultValue={d.status === "picked_up" ? "in_transit" : "arriving"}>
                    {d.status === "picked_up" ? <option value="in_transit">On the way</option> : null}
                    {d.status === "in_transit" ? <option value="arriving">Arriving</option> : null}
                    <option value="note">Note only</option>
                  </SelectInput>
                </Labelled>
                <Labelled label="Who reported it">
                  <SelectInput name="source" defaultValue="staff">
                    <option value="staff">Me (staff or courier)</option>
                    <option value="partner">The delivery partner</option>
                  </SelectInput>
                </Labelled>
                <Labelled label="Note">
                  <TextInput name="notes" />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}

          {out && proof ? (
            <Panel title="Proof of delivery" note={methods.length ? `Approved methods: ${methods.map((m) => PROOF_LABELS[m] ?? m).join(", ")}.` : undefined}>
              {methods.length === 0 ? (
                <Notice tone="warning" title="No proof policy" items={["No proof-of-delivery method is approved, so deliveries cannot be completed yet."]} />
              ) : hasProof ? (
                <p className="type-body-sm font-semibold text-success">Accepted proof is recorded for this attempt.</p>
              ) : (
                <KForm action={deliveryUpdate} submit="Record proof" pending="Checking…" label="Record proof" size="lg">
                  <Hidden values={{ id: d.id, action: "proof" }} />
                  <RequestKey />
                  <Labelled label="Method">
                    <SelectInput name="method">
                      {methods.map((m) => (
                        <option key={m} value={m}>
                          {PROOF_LABELS[m] ?? humanize(m)}
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Customer's delivery code" hint="For the code method: ask the customer; it is in their Mise account.">
                    <TextInput name="code" inputMode="numeric" autoComplete="one-time-code" className="font-mono text-[1.25rem]" />
                  </Labelled>
                  <Labelled label="Received by (name)">
                    <TextInput name="recipient_name" />
                  </Labelled>
                  <Labelled label="Partner reference">
                    <TextInput name="partner_reference" />
                  </Labelled>
                  <Labelled label="How you confirmed (staff method)">
                    <TextInput name="notes" />
                  </Labelled>
                </KForm>
              )}
            </Panel>
          ) : null}

          {out && confirm ? (
            <Panel title="Outcome">
              <div className="grid gap-5">
                <KForm action={deliveryUpdate} submit="Mark delivered" pending="Saving…" label="Mark delivered" size="lg">
                  <Hidden values={{ id: d.id, action: "deliver" }} />
                  <RequestKey />
                  {!hasProof ? <p className="type-caption text-muted">Needs accepted proof first.</p> : null}
                </KForm>
                <KForm action={deliveryUpdate} submit="Record failed attempt" pending="Saving…" label="Record a failed attempt" variant="secondary" size="lg">
                  <Hidden values={{ id: d.id, action: "fail" }} />
                  <RequestKey />
                  <Labelled label="Reason">
                    <SelectInput name="reason">
                      {(settings?.failure_reasons ?? []).map((r) => (
                        <option key={r} value={r}>
                          {humanize(r)}
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <Labelled label="What happened">
                    <TextArea name="notes" rows={2} />
                  </Labelled>
                </KForm>
              </div>
            </Panel>
          ) : null}
          {out ? <OnlineOnly /> : null}

          {d.status === "delivery_failed" && (can(admin, "delivery.reschedule") || can(admin, "delivery_exceptions.manage")) ? (
            <Panel title="Next step after a failed attempt" note="Kits that come back are quarantined for review; they never go back into stock.">
              <div className="grid gap-5">
                {can(admin, "delivery.reschedule") ? (
                  <KForm action={deliveryUpdate} submit="Plan a new attempt" pending="Saving…" label="Reattempt">
                    <Hidden values={{ id: d.id, action: "reattempt" }} />
                    <Labelled label="Reason">
                      <TextInput name="reason" />
                    </Labelled>
                  </KForm>
                ) : null}
                {can(admin, "delivery_exceptions.manage") ? (
                  <KForm action={deliveryUpdate} submit="Record return" pending="Saving…" label="Record return" variant="secondary">
                    <Hidden values={{ id: d.id, action: "return" }} />
                    <Labelled label="Reason">
                      <TextInput name="reason" />
                    </Labelled>
                    <Labelled label="Condition">
                      <SelectInput name="condition">
                        <option value="intact">Intact</option>
                        <option value="damaged">Damaged</option>
                        <option value="temperature_concern">Temperature concern</option>
                        <option value="unknown">Unknown</option>
                      </SelectInput>
                    </Labelled>
                  </KForm>
                ) : null}
              </div>
            </Panel>
          ) : null}

          {["pending_assignment", "assigned"].includes(d.status) && can(admin, "delivery.plan") ? (
            <Panel title="Correct the address" note="Only before dispatch, with a reason. The previous address stays in the history.">
              <KForm action={correctAddress} submit="Save address" pending="Saving…" label="Correct address">
                <Hidden values={{ id: d.id, region: d.address.region }} />
                <div className="grid gap-3 sm:grid-cols-2">
                  <Labelled label="Recipient">
                    <TextInput name="recipient_name" defaultValue={d.address.recipient_name} />
                  </Labelled>
                  <Labelled label="Phone">
                    <TextInput name="phone" defaultValue={d.address.phone ?? ""} />
                  </Labelled>
                  <Labelled label="Street">
                    <TextInput name="line1" defaultValue={d.address.line1} />
                  </Labelled>
                  <Labelled label="Line 2">
                    <TextInput name="line2" defaultValue={d.address.line2 ?? ""} />
                  </Labelled>
                  <Labelled label="City">
                    <TextInput name="city" defaultValue={d.address.city} />
                  </Labelled>
                  <Labelled label="Instructions">
                    <TextInput name="instructions" defaultValue={d.address.instructions ?? ""} />
                  </Labelled>
                </div>
                <Labelled label="Reason for the change">
                  <TextInput name="reason" />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}

          {["pending_assignment", "assigned", "ready_for_pickup"].includes(d.status) && can(admin, "delivery.dispatch") ? (
            <Panel title="Cancel delivery" note="The order and payment are not changed; any refund goes through payments.">
              <KForm action={deliveryUpdate} submit="Cancel delivery" pending="Cancelling…" label="Cancel delivery" variant="secondary">
                <Hidden values={{ id: d.id, action: "cancel" }} />
                <Labelled label="Reason">
                  <TextInput name="reason" />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}

          {["delivered", "delivery_failed"].includes(d.status) && can(admin, "delivery_settings.manage") ? (
            <Panel title="Correct a recorded outcome" note="Only for an outcome recorded by mistake. Explicit, reasoned and kept in the history.">
              <KForm action={deliveryUpdate} submit="Record correction" pending="Saving…" label="Correct outcome" variant="secondary">
                <Hidden values={{ id: d.id, action: "correct", to: d.status === "delivered" ? "delivery_failed" : "in_transit" }} />
                <p className="type-body-sm">
                  Change “{DELIVERY_LABELS[d.status]}” to “{d.status === "delivered" ? "Failed" : "In transit"}”.
                </p>
                <Labelled label="Reason">
                  <TextArea name="reason" rows={2} />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}
        </div>

        <div className="grid content-start gap-6 [&>*]:min-w-0">
          <Panel title="Tracking history" note="Append-only. Each entry says who reported it.">
            <Timeline
              items={(d.events ?? [])
                .slice()
                .reverse()
                .map((e) => ({
                  at: e.occurred_at,
                  title: (e.to ? DELIVERY_LABELS[e.to] ?? e.to : humanize(e.type)) + (e.type === "correction" ? " (correction)" : ""),
                  detail: [SOURCES[e.source], e.actor_name, e.notes, e.location ? `at ${e.location} (verified)` : null, e.attempt > 1 ? `attempt ${e.attempt}` : null]
                    .filter(Boolean)
                    .join(" · "),
                }))}
            />
          </Panel>
          {d.proofs ? (
            <Panel title="Proof records">
              {d.proofs.length ? (
                <ul className="type-body-sm grid gap-1">
                  {d.proofs.map((p) => (
                    <li key={p.id}>
                      {formatDateTime(p.created_at)} · attempt {p.attempt} · {PROOF_LABELS[p.method] ?? p.method} · <StatusTag status={p.status} />
                      {p.rejection_reason ? ` · ${p.rejection_reason}` : ""}
                      {p.recipient_name ? ` · ${p.recipient_name}` : ""}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="type-body-sm text-muted">None yet.</p>
              )}
            </Panel>
          ) : null}
          {d.run_id ? (
            <p className="type-body-sm">
              Run{" "}
              <Link href={`/delivery/runs/${d.run_id}`} className="font-semibold underline underline-offset-4">
                {d.run_number}
              </Link>{" "}
              · {d.partner_name} · stop {d.stop_sequence}
            </p>
          ) : null}
          {can(admin, "delivery.read") ? (
            <>
              <Panel title="Customer messages" note="“Sent” only when the mail provider accepted it.">
                {d.notifications?.length ? (
                  <ul className="type-body-sm grid gap-1">
                    {d.notifications.map((n, i) => (
                      <li key={i}>
                        {humanize(n.kind)} · <StatusTag status={n.status} /> {n.reason ? `· ${n.reason}` : ""}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="type-body-sm text-muted">None.</p>
                )}
              </Panel>
              <Panel title="Requests, returns and support">
                <ul className="type-body-sm grid gap-1">
                  {(d.reschedule_requests ?? []).map((r) => (
                    <li key={r.id}>
                      Reschedule ({r.requested_by}) · {r.status} · “{r.reason}”{r.new_date ? ` → ${formatDay(r.new_date)}` : ""}
                    </li>
                  ))}
                  {(d.returns ?? []).map((r) => (
                    <li key={r.id}>
                      Return · {r.condition} · {r.disposition} · {r.status.replace(/_/g, " ")}
                    </li>
                  ))}
                  {(d.support_cases ?? []).map((c) => (
                    <li key={c.case_id}>
                      <Link href={`/support/${c.case_id}`} className="underline underline-offset-4">
                        Case #{c.number}
                      </Link>{" "}
                      · {c.subject} · {c.status}
                    </li>
                  ))}
                  {!(d.reschedule_requests?.length || d.returns?.length || d.support_cases?.length) ? <li className="text-muted">Nothing.</li> : null}
                </ul>
                <p className="type-body-sm mt-3">
                  <Link href={`/delivery/exceptions?delivery_id=${d.id}`} className="underline underline-offset-4">
                    Exceptions for this delivery ({d.open_exceptions} open)
                  </Link>
                  {" · "}
                  <Link href={`/customers/${d.customer_id}`} className="underline underline-offset-4">
                    Customer
                  </Link>
                </p>
              </Panel>
            </>
          ) : null}
        </div>
      </div>
    </>
  );
}
