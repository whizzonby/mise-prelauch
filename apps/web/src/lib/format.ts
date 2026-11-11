import type { DeliveryAvailability } from "@mise/api-client";

/** 25 -> "25 min", 70 -> "1 hr 10 min", 120 -> "2 hr". */
export function minutes(total: number): string {
  if (total < 60) return `${total} min`;
  const h = Math.floor(total / 60);
  const m = total % 60;
  return m ? `${h} hr ${m} min` : `${h} hr`;
}

export function servings(n: number): string {
  return n === 1 ? "Serves 1" : `Serves ${n}`;
}

/** What delivery availability means, in words that never promise delivery. */
export function availabilityText(a: DeliveryAvailability): string {
  switch (a) {
    case "available":
      return "Mise delivers here.";
    case "planned":
      return "Delivery here is planned. We will tell you when it starts.";
    case "unavailable":
      return "Mise does not deliver here yet.";
    default:
      return "Delivery areas are still being confirmed. We will let you know when deliveries reach you.";
  }
}

/** First letter of each of up to two names, for the account button. */
export function initials(first: string, last: string): string {
  return `${first.trim().charAt(0)}${last.trim().charAt(0)}`.toUpperCase() || "M";
}
