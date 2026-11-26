import { expect, test } from "@playwright/test";

import { urls } from "../playwright.config";
import { signInToAdmin } from "./helpers";

// Sprint 9.1: several payment providers behind the one payments module. In
// this environment only the simulator is configured (no Stripe or PayPal
// credentials exist), so these tests check what staff and customers are
// shown, and that nothing unavailable is offered. Provider behaviour itself
// is tested in Go against fake provider servers.

test("customers are offered only providers that can take the payment now", async ({ request }) => {
  const ttd = await (await request.get(`${urls.api}/api/v1/payment-options?currency=TTD`)).json();
  expect(ttd.data.items.map((o: { provider: string }) => o.provider)).toEqual(["testpay"]);
  expect(ttd.data.items[0]).toMatchObject({ label: "Test payments (simulator)", test: true, recurring: true });
  const eur = await (await request.get(`${urls.api}/api/v1/payment-options?currency=EUR`)).json();
  expect(eur.data.items).toEqual([]);
  expect((await request.get(`${urls.api}/api/v1/payment-options?currency=ttd`)).status()).toBe(422);
});

test("staff see every provider's status, masked credentials and why it is not taking payments", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/payments/providers`);
  await expect(page.getByRole("heading", { level: 1, name: "Payment providers" })).toBeVisible();
  for (const name of ["Test payments (simulator)", "Card (Scotiabank)"]) {
    await expect(page.getByRole("heading", { name })).toBeVisible();
  }
  await expect(page.getByText("Taking payments").first()).toBeVisible();
  await expect(page.getByText(/Blocked: Scotiabank has not provided a merchant integration specification/).first()).toBeVisible();
  // Nothing secret is on the page.
  const html = await page.content();
  for (const secret of ["sk_live_", "sk_test_", "whsec_", "client_secret"]) expect(html).not.toContain(secret);

  // Scotiabank cannot be switched on: the API refuses while it is blocked.
  const scotia = page.getByRole("form", { name: "Enable Card (Scotiabank)" });
  await scotia.getByRole("button", { name: "Enable for checkout" }).click();
  await expect(scotia.getByText(/cannot be enabled/)).toBeVisible();
});

test("the launch page reports that only test payments are possible", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/launch`);
  await expect(page.getByText(/Only test-mode providers are taking payments \(Test payments \(simulator\)\)/)).toBeVisible();
});
