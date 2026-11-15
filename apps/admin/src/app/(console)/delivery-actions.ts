"use server";

import { ApiError } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "./meals/actions";

/*
 * Sprint 7 actions: fulfillment, delivery configuration, dispatch, custody,
 * tracking, proof, exceptions, returns and reconciliation. The API checks
 * every permission and rule again (state machine, capacity, proof policy,
 * courier scope); these only pass the form along and report the outcome.
 * Each form carries its own idempotency key, so a double tap is one record.
 */

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const opt = (form: FormData, key: string) => (text(form, key) === "" ? undefined : text(form, key));
const all = (form: FormData, key: string) => form.getAll(key).map(String).map((v) => v.trim()).filter(Boolean);
const flag = (form: FormData, key: string) => form.get(key) === "on" || form.get(key) === "true";
const int = (form: FormData, key: string) => {
  const v = text(form, key);
  return v === "" ? undefined : Number.parseInt(v, 10);
};
const cents = (form: FormData, key: string) => {
  const v = text(form, key).replace(/[,\s$]/g, "");
  if (v === "") return undefined;
  const n = Number(v);
  return Number.isFinite(n) ? Math.round(n * 100) : Number.NaN;
};
/** A key per form render: a retried submit of the same form is one record. */
const key = (form: FormData) => text(form, "idempotency_key") || crypto.randomUUID();

function failure(error: unknown): CatalogState {
  if (error instanceof ApiError) {
    if (error.code === "validation_failed") return { error: "Some fields need attention.", fields: error.fields };
    if (error.status === 403) return { error: error.code === "forbidden" ? "Your role does not allow this." : error.message };
    return { error: error.message };
  }
  return { error: "The Mise API could not be reached. Check your connection and try again; what you typed is kept." };
}

async function run(fn: (token: string) => Promise<unknown>, paths: string[], message: string): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await fn(token);
  } catch (e) {
    return failure(e);
  }
  for (const p of paths) revalidatePath(p, "layout");
  return { message };
}

// ---------------------------------------------------------------- fulfillment

export async function verifyItem(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run(
    (t) =>
      api.delivery.verify(t, id, {
        item_id: text(form, "item_id"),
        counted_quantity: int(form, "counted_quantity") ?? -1,
        scanned_batch_code: text(form, "scanned_batch_code"),
        recipe_confirmed: flag(form, "recipe_confirmed"),
        packaging_ok: flag(form, "packaging_ok"),
        label_ok: flag(form, "label_ok"),
        discrepancy: text(form, "discrepancy"),
        idempotency_key: key(form),
      }),
    ["/fulfillment"],
    "Check recorded.",
  );
}

export async function releaseFulfillment(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.delivery.releaseFulfillment(t, text(form, "id"), int(form, "version")), ["/fulfillment", "/delivery"], "Released to dispatch.");
}

export async function syncFulfillment(): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    const r = await api.delivery.syncFulfillment(token);
    revalidatePath("/fulfillment");
    return { message: r.items.length ? `${r.items.length} order(s) evaluated.` : "Every ready order already has a fulfillment." };
  } catch (e) {
    return failure(e);
  }
}

export async function flagBatch(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.delivery.flagBatch(t, text(form, "batch_id"), text(form, "reason")), ["/fulfillment", "/delivery/exceptions"], "Batch flagged; affected orders are blocked.");
}

// ---------------------------------------------------------------- configuration

export async function saveDeliverySettings(_: CatalogState, form: FormData): Promise<CatalogState> {
  const hours = int(form, "reschedule_cutoff_hours");
  const lo = int(form, "delivery_offset_min_days");
  const hi = int(form, "delivery_offset_max_days");
  return run(
    (t) =>
      api.delivery.saveSettings(t, {
        proof_methods: all(form, "proof_methods"),
        proof_policy_ref: text(form, "proof_policy_ref"),
        ...(hours === undefined ? { clear_reschedule_policy: true } : { reschedule_cutoff_hours: hours }),
        failure_reasons: text(form, "failure_reasons").split(/[\n,]/).map((s) => s.trim()).filter(Boolean),
        allow_unapproved_partners: flag(form, "allow_unapproved_partners"),
        unapproved_partner_policy_ref: text(form, "unapproved_partner_policy_ref"),
        label_certified: flag(form, "label_certified"),
        customer_emails_approved: flag(form, "customer_emails_approved"),
        ...(lo === undefined && hi === undefined
          ? { clear_delivery_week_rule: true }
          : { delivery_offset_min_days: lo, delivery_offset_max_days: hi, delivery_week_rule_ref: text(form, "delivery_week_rule_ref") }),
      }),
    ["/delivery"],
    "Settings saved.",
  );
}

export async function saveZone(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.delivery.saveZone(t, opt(form, "id") ?? null, {
        code: text(form, "code"),
        name: text(form, "name"),
        status: text(form, "status") || "draft",
        fee_policy_ref: text(form, "fee_policy_ref"),
        notes: text(form, "notes"),
        regions: all(form, "regions"),
      }),
    ["/delivery/zones"],
    "Zone saved.",
  );
}

export async function saveServiceDay(_: CatalogState, form: FormData): Promise<CatalogState> {
  const labels = all(form, "window_label");
  const starts = form.getAll("window_start").map(String);
  const ends = form.getAll("window_end").map(String);
  const approved = form.getAll("window_approved").map(String);
  const windows = labels.map((label, i) => ({ label, starts_at: starts[i] ?? "", ends_at: ends[i] ?? "", status: approved.includes(String(i)) ? "approved" : "draft" }));
  return run(
    (t) =>
      api.delivery.saveServiceDay(t, {
        date: text(form, "date"),
        zone_id: text(form, "zone_id"),
        status: text(form, "status") as "open" | "closed",
        notes: text(form, "notes"),
        windows,
      }),
    ["/delivery/calendar", "/delivery/planning"],
    "Service day saved.",
  );
}

export async function setCapacity(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.delivery.setCapacity(t, {
        date: text(form, "date"),
        zone_id: text(form, "zone_id"),
        partner_id: text(form, "partner_id"),
        capacity: int(form, "capacity") ?? -1,
        notes: text(form, "notes"),
      }),
    ["/delivery/calendar", "/delivery/planning"],
    "Capacity saved.",
  );
}

export async function savePartner(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = opt(form, "id") ?? null;
  const { token } = await requireAdmin();
  let saved: string;
  try {
    const p = await api.delivery.savePartner(token, id, {
      name: text(form, "name"),
      contact_name: text(form, "contact_name"),
      phone: text(form, "phone"),
      email: text(form, "email"),
      escalation_contact: text(form, "escalation_contact"),
      integration_mode: text(form, "integration_mode") || "manual",
      agreement_status: text(form, "agreement_status") || "none",
      agreement_ref: text(form, "agreement_ref"),
      supported_windows: text(form, "supported_windows"),
      capacity_note: text(form, "capacity_note"),
      billing_reference: text(form, "billing_reference"),
      notes: text(form, "notes"),
      zone_ids: all(form, "zone_ids"),
      version: int(form, "version"),
    });
    saved = p.id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/delivery/partners", "layout");
  if (!id) redirect(`/delivery/partners/${saved}`);
  return { message: "Partner saved." };
}

export async function partnerCriterion(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) => api.delivery.partnerCriterion(t, text(form, "id"), { criterion: text(form, "criterion"), met: text(form, "met") === "yes", evidence: text(form, "evidence") }),
    ["/delivery/partners"],
    "Criterion recorded.",
  );
}

export async function partnerStatus(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) => api.delivery.partnerStatus(t, text(form, "id"), { status: text(form, "status") as "approved", reason: text(form, "reason") }),
    ["/delivery/partners"],
    "Status changed.",
  );
}

export async function saveCourier(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.delivery.saveCourier(t, opt(form, "id") ?? null, {
        partner_id: text(form, "partner_id"),
        name: text(form, "name"),
        phone: text(form, "phone"),
        user_email: text(form, "user_email"),
        zone_ids: all(form, "zone_ids"),
        status: text(form, "status") || "active",
        notes: text(form, "notes"),
      }),
    ["/delivery/couriers"],
    "Courier saved.",
  );
}

export async function recordCost(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.delivery.recordCost(t, {
        partner_id: text(form, "partner_id"),
        run_id: text(form, "run_id"),
        delivery_id: text(form, "delivery_id"),
        kind: text(form, "kind"),
        amount_minor: cents(form, "amount") ?? Number.NaN,
        currency: "TTD",
        rate_reference: text(form, "rate_reference"),
        notes: text(form, "notes"),
      }),
    ["/delivery/runs"],
    "Cost recorded.",
  );
}

// ---------------------------------------------------------------- planning and dispatch

export async function scheduleDelivery(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.delivery.deliveryAction(t, text(form, "id"), "schedule", {
        service_date: text(form, "service_date"),
        window_id: text(form, "window_id"),
        zone_id: text(form, "zone_id"),
        reason: text(form, "reason"),
      }),
    ["/delivery"],
    "Scheduled.",
  );
}

export async function createRun(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    const r = await api.delivery.createRun(token, {
      service_date: text(form, "service_date"),
      partner_id: text(form, "partner_id"),
      courier_id: text(form, "courier_id"),
      zone_id: text(form, "zone_id"),
      notes: text(form, "notes"),
      partner_override: text(form, "partner_override"),
    });
    id = r.id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/delivery", "layout");
  redirect(`/delivery/runs/${id}`);
}

export async function addStop(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.delivery.addStop(t, text(form, "run_id"), {
        delivery_id: text(form, "delivery_id"),
        capacity_override: text(form, "capacity_override"),
        partner_override: text(form, "partner_override"),
      }),
    ["/delivery"],
    "Added to the run.",
  );
}

export async function removeStop(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.delivery.removeStop(t, text(form, "run_id"), text(form, "stop_id"), text(form, "reason")), ["/delivery"], "Removed from the run.");
}

export async function moveStop(_: CatalogState, form: FormData): Promise<CatalogState> {
  const order = all(form, "order");
  const i = order.indexOf(text(form, "stop_id"));
  const j = text(form, "direction") === "up" ? i - 1 : i + 1;
  if (i < 0 || j < 0 || j >= order.length) return {};
  [order[i], order[j]] = [order[j]!, order[i]!];
  return run((t) => api.delivery.sequence(t, text(form, "run_id"), order), ["/delivery/runs"], "Order saved.");
}

export async function runAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const action = text(form, "action") as "finalize" | "reopen" | "release";
  const done = { finalize: "Manifest generated.", reopen: "Run back in draft; the manifest was superseded.", release: "Released for pickup." }[action];
  return run((t) => api.delivery.runAction(t, text(form, "run_id"), action, { reason: text(form, "reason") }), ["/delivery"], done);
}

export async function recordHandoff(_: CatalogState, form: FormData): Promise<CatalogState> {
  const packages = all(form, "package").map((id) => ({ package_id: id, outcome: (text(form, `outcome_${id}`) || "accepted") as "accepted", note: text(form, `note_${id}`) }));
  return run(
    (t) =>
      api.delivery.handoff(t, text(form, "run_id"), {
        from_party: text(form, "from_party"),
        to_party: text(form, "to_party"),
        location: text(form, "location"),
        location_verified: flag(form, "location_verified"),
        packages,
        evidence_ref: text(form, "evidence_ref"),
        notes: text(form, "notes"),
        idempotency_key: key(form),
      }),
    ["/delivery"],
    "Handoff recorded.",
  );
}

export async function reconcileRun(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.delivery.reconcile(t, text(form, "run_id"), text(form, "explanation")), ["/delivery"], "Run reconciled.");
}

// ---------------------------------------------------------------- delivery progress

export async function deliveryUpdate(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action");
  const k = key(form);
  const bodies: Record<string, [Parameters<typeof api.delivery.deliveryAction>[2], object, string]> = {
    status: ["events", { status: text(form, "status"), source: text(form, "source") || "staff", notes: text(form, "notes"), location: text(form, "location"),
      location_verified: flag(form, "location_verified"), partner_reference: text(form, "partner_reference"), idempotency_key: k }, "Update recorded."],
    proof: ["proof", { method: text(form, "method"), code: text(form, "code"), recipient_name: text(form, "recipient_name"),
      partner_reference: text(form, "partner_reference"), notes: text(form, "notes"), idempotency_key: k }, "Proof recorded."],
    deliver: ["deliver", { notes: text(form, "notes"), idempotency_key: k }, "Delivery completed."],
    fail: ["fail", { reason: text(form, "reason"), notes: text(form, "notes"), source: text(form, "source") || "staff", idempotency_key: k }, "Failure recorded; an exception was raised."],
    reattempt: ["reattempt", { reason: text(form, "reason") }, "Back to planning as a new attempt."],
    return: ["return", { reason: text(form, "reason"), condition: text(form, "condition") }, "Return recorded; the packages are quarantined."],
    cancel: ["cancel", { reason: text(form, "reason") }, "Delivery cancelled."],
    correct: ["correct", { to: text(form, "to"), reason: text(form, "reason") }, "Correction recorded."],
    reschedule: ["reschedule-requests", { preferred_date: opt(form, "preferred_date"), reason: text(form, "reason") }, "Request recorded."],
    case: ["cases", { case_id: text(form, "case_id") }, "Case linked."],
  };
  const b = bodies[action];
  if (!b) return { error: "Unknown action." };
  return run((t) => api.delivery.deliveryAction(t, id, b[0], b[1]), ["/delivery"], b[2]);
}

export async function correctAddress(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.delivery.correctAddress(t, text(form, "id"), {
        reason: text(form, "reason"),
        address: {
          recipient_name: text(form, "recipient_name"),
          phone: opt(form, "phone") ?? null,
          line1: text(form, "line1"),
          line2: opt(form, "line2") ?? null,
          city: text(form, "city"),
          region: text(form, "region"),
          country: "TT",
          instructions: opt(form, "instructions") ?? null,
        },
      }),
    ["/delivery"],
    "Address corrected.",
  );
}

export async function decideReschedule(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.delivery.decideReschedule(t, text(form, "request_id"), {
        decision: text(form, "decision") as "approve" | "decline",
        new_date: opt(form, "new_date"),
        window_id: text(form, "window_id"),
        note: text(form, "note"),
      }),
    ["/delivery"],
    "Decision recorded.",
  );
}

// ---------------------------------------------------------------- exceptions and returns

export async function raiseException(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.delivery.raiseException(t, {
        type: text(form, "type") as "other",
        severity: text(form, "severity") as "medium",
        stage: text(form, "stage") || "delivery",
        blocking: flag(form, "blocking"),
        summary: text(form, "summary"),
        notes: text(form, "notes"),
        delivery_id: text(form, "delivery_id"),
        obligation_id: text(form, "obligation_id"),
      }),
    ["/delivery/exceptions"],
    "Exception raised.",
  );
}

export async function updateException(_: CatalogState, form: FormData): Promise<CatalogState> {
  const status = opt(form, "status");
  const next = opt(form, "next_action");
  const owner = opt(form, "owner");
  return run(
    (t) =>
      api.delivery.updateException(t, text(form, "id"), {
        ...(status ? { status: status as "resolved" } : {}),
        ...(next ? { next_action: next as "none" } : {}),
        ...(owner ? { owner_user_id: owner } : {}),
        note: text(form, "note"),
        resolution: text(form, "resolution"),
      }),
    ["/delivery/exceptions", "/fulfillment"],
    "Exception updated.",
  );
}

export async function reviewReturn(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) => api.delivery.reviewReturn(t, text(form, "id"), { disposition: text(form, "disposition") as "discarded", note: text(form, "note") }),
    ["/delivery/returns"],
    "Review recorded.",
  );
}
