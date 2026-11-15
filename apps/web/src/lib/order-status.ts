/**
 * An order's status in customer words. A delivered outcome that delivery
 * corrected is shown as under review: the order's own status is never
 * rewritten (commerce keeps its history), so the dispute flag decides.
 */
const LABELS: Record<string, string> = {
  pending: "Confirmed",
  confirmed: "Confirmed",
  awaiting_production: "Confirmed",
  in_production: "Being prepared",
  ready_for_fulfillment: "Packed",
  out_for_delivery: "Out for delivery",
  delivered: "Delivered",
  cancelled: "Cancelled",
};

export function orderStatus(status: string, outcome?: string | null): { label: string; tone: "success" | "warning" | "neutral" } {
  if (outcome === "disputed") return { label: "Delivery under review", tone: "warning" };
  if (status === "cancelled") return { label: LABELS.cancelled!, tone: "neutral" };
  return { label: LABELS[status] ?? "Confirmed", tone: "success" };
}
