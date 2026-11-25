import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { urls } from "../playwright.config";
import { createLeadViaApi, linkIn, uniqueEmail, verifyViaApi, waitForEmail } from "./helpers";

// Customer accounts in the customer app (apps/web), Sprint 1.

const password = "plantain and saltfish 2026";

async function register(page: import("@playwright/test").Page, email: string) {
  await page.goto(`${urls.web}/register`);
  await page.getByLabel("First name").fill("Kavita");
  await page.getByLabel("Last name").fill("Ramdass");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByLabel("I accept the Mise terms and privacy policy.").check();
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Check your email");
}

async function confirmFromEmail(page: import("@playwright/test").Page, request: import("@playwright/test").APIRequestContext, email: string) {
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  const link = new URL(linkIn(body, "/verify-email"));
  await page.goto(`${urls.web}${link.pathname}${link.search}`);
  // Opening the link alone confirms nothing; the person presses the button.
  await page.getByRole("button", { name: "Confirm my email" }).click();
  await expect(page.getByText("Your email is confirmed")).toBeVisible();
}

async function signIn(page: import("@playwright/test").Page, email: string, pw = password) {
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(pw);
  await page.getByRole("button", { name: "Sign in" }).click();
}

test("a visitor registers, confirms their email, signs in and out", async ({ page, request }) => {
  const email = uniqueEmail("account");
  // A visitor arriving at the app sees the meal kits (Sprint 9), not a sign-in wall.
  await page.goto(urls.web);
  await expect(page).toHaveURL(/\/meals$/);

  await register(page, email);
  await confirmFromEmail(page, request, email);

  await signIn(page, email);
  // A new customer starts by setting up their kitchen.
  await expect(page).toHaveURL(/\/onboarding$/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Welcome to Mise, Kavita.");
  await page.goto(`${urls.web}/profile/security`);
  await expect(page.getByText("(this device)")).toBeVisible();

  // The session lives in an httpOnly cookie; nothing is in browser storage.
  const cookie = (await page.context().cookies()).find((c) => c.name === "mise_session");
  expect(cookie?.httpOnly).toBe(true);
  expect(await page.evaluate(() => Object.keys(localStorage).length)).toBe(0);

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/sign-in$/);
  await page.goto(`${urls.web}/dashboard`);
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fdashboard$/);
});

test("wrong passwords and unconfirmed addresses are refused", async ({ page }) => {
  const email = uniqueEmail("unconfirmed");
  await register(page, email);
  await page.goto(`${urls.web}/sign-in`);
  await signIn(page, email);
  await expect(page.getByRole("alert").filter({ hasText: "Confirm your email address first" })).toBeVisible();

  await signIn(page, email, "not the right password");
  await expect(page.getByRole("alert").filter({ hasText: "Email or password is incorrect." })).toBeVisible();
});

test("a forgotten password is reset from the email link", async ({ page, request }) => {
  const email = uniqueEmail("reset");
  await register(page, email);
  await confirmFromEmail(page, request, email);

  await page.goto(`${urls.web}/forgot-password`);
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Send reset link" }).click();
  await expect(page.getByText("a link to reset its password is on its way")).toBeVisible();

  const body = await waitForEmail(request, email, "Reset your Mise password");
  const link = new URL(linkIn(body, "/reset-password"));
  await page.goto(`${urls.web}${link.pathname}${link.search}`);
  await page.getByLabel("New password").fill("callaloo on a sunday 99");
  await page.getByRole("button", { name: "Save new password" }).click();
  await expect(page.getByText("Your password is changed")).toBeVisible();

  await signIn(page, email, "callaloo on a sunday 99");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Welcome to Mise, Kavita.");
});

test("a waitlist lead who registers is linked to their account in the admin", async ({ page, request }) => {
  const email = uniqueEmail("linked");
  await createLeadViaApi(request, { name: "Kavita", email, ip: "203.0.113.77" });
  await verifyViaApi(request, email);

  await register(page, email);
  await confirmFromEmail(page, request, email);

  // The API is the source of truth: the lead now shows its account.
  const login = await request.post(`${urls.api}/api/v1/admin/auth/login`, {
    data: { email: process.env.E2E_ADMIN_EMAIL, password: process.env.E2E_ADMIN_PASSWORD },
  });
  const { data } = (await login.json()) as { data: { token: string; mfa: string } };
  test.skip(data.mfa !== "verified" && data.mfa !== "not_enrolled", "admin MFA is on in this environment");
  const auth = { Authorization: `Bearer ${data.token}` };
  const list = await request.get(`${urls.api}/api/v1/admin/leads`, { headers: auth, params: { q: email } });
  const lead = ((await list.json()) as { data: { items: { id: string }[] } }).data.items[0];
  expect(lead, `the lead for ${email} in the admin list`).toBeDefined();
  if (!lead) return;
  const detail = await request.get(`${urls.api}/api/v1/admin/leads/${lead.id}`, { headers: auth });
  const json = (await detail.json()) as { data: { account: unknown; status: string } };
  expect(json.data.account).not.toBeNull();
  expect(json.data.status).toBe("VERIFIED");
});

for (const path of ["/sign-in", "/register", "/forgot-password"]) {
  test(`account page ${path} has no automatically detectable accessibility problems`, async ({ page }) => {
    await page.goto(`${urls.web}${path}`);
    const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze();
    expect(results.violations.map((v) => `${v.id}: ${v.help}`)).toEqual([]);
  });
}
