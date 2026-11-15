import type { DeliveryAddress } from "@mise/api-client";
import Link from "next/link";
import type { ReactNode } from "react";

import { StatusTag } from "@/components/admin-ui";

/*
 * Sprint 7 building blocks for fulfillment and delivery pages: a delivery
 * status tag with plain labels, the address card, the online-only notice for
 * phone workflows, and the option lists the forms share.
 */

export const DELIVERY_LABELS: Record<string, string> = {
  pending_assignment: "Waiting for a run",
  assigned: "Assigned",
  ready_for_pickup: "Ready for pickup",
  picked_up: "Picked up",
  in_transit: "In transit",
  arriving: "Arriving",
  delivered: "Delivered",
  delivery_failed: "Failed",
  returned: "Returned",
  cancelled: "Cancelled",
};

export const FULFILLMENT_LABELS: Record<string, string> = {
  ready_for_verification: "To verify",
  verification_in_progress: "Verifying",
  verified: "Verified",
  ready_for_dispatch: "Released to dispatch",
  handed_over: "With partner",
  fulfilled: "Fulfilled",
  exception: "Exception",
  cancelled: "Cancelled",
};

export function DeliveryStatus({ status }: { status: string }) {
  return <StatusTag status={status} label={DELIVERY_LABELS[status] ?? status} />;
}

export function FulfillmentStatus({ status }: { status: string }) {
  return <StatusTag status={status} label={FULFILLMENT_LABELS[status] ?? status} />;
}

export const EXCEPTION_TYPES = ["missing_package", "incorrect_package", "address_problem", "capacity_shortage", "partner_cancellation", "late_handoff",
  "handoff_dispute", "delivery_failure", "missing_proof", "damaged_package", "customer_complaint", "other"];
export const SEVERITIES = ["low", "medium", "high", "critical"];
export const NEXT_ACTIONS = ["none", "reattempt", "reschedule", "return", "escalate", "refund_review"];
export const PROOF_LABELS: Record<string, string> = {
  confirmation_code: "Customer's delivery code",
  recipient_acknowledgment: "Recipient named who received it",
  staff_confirmation: "Mise staff confirmation",
  partner_confirmation: "Partner's confirmation reference",
};
export const CRITERIA_LABELS: Record<string, string> = {
  contract: "Signed contract on file",
  service_coverage: "Covers the zones assigned",
  handling_capability: "Can handle meal kits as required",
  reliability: "Reliability shown (trial or references)",
  escalation_process: "Escalation contact and process agreed",
  delivery_evidence: "Can provide the required proof of delivery",
  handling_procedures: "Follows Mise's handling procedures",
};

export function AddressCard({ address, validation }: { address: DeliveryAddress; validation?: string }) {
  return (
    <address className="type-body-sm not-italic">
      <span className="block font-semibold">{address.recipient_name}</span>
      <span className="block">{address.line1}</span>
      {address.line2 ? <span className="block">{address.line2}</span> : null}
      <span className="block">
        {[address.area, address.city].filter(Boolean).join(", ")} · {address.region_label || address.region}
      </span>
      {address.phone ? (
        <a href={`tel:${address.phone}`} className="mt-1 block font-semibold underline underline-offset-4">
          {address.phone}
        </a>
      ) : null}
      {address.instructions ? <span className="mt-2 block border-l-2 border-accent pl-2">{address.instructions}</span> : null}
      {validation && validation !== "serviceable" ? (
        <span className="mt-2 block">
          <StatusTag status={validation} label={validation === "unzoned" ? "No zone covers this region" : validation === "not_serviceable" ? "Outside delivery area" : "Coverage unknown"} />
        </span>
      ) : null}
    </address>
  );
}

/** Phone workflows need a connection: say so plainly (forms keep what was typed if a save fails). */
export function OnlineOnly() {
  return (
    <p className="type-caption border-l-2 border-border-strong pl-3 text-muted">
      Needs an internet connection. If a save fails, what you entered stays on the screen. Try again when you have signal.
    </p>
  );
}

export function Hidden({ values }: { values: Record<string, string | number | undefined | null> }) {
  return (
    <>
      {Object.entries(values).map(([k, v]) => (v === undefined || v === null ? null : <input key={k} type="hidden" name={k} value={String(v)} />))}
    </>
  );
}

/** A fresh request key per render, so a retried submit is one record. */
export function RequestKey() {
  return <input type="hidden" name="idempotency_key" value={crypto.randomUUID()} />;
}

export function CountTile({ label, value, href, tone }: { label: string; value: number; href?: string; tone?: "warn" | "bad" }) {
  const body = (
    <>
      <span className="type-caption block text-muted">{label}</span>
      <span className={`mt-1 block font-display text-[2rem] leading-none ${value > 0 && tone === "bad" ? "text-error" : ""}`}>{value}</span>
    </>
  );
  const cls = `block border bg-surface p-4 ${value > 0 && tone ? "border-l-4 " + (tone === "bad" ? "border-error" : "border-accent") : "border-border"}`;
  return href ? (
    <Link href={href} className={`${cls} hover:border-border-strong`}>
      {body}
    </Link>
  ) : (
    <div className={cls}>{body}</div>
  );
}

export function Section({ title, children, note }: { title: string; children: ReactNode; note?: ReactNode }) {
  return (
    <section className="grid gap-3">
      <h2 className="font-display text-[1.375rem]">{title}</h2>
      {note ? <p className="type-body-sm -mt-2 text-muted">{note}</p> : null}
      {children}
    </section>
  );
}
