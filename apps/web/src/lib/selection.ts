import type { SelectionItem } from "@mise/api-client";

/** Meal id to quantity, as the customer is choosing. */
export type Choice = Record<string, number>;

export function countOf(c: Choice): number {
  return Object.values(c).reduce((a, b) => a + b, 0);
}

export function addMeal(c: Choice, id: string): Choice {
  return { ...c, [id]: (c[id] ?? 0) + 1 };
}

export function removeMeal(c: Choice, id: string): Choice {
  const next = { ...c };
  const q = (next[id] ?? 0) - 1;
  if (q > 0) next[id] = q;
  else delete next[id];
  return next;
}

/** Items to send, without zero quantities, in a stable order. */
export function toItems(c: Choice): SelectionItem[] {
  return Object.entries(c)
    .filter(([, q]) => q > 0)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([meal_id, quantity]) => ({ meal_id, quantity }));
}

export function sameChoice(a: Choice, b: Choice): boolean {
  const x = toItems(a);
  const y = toItems(b);
  return x.length === y.length && x.every((it, i) => it.meal_id === y[i]?.meal_id && it.quantity === y[i]?.quantity);
}
