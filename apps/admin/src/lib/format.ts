/** Turns a stored option value ("san-fernando-south") into a readable label. */
export function humanize(value: string): string {
  const text = value.replace(/[-_]+/g, " ").trim();
  return text.charAt(0).toUpperCase() + text.slice(1);
}

const TIME_ZONE = "America/Port_of_Spain";
const dateTime = new Intl.DateTimeFormat("en-GB", { dateStyle: "medium", timeStyle: "short", timeZone: TIME_ZONE });
const dateOnly = new Intl.DateTimeFormat("en-GB", { dateStyle: "medium", timeZone: TIME_ZONE });
const isoDay = new Intl.DateTimeFormat("en-CA", { year: "numeric", month: "2-digit", day: "2-digit", timeZone: TIME_ZONE });

/**
 * The calendar date (YYYY-MM-DD) in Trinidad and Tobago at an instant, now by
 * default. Never toISOString().slice(0, 10): that is the UTC date, which is
 * already tomorrow from 8 pm local time.
 */
export function businessDay(at: Date = new Date()): string {
  return isoDay.format(at);
}
const shortDate = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", timeZone: "UTC" });

/** Timestamps are shown in Trinidad & Tobago time, where the team works. */
export function formatDateTime(iso: string): string {
  return dateTime.format(new Date(iso));
}

export function formatDate(iso: string): string {
  return dateOnly.format(new Date(iso));
}

/** Formats a calendar day (YYYY-MM-DD) without shifting it across time zones. */
export function formatDay(day: string): string {
  return shortDate.format(new Date(`${day}T00:00:00Z`));
}

export const number = new Intl.NumberFormat("en-GB");

export function percent(part: number, whole: number): string {
  if (whole === 0) return "0%";
  return `${Math.round((part / whole) * 100)}%`;
}

/**
 * Packaging vote labels. Keep in step with packagingOptions in
 * apps/marketing/src/content/waitlist.ts. "none" is a lead who did not answer.
 */
export const PACKAGING_LABELS: Record<string, string> = {
  compostable: "Compostable",
  paper: "Paper and card",
  reusable: "Reusable, collected",
  "no-preference": "No preference",
  none: "Did not answer",
};

export function packagingLabel(value: string): string {
  return PACKAGING_LABELS[value] ?? humanize(value);
}

export const LEAD_STATUSES = ["PENDING", "VERIFIED", "QUALIFIED", "CONVERTED", "UNSUBSCRIBED", "BLOCKED"] as const;

export function statusTone(status: string): "neutral" | "success" | "warning" | "error" | "accent" {
  switch (status) {
    case "VERIFIED":
    case "CONVERTED":
    case "converted":
      return "success";
    case "QUALIFIED":
      return "accent";
    case "PENDING":
    case "pending":
      return "warning";
    case "BLOCKED":
    case "flagged":
      return "error";
    default:
      return "neutral";
  }
}

/** Rounds a maximum up to a clean axis top: 4, 8, 20, 40, 100… */
export function niceMax(value: number): number {
  if (value <= 4) return 4;
  const magnitude = 10 ** Math.floor(Math.log10(value));
  for (const step of [1, 2, 4, 5, 10]) {
    if (value <= step * magnitude) return step * magnitude;
  }
  return 10 * magnitude;
}

/** A lead's name as written. Leads who joined before the form asked for a last name have none. */
export function fullName(lead: { first_name: string; last_name: string }): string {
  return [lead.first_name, lead.last_name].filter(Boolean).join(" ");
}

/** Minor units to "TTD 330.00". */
export function money(minor: number, currency = "TTD"): string {
  const sign = minor < 0 ? "-" : "";
  const abs = Math.abs(minor);
  return `${sign}${currency} ${Math.floor(abs / 100).toLocaleString("en-US")}.${String(abs % 100).padStart(2, "0")}`;
}

/** An instant as the value of a datetime-local input, in Trinidad and Tobago time. */
export function localInputValue(iso: string | null | undefined): string {
  if (!iso) return "";
  const d = new Date(new Date(iso).getTime() - 4 * 3600 * 1000);
  return d.toISOString().slice(0, 16);
}

export function commerceTone(status: string): "neutral" | "success" | "warning" | "error" | "accent" {
  switch (status) {
    // Sprint 7: fulfillment and delivery.
    case "verified":
    case "ready_for_dispatch":
    case "handed_over":
    case "fulfilled":
    case "delivered":
    case "serviceable":
    case "sent":
    case "reconciled":
    case "accepted":
    case "resolved":
      return "success";
    case "ready_for_verification":
    case "verification_in_progress":
    case "assigned":
    case "ready_for_pickup":
    case "picked_up":
    case "in_transit":
    case "arriving":
    case "queued":
    case "requested":
    case "configured":
      return "accent";
    case "exception":
    case "delivery_failed":
    case "discrepancy":
    case "not_serviceable":
    case "critical":
    case "high":
    case "missing":
    // Sprint 9: launch gates.
    case "blocked":
      return "error";
    case "pending_assignment":
    case "unzoned":
    case "returned":
    case "quarantined":
    case "awaiting_review":
    case "skipped":
    case "medium":
    case "warning":
    case "awaiting_approval":
    case "refused":
      return "warning";
    case "active":
    case "published":
    case "paid":
    case "succeeded":
    case "confirmed":
    case "finalized":
    case "processed":
    case "approved":
    case "released":
    case "completed":
    case "passed":
    case "pass":
    case "done":
    case "ok":
    case "received":
    case "available":
    case "recorded":
    case "none_applies":
    case "issued_stock":
      return "success";
    case "submitted":
    case "under_review":
    case "in_progress":
    case "awaiting_quality_check":
    case "current":
    case "pending_review":
    case "pending_approval":
    case "issued":
    case "partially_received":
    case "proposed":
      return "accent";
    case "quality_hold":
    case "on_hold":
    case "blocked":
    case "fail":
    case "changes_requested":
    case "unconvertible":
    case "inactive_ingredient":
    case "open":
      return "error";
    case "past_due":
    case "rejected":
    case "suspended":
    case "expired":
    case "quarantined":
    case "not_recorded":
    case "failed":
    case "needs_resolution":
    case "unpaid":
      return "error";
    case "draft":
    case "pending_payment":
    case "scheduled":
    case "due":
    case "pending":
    case "paused":
    case "pause_scheduled":
    case "cancel_scheduled":
    case "partially_refunded":
    case "planned":
    case "todo":
      return "warning";
    default:
      return "neutral";
  }
}

/**
 * An exact decimal quantity from the API ("1875", "0.5") with its unit, made
 * readable: grams over 1,000 also show kilograms. Never rounded silently
 * beyond 3 decimals (the API already rounds once).
 */
export function quantity(value: string | null | undefined, unit: string): string {
  if (value == null) return "Not calculable";
  const n = Number(value);
  const fmt = new Intl.NumberFormat("en-GB", { maximumFractionDigits: 3 });
  const main = `${fmt.format(n)} ${unit}`;
  if (unit === "g" && n >= 1000) return `${main} (${fmt.format(n / 1000)} kg)`;
  if (unit === "ml" && n >= 1000) return `${main} (${fmt.format(n / 1000)} l)`;
  return main;
}

export const WEEKDAYS = ["", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
