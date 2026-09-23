import { describe, expect, it } from "vitest";

import { formatDateTime, formatDay, humanize, niceMax, percent, statusTone } from "./format";

describe("format", () => {
  it("turns stored option values into labels", () => {
    expect(humanize("san-fernando-south")).toBe("San fernando south");
    expect(humanize("household_type")).toBe("Household type");
  });

  it("gives a share as a whole percentage and survives an empty total", () => {
    expect(percent(1, 3)).toBe("33%");
    expect(percent(0, 0)).toBe("0%");
  });

  it("shows timestamps in Trinidad & Tobago time", () => {
    // 02:30 UTC is 22:30 the previous day in Port of Spain (UTC-4).
    expect(formatDateTime("2026-09-23T02:30:00Z")).toBe("22 Sept 2026, 22:30");
  });

  it("formats a calendar day without shifting it", () => {
    expect(formatDay("2026-09-01")).toBe("1 Sept");
  });

  it("marks statuses that need attention", () => {
    expect(statusTone("BLOCKED")).toBe("error");
    expect(statusTone("flagged")).toBe("error");
    expect(statusTone("PENDING")).toBe("warning");
    expect(statusTone("VERIFIED")).toBe("success");
  });
});

describe("chart axis", () => {
  it("rounds the axis top up to a clean number", () => {
    expect([0, 3, 4, 5, 17, 27, 41, 99, 100, 101, 870].map(niceMax)).toEqual([4, 4, 4, 5, 20, 40, 50, 100, 100, 200, 1000]);
  });
});
