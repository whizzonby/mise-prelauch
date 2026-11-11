import { describe, expect, it } from "vitest";

import { availabilityText, initials, minutes } from "./format";
import { greeting, localHour } from "./greeting";
import { safeNext } from "./safe-redirect";

describe("safeNext", () => {
  it("keeps paths on this site", () => {
    expect(safeNext("/meals?diet=vegan")).toBe("/meals?diet=vegan");
    expect(safeNext("/meals/curry-chicken#ingredients")).toBe("/meals/curry-chicken#ingredients");
  });

  it("refuses anything that could leave the site", () => {
    for (const bad of [
      "https://evil.example/",
      "//evil.example",
      "/\\evil.example",
      "javascript:alert(1)",
      "evil.example",
      "/meals\\..\\x",
      "/%0d%0aSet-Cookie:x",
      "\t/meals",
    ]) {
      const out = safeNext(bad);
      expect(out.startsWith("/")).toBe(true);
      expect(out.startsWith("//")).toBe(false);
      expect(out).not.toContain("evil");
    }
    expect(safeNext("//evil.example")).toBe("/dashboard");
  });

  it("never sends someone back to a signed-out page", () => {
    expect(safeNext("/sign-in")).toBe("/dashboard");
    expect(safeNext("/register?x=1")).toBe("/dashboard");
    expect(safeNext(undefined)).toBe("/dashboard");
  });
});

describe("greeting", () => {
  // 11:30 UTC is 07:30 in Port of Spain (UTC-4, no daylight saving).
  it("uses Trinidad and Tobago time, not the server's", () => {
    expect(localHour(new Date("2026-11-11T11:30:00Z"))).toBe(7);
    expect(greeting(new Date("2026-11-11T11:30:00Z"))).toBe("Good morning");
    expect(greeting(new Date("2026-11-11T17:00:00Z"))).toBe("Good afternoon");
    expect(greeting(new Date("2026-11-11T23:30:00Z"))).toBe("Good evening");
    expect(greeting(new Date("2026-11-12T06:00:00Z"))).toBe("Good evening"); // 2 am
  });
});

describe("format", () => {
  it("writes times the way a recipe would", () => {
    expect(minutes(25)).toBe("25 min");
    expect(minutes(70)).toBe("1 hr 10 min");
    expect(minutes(120)).toBe("2 hr");
  });

  it("never promises delivery unless it is available", () => {
    expect(availabilityText("unknown")).not.toMatch(/delivers here/);
    expect(availabilityText("planned")).not.toMatch(/delivers here/);
    expect(availabilityText("available")).toBe("Mise delivers here.");
  });

  it("makes initials", () => {
    expect(initials("kavita", "ramdass")).toBe("KR");
    expect(initials(" ", "")).toBe("M");
  });
});
