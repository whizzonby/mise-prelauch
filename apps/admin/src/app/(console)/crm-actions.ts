"use server";

import { ApiError, type SegmentRules } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "./meals/actions";

/*
 * Sprint 4 actions: notes, lead stages, support cases, staff subscription
 * actions, segments, campaigns, staff, settings, exports and alerts. The
 * API checks every permission again; these only pass the form along.
 */

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const all = (form: FormData, key: string) => form.getAll(key).map(String).filter(Boolean);

function failure(error: unknown): CatalogState {
  if (error instanceof ApiError) {
    if (error.code === "validation_failed") return { error: "Some fields need attention.", fields: error.fields };
    if (error.status === 403) return { error: "Your role does not allow this." };
    return { error: error.message };
  }
  return { error: "The Mise API could not be reached. Try again in a moment." };
}

async function run(fn: (token: string) => Promise<unknown>, paths: string[], message: string): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await fn(token);
  } catch (e) {
    return failure(e);
  }
  for (const p of paths) revalidatePath(p);
  return { message };
}

export async function addNote(_: CatalogState, form: FormData): Promise<CatalogState> {
  const subject = text(form, "subject") === "lead" ? "leads" : "customers";
  const id = text(form, "id");
  return run((t) => api.admin.addNote(t, subject, id, text(form, "body")), [`/${subject}/${id}`], "Note added.");
}

export async function setLeadCRM(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run((t) => api.admin.setLeadCRM(t, id, text(form, "stage"), text(form, "assigned_to") || null), [`/leads/${id}`, "/leads"], "Saved.");
}

export async function createCase(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    const c = await api.admin.createCase(token, {
      customer_id: text(form, "customer_id"),
      lead_id: text(form, "lead_id"),
      subject: text(form, "subject"),
      category: text(form, "category"),
      priority: text(form, "priority") || "normal",
      assigned_to: text(form, "assigned_to"),
      note: text(form, "note"),
    });
    id = c.id;
  } catch (e) {
    return failure(e);
  }
  redirect(`/support/${id}`);
}

export async function updateCase(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run(
    (t) =>
      api.admin.updateCase(t, id, {
        status: text(form, "status") || undefined,
        priority: text(form, "priority") || undefined,
        assigned_to: form.has("assigned_to") ? text(form, "assigned_to") : undefined,
        resolution: text(form, "resolution") || undefined,
      }),
    [`/support/${id}`, "/support"],
    "Case updated.",
  );
}

export async function caseNote(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run((t) => api.admin.caseNote(t, id, text(form, "body")), [`/support/${id}`], "Note added.");
}

export async function subscriptionAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action") as "pause" | "resume" | "cancel";
  return run(
    (t) =>
      api.admin.subscriptionAction(t, id, action, {
        reason: text(form, "reason") as "customer_request",
        note: text(form, "note"),
        weeks: Number(text(form, "weeks")) || undefined,
        cancel_reason: text(form, "cancel_reason") || undefined,
      }),
    [`/subscriptions/${id}`],
    "Done. The customer has been emailed as usual.",
  );
}

function rulesFrom(form: FormData): SegmentRules {
  const r: SegmentRules = {};
  const lists = ["plan_ids", "subscription_statuses", "lifecycle", "regions", "dietary", "sources"] as const;
  for (const k of lists) {
    const v = k === "sources" || k === "regions" || k === "dietary" ? text(form, k).split(",").map((s) => s.trim().toLowerCase()).filter(Boolean) : all(form, k);
    if (v.length) (r as Record<string, unknown>)[k] = v;
  }
  if (text(form, "joined_from")) r.joined_from = text(form, "joined_from");
  if (text(form, "joined_to")) r.joined_to = text(form, "joined_to");
  if (text(form, "min_orders")) r.min_orders = Number(text(form, "min_orders"));
  if (text(form, "min_tenure_weeks")) r.min_tenure_weeks = Number(text(form, "min_tenure_weeks"));
  if (text(form, "consent_topic")) r.consent_topic = text(form, "consent_topic");
  return r;
}

export async function saveSegment(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id") || undefined;
  let saved: string;
  try {
    saved = (await api.admin.saveSegment(token, { name: text(form, "name"), description: text(form, "description"), rules: rulesFrom(form) }, id)).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/segments");
  redirect(`/segments/${saved}`);
}

export async function deleteSegment(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await api.admin.deleteSegment(token, text(form, "id"));
  } catch (e) {
    return failure(e);
  }
  redirect("/segments");
}

function localInstant(raw: string): string | null {
  return raw ? new Date(`${raw}:00-04:00`).toISOString() : null;
}

export async function saveCampaign(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const id = text(form, "id") || undefined;
  let saved: string;
  try {
    saved = (
      await api.admin.saveCampaign(
        token,
        {
          name: text(form, "name"),
          objective: text(form, "objective") as "acquisition",
          channel: text(form, "channel") as "email",
          utm_campaign: text(form, "utm_campaign"),
          segment_id: text(form, "segment_id"),
          promotion_id: text(form, "promotion_id"),
          starts_at: localInstant(text(form, "starts_at")),
          ends_at: localInstant(text(form, "ends_at")),
          notes: text(form, "notes"),
        },
        id,
      )
    ).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/campaigns");
  redirect(`/campaigns/${saved}`);
}

export async function campaignStatus(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run((t) => api.admin.setCampaignStatus(t, id, text(form, "status")), [`/campaigns/${id}`, "/campaigns"], "Status changed.");
}

export async function invite(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.admin.invite(t, { email: text(form, "email"), name: text(form, "name"), role: text(form, "role") }), ["/staff"],
    "Invitation sent. The link works once, for 7 days.");
}

export async function revokeInvitation(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.admin.revokeInvitation(t, text(form, "id")), ["/staff"], "Invitation revoked.");
}

export async function staffAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const action = text(form, "action") as "roles" | "roles/revoke" | "disable" | "enable" | "signout";
  return run((t) => api.admin.staffAction(t, text(form, "id"), action, { role: text(form, "role") || undefined, reason: text(form, "reason") }),
    ["/staff"], "Done, and recorded in the audit log.");
}

export async function saveSettings(_: CatalogState, form: FormData): Promise<CatalogState> {
  const values: Record<string, string> = {};
  for (const [k, v] of form.entries()) if (k.startsWith("business.")) values[k] = String(v);
  return run((t) => api.admin.putSettings(t, values), ["/settings"], "Settings saved.");
}

export async function requestExport(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.admin.requestExport(t, {
        kind: text(form, "kind") as "customers",
        from: text(form, "from") || undefined,
        to: text(form, "to") || undefined,
        status: text(form, "status") || undefined,
        segment_id: text(form, "segment_id") || undefined,
      }),
    ["/exports"],
    "Export requested. It will be ready here in a moment; refresh the page.",
  );
}

export async function readAllAlerts(): Promise<void> {
  const { token } = await requireAdmin();
  await api.admin.readAlerts(token, []).catch(() => null);
  revalidatePath("/", "layout");
}
