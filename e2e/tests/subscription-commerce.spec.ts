import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Page } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, signInToAdmin, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 3: plans, subscribing through the test payment provider, choosing
// meals, billing and invoices, pausing and cancelling, and the commerce admin.
// Needs the demo catalogue and demo plans and menus:
//   misectl catalog seed-demo && misectl commerce seed-demo
// and the worker running (it processes the provider's webhooks).

const password = "plantain and saltfish 2026";

/** A confirmed customer with a delivery address, signed in to the app. */
async function subscriber(page: Page, request: APIRequestContext, prefix: string) {
  const email = uniqueEmail(prefix);
  const res = await request.post(`${urls.api}/api/v1/auth/register`, {
    data: { email, password, first_name: "Darnell", last_name: "Joseph", accept_terms: true },
  });
  expect(res.status(), await res.text()).toBe(202);
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  const token = new URL(linkIn(body, "/verify-email")).searchParams.get("token");
  expect((await request.post(`${urls.api}/api/v1/auth/verify-email`, { data: { token } })).status()).toBe(200);
  const login = await request.post(`${urls.api}/api/v1/auth/login`, { data: { email, password } });
  const session = (await login.json()).data.token as string;
  const addr = await request.post(`${urls.api}/api/v1/me/addresses`, {
    headers: { Authorization: `Bearer ${session}` },
    data: { recipient_name: "Darnell Joseph", line1: "4 Coffee Street", city: "San Fernando", region: "san-fernando" },
  });
  expect(addr.status(), await addr.text()).toBe(201);
  await page.goto(`${urls.web}/sign-in`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/onboarding$/);
  return email;
}

/** Plans page → Basic → address → review → test provider. Returns at the provider's page. */
async function startBasic(page: Page) {
  await page.goto(`${urls.web}/plans`);
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Cook Caribbean at home");
  await expect(page.getByText("Test mode.").first()).toBeVisible();
  await page.getByRole("link", { name: "Choose Basic" }).click();
  await expect(page.getByRole("heading", { name: "Your Basic plan" })).toBeVisible();
  await page.getByRole("button", { name: "Review and pay" }).click();
  await expect(page.getByRole("heading", { name: "Review" })).toBeVisible();
  // Every amount and the renewal terms are shown before payment.
  await expect(page.getByRole("table")).toContainText("TTD 330.00");
  await expect(page.getByText(/is charged for each week on/)).toBeVisible();
  await expect(page.getByText("No tax is charged on this invoice.")).toBeVisible();
  await page.getByRole("button", { name: "Continue to payment" }).click();
  await expect(page).toHaveURL(/\/test-checkout\/tps_/);
  await expect(page.getByText("Test payment provider. No real money moves")).toBeVisible();
}

test("subscribe, pay through the test provider, choose meals, and see the invoice", async ({ page, request }) => {
  await subscriber(page, request, "subscribe");
  await startBasic(page);
  await page.getByRole("button", { name: "Approve payment" }).click();

  // The result page waits for the provider's confirmation to be processed.
  await expect(page).toHaveURL(/\/subscribe\/result\?checkout=/);
  await expect(page.getByRole("heading", { name: "You're subscribed." })).toBeVisible({ timeout: 30_000 });

  await page.goto(`${urls.web}/subscription`);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Basic plan");
  await expect(page.getByText("Active", { exact: true })).toBeVisible();
  await expect(page.getByText("Test card, charged automatically each week.")).toBeVisible();

  // Choose meals: the allowance is enforced on screen and by the server.
  await page.goto(`${urls.web}/menu`);
  await expect(page).toHaveURL(/\/menu\/\d{4}-\d{2}-\d{2}\/select$/);
  const summary = page.getByRole("complementary").filter({ has: page.getByRole("heading", { name: "Your box" }) });
  await expect(summary.getByRole("button", { name: "Choose 3 more" })).toBeDisabled();
  const add = page.getByRole("button", { name: /^Add [^a]/ });
  await add.nth(0).click();
  await add.nth(0).click();
  await page.getByRole("button", { name: /^Add another/ }).first().click();
  await expect(summary.getByText("Not saved yet.")).toBeVisible();
  // The plan's allowance is full: no more can be added.
  await expect(page.getByRole("button", { name: /^Add [^a]/ }).first()).toBeDisabled();
  await summary.getByRole("button", { name: "Confirm meals" }).click();
  await expect(summary.getByText(/Your meals are confirmed\./)).toBeVisible();
  // Confirmed according to the server, after a reload too.
  await page.reload();
  await expect(summary.getByText(/You can change your meals until/)).toBeVisible();
  await expect(summary.getByRole("button", { name: "Confirmed" })).toBeDisabled();

  // Billing and the invoice.
  await page.goto(`${urls.web}/billing`);
  const invoiceLink = page.getByRole("link", { name: /^MISE-\d{4}-\d{6}$/ });
  await expect(invoiceLink).toBeVisible();
  await invoiceLink.click();
  await expect(page.getByRole("heading", { name: "Invoice and receipt" })).toBeVisible();
  await expect(page.getByText("No tax is charged on this invoice.")).toBeVisible();
  const pdf = await page.request.get(`${page.url()}/pdf`);
  expect(pdf.status()).toBe(200);
  expect(pdf.headers()["content-type"]).toBe("application/pdf");
  expect((await pdf.body()).subarray(0, 5).toString()).toBe("%PDF-");

  // Orders: none until the week's menu closes.
  await page.goto(`${urls.web}/orders`);
  await expect(page.getByRole("heading", { name: "No orders yet." })).toBeVisible();
});

test("a declined first payment can be paid again", async ({ page, request }) => {
  await subscriber(page, request, "declined");
  await startBasic(page);
  await page.getByRole("button", { name: "Decline payment" }).click();
  await expect(page.getByRole("heading", { name: "Your payment didn't go through." })).toBeVisible({ timeout: 30_000 });
  await page.getByRole("link", { name: "Try again" }).click();
  await expect(page.getByRole("heading", { name: "Finish your first payment" })).toBeVisible();
  await page.getByRole("button", { name: "Pay now" }).click();
  await expect(page).toHaveURL(/\/test-checkout\/tps_/);
  await page.getByRole("button", { name: "Approve payment" }).click();
  await expect(page.getByRole("heading", { name: "You're subscribed." })).toBeVisible({ timeout: 30_000 });
});

test("pause, resume and cancel follow the policy and say what happens", async ({ page, request }) => {
  await subscriber(page, request, "pauses");
  await startBasic(page);
  await page.getByRole("button", { name: "Approve payment" }).click();
  await expect(page.getByRole("heading", { name: "You're subscribed." })).toBeVisible({ timeout: 30_000 });

  // The first week is paid, so a pause starts the week after.
  await page.goto(`${urls.web}/subscription/pause`);
  await page.getByText("2 weeks", { exact: true }).click();
  await page.getByRole("button", { name: "Pause deliveries" }).click();
  await expect(page.getByText("Your subscription was updated.")).toBeVisible();
  await expect(page.getByText("Pause scheduled", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Resume now" }).click();
  await expect(page.getByText("Active", { exact: true })).toBeVisible();

  await page.goto(`${urls.web}/subscription/cancel`);
  await expect(page.getByText("Weeks you've already paid for are still delivered.")).toBeVisible();
  await page.getByLabel("I'm moving").check();
  await page.getByRole("button", { name: "Cancel subscription" }).click();
  await expect(page.getByText("Ending", { exact: true })).toBeVisible();
  await expect(page.getByText(/Cancelled\. No deliveries from the week of/)).toBeVisible();
  // Changed their mind before it took effect.
  await page.getByRole("button", { name: "Keep my subscription" }).click();
  await expect(page.getByText("Active", { exact: true })).toBeVisible();
});

test("commerce pages are accessible and work on a phone", async ({ page, request }) => {
  await subscriber(page, request, "a11y-shop");
  for (const path of ["/plans", "/plans/basic", "/subscription", "/billing", "/orders"]) {
    await page.goto(`${urls.web}${path}`);
    const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze();
    expect(results.violations, `${path}: ${results.violations.map((v) => v.id).join(", ")}`).toEqual([]);
  }
  await startBasic(page);
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze();
  expect(results.violations.map((v) => v.id)).toEqual([]);
  await page.getByRole("button", { name: "Approve payment" }).click();
  await expect(page.getByRole("heading", { name: "You're subscribed." })).toBeVisible({ timeout: 30_000 });

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${urls.web}/menu`);
  await expect(page).toHaveURL(/\/select$/);
  const select = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze();
  expect(select.violations.map((v) => v.id)).toEqual([]);
  // The phone action bar is there, and nothing scrolls sideways.
  await expect(page.getByText(/0 of 3 chosen/)).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
});

test("admin: plans, menus, subscriptions and payments are visible to staff", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/plans`);
  await expect(page.getByRole("heading", { name: "Plans", level: 1 })).toBeVisible();
  await expect(page.getByRole("link", { name: "Basic" })).toBeVisible();
  await page.goto(`${urls.admin}/menus`);
  await expect(page.getByRole("heading", { name: "Weekly menus" })).toBeVisible();
  await expect(page.getByText("Published").first()).toBeVisible();
  await page.goto(`${urls.admin}/subscriptions`);
  await expect(page.getByRole("heading", { name: "Subscriptions" })).toBeVisible();
  await page.goto(`${urls.admin}/payments`);
  // Sprint 9.1: several providers; the page says which payments moved no real money.
  await expect(page.getByText("Payments marked test moved no real money")).toBeVisible();
  await page.goto(`${urls.admin}/orders`);
  await expect(page.getByRole("heading", { name: /Demand for the week of/ })).toBeVisible();
  await page.goto(`${urls.admin}/promotions`);
  await expect(page.getByText("None are created automatically.")).toBeVisible();
});
