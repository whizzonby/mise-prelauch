import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Page } from "@playwright/test";

import { urls } from "../playwright.config";
import { createLeadViaApi, linkIn, signInToAdmin, uniqueEmail, verifyViaApi, waitForEmail } from "./helpers";

// Sprint 4: the operational overview, customers and the Customer 360, CRM
// stages and notes, support cases, segments and campaigns, staff
// invitations and roles, exports and the audit log. Needs the worker
// running (it sends invitations and builds exports). The E2E admin must be
// a super admin.

const password = "plantain and saltfish 2026";

/** A confirmed customer account, made through the public API. */
async function customer(request: APIRequestContext, prefix: string) {
  const email = uniqueEmail(prefix);
  const res = await request.post(`${urls.api}/api/v1/auth/register`, {
    data: { email, password, first_name: "Renée", last_name: "Baptiste", accept_terms: true },
  });
  expect(res.status(), await res.text()).toBe(202);
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  const token = new URL(linkIn(body, "/verify-email")).searchParams.get("token");
  expect((await request.post(`${urls.api}/api/v1/auth/verify-email`, { data: { token } })).status()).toBe(200);
  return email;
}

async function noSeriousA11yIssues(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations.filter((v) => v.impact === "serious" || v.impact === "critical")).toEqual([]);
}

test("the overview explains every figure and lists what needs attention", async ({ page }) => {
  await signInToAdmin(page);
  for (const section of ["Needs attention", "Customers and subscriptions", "Orders", "Money", "Waitlist"]) {
    await expect(page.getByRole("heading", { name: section, exact: true })).toBeVisible();
  }
  await expect(page.getByText("Active subscriptions", { exact: true })).toBeVisible();
  await page.getByText("What this counts").first().click();
  await expect(page.getByText(/Collected is gross receipts before refunds/)).toBeVisible();
  // The grouped navigation shows the Sprint 4 areas.
  const nav = page.getByRole("navigation", { name: "Admin" });
  for (const label of ["All customers", "Support cases", "Segments", "Campaigns", "Reports", "Exports", "Staff", "Audit log", "Settings"]) {
    await expect(nav.getByRole("link", { name: label, exact: true })).toBeVisible();
  }
  await noSeriousA11yIssues(page);
});

test("find a customer, read the 360, add a note and open a support case", async ({ page, request }) => {
  const email = await customer(request, "c360");
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/customers`);
  await page.getByLabel("Name or email").fill(email);
  await page.getByRole("button", { name: "Apply" }).click();
  await expect(page.getByText("1 result")).toBeVisible();
  await page.getByRole("link", { name: "Renée Baptiste" }).click();

  await expect(page.getByRole("heading", { level: 1, name: "Renée Baptiste" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Acquisition" })).toBeVisible();
  await expect(page.getByText("Never subscribed.")).toBeVisible();
  await noSeriousA11yIssues(page);

  // Sensitive details sit on their own tab (and opening it is audited).
  await page.getByRole("link", { name: "Preferences" }).click();
  await expect(page.getByRole("heading", { name: "Allergies" })).toBeVisible();
  await expect(page.getByText("Viewing it is recorded in the audit log.")).toBeVisible();

  await page.getByRole("link", { name: "Notes" }).click();
  await page.getByRole("textbox", { name: "Note" }).fill("Called about delivery areas; will follow up next week.");
  await page.getByRole("button", { name: "Add note" }).click();
  await expect(page.getByText("Called about delivery areas; will follow up next week.")).toBeVisible();

  // A case opened from the profile is linked to the customer.
  await page.getByRole("link", { name: "Open a support case" }).click();
  await expect(page.getByText(`(${email})`)).toBeVisible();
  await page.getByLabel("Subject").fill("Asks whether we deliver to Arima");
  await page.getByLabel("Category").selectOption("delivery");
  await page.getByLabel("First note").fill("Customer emailed on Monday.");
  await page.getByRole("button", { name: "Open case" }).click();
  await expect(page.getByRole("heading", { level: 1, name: /^#\d+ Asks whether we deliver to Arima$/ })).toBeVisible();

  // Resolving needs a resolution.
  await page.getByLabel("Status").selectOption("resolved");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("form", { name: "Update case" }).getByRole("alert")).toContainText("Say how the case was resolved.");
  await page.getByLabel("Resolution").fill("Told them Arima is in the first delivery area.");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Case updated.")).toBeVisible();
  await expect(page.getByText("Status: In progress → Resolved").or(page.getByText("Status: Open → Resolved"))).toBeVisible();

  // The audit log recorded the sensitive view.
  await page.goto(`${urls.admin}/audit?action=customer.sensitive`);
  await expect(page.getByRole("cell", { name: "customer.sensitive_viewed" }).first()).toBeVisible();
});

test("a lead's CRM stage and notes, without touching its email status", async ({ page, request }) => {
  const email = uniqueEmail("crm");
  await createLeadViaApi(request, { name: "Anika", email, ip: "203.0.113.41" });
  await verifyViaApi(request, email);
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/leads?q=${encodeURIComponent(email)}`);
  await page.getByRole("row").filter({ hasText: email }).getByRole("link", { name: "Anika" }).click();

  const crm = page.getByRole("form", { name: "CRM stage" });
  await crm.getByLabel("Stage").selectOption("contacted");
  await crm.getByRole("button", { name: "Save" }).click();
  await expect(crm.getByText("Saved.")).toBeVisible();

  await page.getByRole("textbox", { name: "Note" }).fill("Prefers WhatsApp.");
  await page.getByRole("button", { name: "Add note" }).click();
  await expect(page.getByText("Prefers WhatsApp.")).toBeVisible();

  await page.goto(`${urls.admin}/leads?q=${encodeURIComponent(email)}&stage=contacted`);
  const row = page.getByRole("row").filter({ hasText: email });
  await expect(row).toContainText("Contacted");
  await expect(row).toContainText("Verified");
});

test("segments preview their members and campaigns record their tag", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/segments/new`);
  const name = `Prospects ${Date.now()}`;
  await page.getByLabel("Name").fill(name);
  await page.getByLabel("Prospect").check();
  await page.getByRole("button", { name: "Create segment" }).click();
  await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
  await expect(page.getByText(/customers? match(es)? right now\./)).toBeVisible();

  await page.goto(`${urls.admin}/campaigns/new`);
  await page.getByLabel("Name").fill(`Launch week ${Date.now()}`);
  await page.getByLabel("UTM campaign tag").fill(`launch-${Date.now()}`);
  await page.getByLabel("Audience segment").selectOption({ label: name });
  await page.getByRole("button", { name: "Create campaign" }).click();
  await expect(page.getByText("Became customers", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Mark as scheduled" }).click();
  // The status moves on, and so do the choices offered.
  await expect(page.getByRole("button", { name: "Mark as draft" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Mark as scheduled" })).toHaveCount(0);
});

test("exports are built in the background and downloaded by the requester", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/exports`);
  await page.getByLabel("What to export").selectOption("customers");
  await page.getByRole("button", { name: "Request export" }).click();
  await expect(page.getByText("Export requested.")).toBeVisible();
  await expect(async () => {
    await page.reload();
    await expect(page.getByRole("link", { name: "Download" }).first()).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 30_000 });
  const download = page.waitForEvent("download");
  await page.getByRole("link", { name: "Download" }).first().click();
  expect((await download).suggestedFilename()).toMatch(/\.csv$/);
});

test("invite a staff member, who sets a password and sees only their role's areas", async ({ page, request, browser }) => {
  const email = uniqueEmail("staff");
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/staff`);
  const form = page.getByRole("form", { name: "Invite staff" });
  await form.getByLabel("Name").fill("Keisha Support");
  await form.getByLabel("Email").fill(email);
  await form.getByLabel("Role").selectOption("support");
  await form.getByRole("button", { name: "Send invitation" }).click();
  await expect(page.getByText("Invitation sent.")).toBeVisible();

  const body = await waitForEmail(request, email, "You're invited to the Mise console");
  const link = linkIn(body, "/accept-invite");
  const staff = await (await browser.newContext()).newPage();
  await staff.goto(link.replace(/^https?:\/\/[^/]+/, urls.admin));
  await staff.getByLabel("Choose a password").fill(password);
  await staff.getByLabel("Type it again").fill(password);
  await staff.getByRole("button", { name: "Set password" }).click();
  await expect(staff.getByText("Your password is set.")).toBeVisible();

  await staff.getByLabel("Email", { exact: true }).fill(email);
  await staff.getByLabel("Password").fill(password);
  await staff.getByRole("button", { name: "Sign in" }).click();
  const nav = staff.getByRole("navigation", { name: "Admin" });
  await expect(nav.getByRole("link", { name: "Support cases" })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Audit log" })).toHaveCount(0);
  await expect(nav.getByRole("link", { name: "Staff" })).toHaveCount(0);
  // The pages themselves are closed too, not just hidden.
  await staff.goto(`${urls.admin}/audit`);
  await expect(staff.getByRole("heading", { name: "Audit log" })).toHaveCount(0);

  // The grant is in the audit log.
  await page.goto(`${urls.admin}/audit?action=staff.inv`);
  await expect(page.getByRole("cell", { name: /staff\.invitation/ }).first()).toBeVisible();
});

test("the console works on a phone", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await signInToAdmin(page);
  for (const path of ["/", "/customers", "/support", "/reports"]) {
    await page.goto(`${urls.admin}${path}`);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, path).toBeLessThanOrEqual(1);
  }
});
