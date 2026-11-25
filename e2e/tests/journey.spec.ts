import { expect, test } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 9, brief §38: one new visitor, in one browser session, from the
// landing page to an active, paid subscription with chosen meals, then what
// staff see for that customer (payment, subscription, history).
//
// Steps 14-19 (production, fulfillment, dispatch, proof of delivery, the
// customer's delivered order) need a week whose cutoff has passed, so they
// cannot follow on the real clock in the same run. They are covered by
// chef-production.spec and delivery-fulfillment.spec over orders that
// `misectl production seed-demo` places through the same public API (see
// docs/testing/E2E_COVERAGE.md).
//
// Needs: misectl catalog seed-demo && misectl commerce seed-demo, and the worker.

const password = "plantain and saltfish 2026";

test("a visitor becomes a paying subscriber with chosen meals, and staff see the same facts", async ({ page, request }) => {
  test.setTimeout(240_000);

  // 1. Landing page.
  await page.goto(urls.site);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

  // 2. Browse published meal kits (the customer app's public catalogue).
  await page.goto(`${urls.web}/meals`);
  const first = page.locator("main article h3 a").first();
  await expect(first).toBeVisible();
  const mealName = (await first.textContent())!.trim();

  // 3. Meal detail.
  await first.click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(mealName);

  // 4. Choose a plan as a visitor.
  await page.goto(`${urls.web}/plans`);
  await page.getByRole("link", { name: "Choose Basic" }).click();
  await expect(page).toHaveURL(/\/sign-in\?next=/);

  // 5. Create an account and confirm the email.
  await page.getByRole("link", { name: "Create an account" }).click();
  const email = uniqueEmail("journey");
  await page.getByLabel("First name").fill("Renée");
  await page.getByLabel("Last name").fill("Baptiste");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByLabel("I accept the Mise terms and privacy policy.").check();
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Check your email");
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  const link = new URL(linkIn(body, "/verify-email"));
  await page.goto(`${urls.web}${link.pathname}${link.search}`);
  await page.getByRole("button", { name: "Confirm my email" }).click();
  await expect(page.getByText("Your email is confirmed")).toBeVisible();
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  // Back at the chosen plan.
  await expect(page).toHaveURL(/\/subscribe\?plan=basic$/);
  await expect(page.getByRole("heading", { name: "Your Basic plan" })).toBeVisible();

  // 7. Delivery details: no address yet, so add one and come straight back.
  await expect(page.getByText("You haven't saved an address yet.")).toBeVisible();
  await page.getByRole("link", { name: "Add an address" }).click();
  const add = page.getByRole("form", { name: "Add an address" });
  await add.getByLabel("Who receives deliveries").fill("Renée Baptiste");
  await add.getByLabel("Street address").fill("7 Rosalino Street");
  await add.getByLabel("Town or city").fill("Woodbrook");
  await add.getByLabel("Region").selectOption("port-of-spain");
  await add.getByRole("button", { name: "Add address" }).click();
  await expect(page.getByText("Address added at")).toBeVisible();
  await page.getByRole("link", { name: "Continue with your plan" }).click();
  await expect(page.getByRole("heading", { name: "Your Basic plan" })).toBeVisible();

  // Review: server-calculated amounts and terms, before any payment.
  await page.getByRole("button", { name: "Review and pay" }).click();
  await expect(page.getByRole("heading", { name: "Review" })).toBeVisible();
  await expect(page.getByRole("table")).toContainText("TTD 330.00");

  // 8-9. Sandbox payment; the result page waits for the webhook-confirmed payment.
  await page.getByRole("button", { name: "Continue to payment" }).click();
  await expect(page).toHaveURL(/\/test-checkout\/tps_/);
  await page.getByRole("button", { name: "Approve payment" }).click();
  await expect(page).toHaveURL(/\/subscribe\/result\?checkout=/);
  await expect(page.getByRole("heading", { name: "You're subscribed." })).toBeVisible({ timeout: 30_000 });

  // 6. Select meals and confirm quantities (the plan's allowance is enforced).
  await page.goto(`${urls.web}/menu`);
  await expect(page).toHaveURL(/\/menu\/\d{4}-\d{2}-\d{2}\/select$/);
  const addMeal = page.getByRole("button", { name: /^Add [^a]/ });
  await addMeal.nth(0).click();
  await addMeal.nth(0).click();
  await page.getByRole("button", { name: /^Add another/ }).first().click();
  // The box summary on a wide screen, the action bar on a phone: whichever is shown.
  await page.getByRole("button", { name: "Confirm meals" }).filter({ visible: true }).click();
  await expect(page.getByRole("button", { name: "Confirmed" }).filter({ visible: true })).toBeDisabled();

  // 11-13. Dashboard, subscription status and the invoice.
  await page.goto(`${urls.web}/dashboard`);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await page.goto(`${urls.web}/subscription`);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Basic plan");
  await expect(page.getByText("Active", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: /Pause/ }).first()).toBeVisible();
  await page.goto(`${urls.web}/billing`);
  await expect(page.getByRole("link", { name: /^MISE-\d{4}-\d{6}$/ })).toBeVisible();
  // 10. Orders are created when the week's menu closes; until then the page says so honestly.
  await page.goto(`${urls.web}/orders`);
  await expect(page.getByRole("heading", { name: "No orders yet." })).toBeVisible();

  // 20. Staff see the same facts: the customer record, the payment and the history.
  const login = await request.post(`${urls.api}/api/v1/admin/auth/login`, {
    data: { email: process.env.E2E_ADMIN_EMAIL, password: process.env.E2E_ADMIN_PASSWORD },
  });
  expect(login.status()).toBe(200);
  const headers = { Authorization: `Bearer ${(await login.json()).data.token}` };
  const get = async (path: string) => {
    const res = await request.get(`${urls.api}/api/v1/admin${path}`, { headers });
    expect(res.status(), `${path}: ${await res.text()}`).toBe(200);
    return (await res.json()).data;
  };
  const found = await get(`/customers?q=${encodeURIComponent(email)}`);
  expect(found.items).toHaveLength(1);
  const id = found.items[0].id as string;
  const billing = JSON.stringify(await get(`/customers/${id}/billing`));
  expect(billing).toContain("succeeded");
  expect(billing).toMatch(/\b33000\b/); // TTD 330.00 in minor units
  const activity = JSON.stringify(await get(`/customers/${id}/activity`));
  expect(activity).toMatch(/subscri/i);
});
