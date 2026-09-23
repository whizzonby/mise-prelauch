/** Turns a stored option value ("san-fernando-south") into a readable label. */
export function humanize(value: string): string {
  const text = value.replace(/[-_]+/g, " ").trim();
  return text.charAt(0).toUpperCase() + text.slice(1);
}

const TIME_ZONE = "America/Port_of_Spain";
const dateTime = new Intl.DateTimeFormat("en-GB", { dateStyle: "medium", timeStyle: "short", timeZone: TIME_ZONE });
const dateOnly = new Intl.DateTimeFormat("en-GB", { dateStyle: "medium", timeZone: TIME_ZONE });
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
