import { expect, type Page, test } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, signInToAdmin, waitForEmail } from "./helpers";

// Sprint 9 performance evidence: navigation timings of key pages on the
// production builds (`next build` + `next start`), median of 5 loads each,
// every load in a fresh browser context (empty HTTP cache, same session). Opt-in, because the numbers depend on the machine:
//   E2E_PERF=1 npx playwright test performance --workers=1
// Results are printed as JSON lines (PERF …) and copied into
// docs/testing/PERFORMANCE_RESULTS.md.

test.skip(!process.env.E2E_PERF, "set E2E_PERF=1 to measure");

type Timing = { ttfb: number; dcl: number; load: number; lcp: number | null; bytes: number };

async function measure(page: Page, url: string): Promise<Timing> {
  await page.goto(url, { waitUntil: "load" });
  return page.evaluate(
    () =>
      new Promise<Timing>((resolve) => {
        const nav = performance.getEntriesByType("navigation")[0] as PerformanceNavigationTiming;
        const bytes = performance.getEntriesByType("resource").reduce((n, r) => n + ((r as PerformanceResourceTiming).transferSize || 0), nav.transferSize || 0);
        let lcp: number | null = null;
        new PerformanceObserver((list) => {
          const last = list.getEntries().at(-1);
          if (last) lcp = last.startTime;
        }).observe({ type: "largest-contentful-paint", buffered: true });
        setTimeout(
          () => resolve({ ttfb: nav.responseStart, dcl: nav.domContentLoadedEventEnd, load: nav.loadEventEnd, lcp, bytes }),
          300,
        );
      }),
  );
}

const median = (xs: number[]) => {
  const s = [...xs].sort((a, b) => a - b);
  return Math.round(s[Math.floor(s.length / 2)]!);
};

async function report(page: Page, name: string, url: string) {
  const runs: Timing[] = [];
  const state = await page.context().storageState();
  for (let i = 0; i < 5; i++) {
    // A fresh browser context each run: empty HTTP cache (cold load), same session cookies.
    const ctx = await page.context().browser()!.newContext({ storageState: state, viewport: { width: 1366, height: 850 } });
    const p = await ctx.newPage();
    runs.push(await measure(p, url));
    await ctx.close();
  }
  const out = {
    name,
    ttfb: median(runs.map((r) => r.ttfb)),
    dcl: median(runs.map((r) => r.dcl)),
    load: median(runs.map((r) => r.load)),
    lcp: runs.every((r) => r.lcp !== null) ? median(runs.map((r) => r.lcp!)) : null,
    kb: Math.round(median(runs.map((r) => r.bytes)) / 1024),
  };
  console.log("PERF", JSON.stringify(out));
  expect(out.load).toBeGreaterThan(0);
}

test("public pages", async ({ page }) => {
  test.setTimeout(300_000);
  await page.goto(`${urls.web}/meals`);
  const meal = await page.locator("main article h3 a").first().getAttribute("href");
  for (const [name, url] of [
    ["landing", urls.site],
    ["catalogue", `${urls.web}/meals`],
    ["meal detail", `${urls.web}${meal}`],
    ["plans", `${urls.web}/plans`],
  ] as const) {
    await report(page, name, url);
  }
});

test("customer dashboard and checkout review", async ({ page, request }) => {
  test.setTimeout(300_000);
  // A confirmed customer with an address, signed in through the app.
  const email = `perf-${Date.now()}@example.com`;
  const password = "plantain and saltfish 2026";
  await request.post(`${urls.api}/api/v1/auth/register`, { data: { email, password, first_name: "Perf", last_name: "Test", accept_terms: true } });
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  const token = new URL(linkIn(body, "/verify-email")).searchParams.get("token");
  await request.post(`${urls.api}/api/v1/auth/verify-email`, { data: { token } });
  const session = (await (await request.post(`${urls.api}/api/v1/auth/login`, { data: { email, password } })).json()).data.token;
  await request.post(`${urls.api}/api/v1/me/addresses`, {
    headers: { Authorization: `Bearer ${session}` },
    data: { recipient_name: "Perf Test", line1: "1 Frederick Street", city: "Port of Spain", region: "port-of-spain" },
  });
  await page.goto(`${urls.web}/sign-in`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL(/\/onboarding$/);
  await page.goto(`${urls.web}/subscribe?plan=basic`);
  await page.getByRole("button", { name: "Review and pay" }).click();
  await page.waitForURL(/\/subscribe\/review/);
  await report(page, "checkout review", page.url());
  await report(page, "customer dashboard", `${urls.web}/dashboard`);
});

test("admin pages", async ({ page }) => {
  test.setTimeout(300_000);
  await signInToAdmin(page);
  for (const [name, path] of [
    ["admin overview", "/"],
    ["admin analytics overview", "/intelligence"],
    ["admin orders", "/orders"],
    ["admin production", "/production"],
  ] as const) {
    await report(page, name, `${urls.admin}${path}`);
  }
});
