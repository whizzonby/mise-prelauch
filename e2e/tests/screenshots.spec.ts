import { mkdirSync } from "node:fs";
import { join } from "node:path";

import { expect, type Page, test } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, signInToAdmin, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 9, brief §40: desktop and phone screenshots of the key screens, for
// the report. Opt-in: E2E_SHOTS=1 npx playwright test screenshots --workers=1
// Screenshots are evidence of layout only, never of function.

test.skip(!process.env.E2E_SHOTS, "set E2E_SHOTS=1 to capture");

const dir = join(import.meta.dirname, "..", "..", "docs", "screenshots", "sprint-9", "visual");
mkdirSync(dir, { recursive: true });
const sizes = [
  ["desktop", { width: 1440, height: 900 }],
  ["phone", { width: 390, height: 844 }],
] as const;

async function capture(page: Page, name: string, url: string, fullPage = false) {
  for (const [label, size] of sizes) {
    await page.setViewportSize(size);
    await page.goto(url);
    await page.waitForLoadState("networkidle");
    await page.waitForFunction(() => document.getAnimations().every((a) => a.playState !== "running" || a.effect?.getTiming().iterations === Infinity));
    await page.screenshot({ path: join(dir, `${name}-${label}.png`), fullPage });
  }
}

test("public and customer screens", async ({ page, request }) => {
  test.setTimeout(300_000);
  await capture(page, "01-landing", urls.site);
  await capture(page, "02-catalogue", `${urls.web}/meals`);
  await page.goto(`${urls.web}/meals`);
  const meal = await page.locator("main article h3 a").first().getAttribute("href");
  await capture(page, "03-meal-detail", `${urls.web}${meal}`);
  await capture(page, "04-plans", `${urls.web}/plans`);

  // A signed-in customer for checkout and the dashboard.
  const email = uniqueEmail("shots");
  const password = "plantain and saltfish 2026";
  await request.post(`${urls.api}/api/v1/auth/register`, { data: { email, password, first_name: "Simone", last_name: "Charles", accept_terms: true } });
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  await request.post(`${urls.api}/api/v1/auth/verify-email`, { data: { token: new URL(linkIn(body, "/verify-email")).searchParams.get("token") } });
  const session = (await (await request.post(`${urls.api}/api/v1/auth/login`, { data: { email, password } })).json()).data.token;
  await request.post(`${urls.api}/api/v1/me/addresses`, {
    headers: { Authorization: `Bearer ${session}` },
    data: { recipient_name: "Simone Charles", line1: "9 Alexandra Street", city: "St Clair", region: "port-of-spain" },
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`${urls.web}/sign-in`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL(/\/onboarding$/);
  await capture(page, "05-checkout-plan", `${urls.web}/subscribe?plan=basic`);
  await page.goto(`${urls.web}/subscribe?plan=basic`);
  await page.getByRole("button", { name: "Review and pay" }).click();
  await page.waitForURL(/\/subscribe\/review/);
  await capture(page, "06-checkout-review", page.url());
  await page.getByRole("button", { name: "Continue to payment" }).click();
  await page.waitForURL(/\/test-checkout\//);
  await page.getByRole("button", { name: "Approve payment" }).click();
  await expect(page.getByRole("heading", { name: "You're subscribed." })).toBeVisible({ timeout: 30_000 });
  await capture(page, "07-customer-dashboard", `${urls.web}/dashboard`);
  await page.goto(`${urls.web}/menu`);
  await capture(page, "08-meal-selection", page.url());
});

test("admin and operations screens", async ({ page }) => {
  test.setTimeout(300_000);
  await signInToAdmin(page);
  for (const [name, path] of [
    ["10-admin-overview", "/"],
    ["11-production", "/production"],
    ["12-procurement", "/procurement"],
    ["13-inventory", "/inventory"],
    ["14-delivery", "/delivery"],
    ["15-analytics", "/intelligence"],
    ["16-launch-readiness", "/launch"],
    ["17-system-status", "/system"],
  ] as const) {
    await capture(page, name, `${urls.admin}${path}`);
  }
});
