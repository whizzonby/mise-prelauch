import { formatMoney, type SubscriptionCycle, type SubscriptionStatus } from "@mise/api-client";

import { BUSINESS_TIME_ZONE } from "./greeting";

export { formatMoney };

/** "Thursday 12 November, 11:59 pm" in Trinidad and Tobago time. */
export function deadline(iso: string, timeZone = BUSINESS_TIME_ZONE): string {
  const d = new Date(iso);
  const day = new Intl.DateTimeFormat("en-TT", { weekday: "long", day: "numeric", month: "long", timeZone }).format(d);
  const time = new Intl.DateTimeFormat("en-TT", { hour: "numeric", minute: "2-digit", hour12: true, timeZone })
    .format(d)
    .replace(/\s/g, " ")
    .toLowerCase();
  return `${day}, ${time}`;
}

/** "12 Nov 2026" for a date-only value ("2026-11-12") or an instant. */
export function shortDate(value: string, timeZone = BUSINESS_TIME_ZONE): string {
  const d = /^\d{4}-\d{2}-\d{2}$/.test(value) ? new Date(`${value}T12:00:00Z`) : new Date(value);
  return new Intl.DateTimeFormat("en-TT", { day: "numeric", month: "short", year: "numeric", timeZone: /^\d{4}-\d{2}-\d{2}$/.test(value) ? "UTC" : timeZone }).format(d);
}

/**
 * How long until a deadline, in words: "2 days", "5 hours", "40 minutes".
 * Display only: the server enforces every deadline.
 */
export function timeLeft(iso: string, now: Date): string | null {
  const ms = new Date(iso).getTime() - now.getTime();
  if (ms <= 0) return null;
  const minutes = Math.floor(ms / 60_000);
  if (minutes < 60) return `${Math.max(minutes, 1)} minute${minutes === 1 ? "" : "s"}`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours} hour${hours === 1 ? "" : "s"}`;
  const days = Math.floor(hours / 24);
  return `${days} days`;
}

type Tone = "neutral" | "success" | "warning" | "error" | "accent";

export function statusTone(status: SubscriptionStatus): Tone {
  switch (status) {
    case "active":
      return "success";
    case "past_due":
      return "error";
    case "pause_scheduled":
    case "paused":
    case "cancel_scheduled":
    case "pending_payment":
      return "warning";
    default:
      return "neutral";
  }
}

export function cycleTone(c: SubscriptionCycle): Tone {
  switch (c.state) {
    case "finalized":
      return "success";
    case "unpaid":
    case "needs_resolution":
      return "error";
    case "paused":
    case "cancelled":
      return "neutral";
  }
  if (c.billing?.status === "failed") return "error";
  if (c.billing?.status === "paid") return "success";
  return "accent";
}

/** Where to go to choose meals for a week. */
export function selectHref(week: string): string {
  return `/menu/${week}/select`;
}
