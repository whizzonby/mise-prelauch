import { describe, expect, it } from "vitest";

import { formatMoney } from "@mise/api-client";

import { timeLeft } from "./commerce";
import { addMeal, countOf, removeMeal, sameChoice, toItems } from "./selection";

describe("meal choice", () => {
  it("counts, adds and removes", () => {
    let c = addMeal({}, "b");
    c = addMeal(c, "a");
    c = addMeal(c, "b");
    expect(countOf(c)).toBe(3);
    c = removeMeal(c, "a");
    expect(c).toEqual({ b: 2 });
    expect(removeMeal({}, "x")).toEqual({});
  });

  it("sends items in a stable order without zero quantities", () => {
    expect(toItems({ b: 1, a: 2, c: 0 })).toEqual([
      { meal_id: "a", quantity: 2 },
      { meal_id: "b", quantity: 1 },
    ]);
  });

  it("compares choices regardless of key order", () => {
    expect(sameChoice({ a: 1, b: 2 }, { b: 2, a: 1 })).toBe(true);
    expect(sameChoice({ a: 1 }, { a: 2 })).toBe(false);
    expect(sameChoice({ a: 0 }, {})).toBe(true);
  });
});

describe("money and time", () => {
  it("formats minor units without floating point surprises", () => {
    expect(formatMoney(33000)).toBe("TTD 330.00");
    expect(formatMoney(123456789)).toBe("TTD 1,234,567.89");
    expect(formatMoney(-4950)).toBe("-TTD 49.50");
    expect(formatMoney(5)).toBe("TTD 0.05");
  });

  it("says how long is left, and nothing once a deadline has passed", () => {
    const now = new Date("2026-11-11T14:00:00Z");
    expect(timeLeft("2026-11-11T14:30:00Z", now)).toBe("30 minutes");
    expect(timeLeft("2026-11-12T14:00:00Z", now)).toBe("24 hours");
    expect(timeLeft("2026-11-14T15:00:00Z", now)).toBe("3 days");
    expect(timeLeft("2026-11-11T13:59:00Z", now)).toBeNull();
  });
});
