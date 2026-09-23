import { expect, test } from "@playwright/test";

import { urls } from "../playwright.config";
import { createLeadViaApi, signInToAdmin, uniqueEmail, verifyViaApi } from "./helpers";

test("admin pages are closed to visitors who are not signed in", async ({ page, request }) => {
  for (const path of ["/", "/leads", "/leads/00000000-0000-0000-0000-000000000000"]) {
    await page.goto(`${urls.admin}${path}`);
    await expect(page).toHaveURL(/\/login/);
  }
  const exportResponse = await request.get(`${urls.admin}/leads/export`);
  expect(exportResponse.status()).toBe(401);

  await page.getByLabel("Email", { exact: true }).fill("admin@mise.local");
  await page.getByLabel("Password").fill("definitely-the-wrong-password");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Email or password is incorrect." })).toBeVisible();
});

test("an admin signs in, reads the dashboard, finds a lead and exports the list", async ({ page, request }) => {
  const email = uniqueEmail("lead");
  await createLeadViaApi(request, { name: "Kavita", email, ip: "203.0.113.30" });
  await verifyViaApi(request, email);

  await signInToAdmin(page);

  // Dashboard: headline numbers and every breakdown the team asked for.
  for (const label of ["Total leads", "Verified leads", "Referral conversions", "Profiles completed"]) {
    await expect(page.getByText(label, { exact: true })).toBeVisible();
  }
  for (const title of ["Signups over time", "Top acquisition sources", "Locations", "Dietary interests", "Household size"]) {
    await expect(page.getByRole("heading", { name: title })).toBeVisible();
  }

  // Leads: search finds exactly the lead just created.
  await page.getByRole("link", { name: "Leads" }).click();
  await page.getByLabel("Name or email").fill(email);
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page.getByRole("status").filter({ hasText: "1 lead match" })).toBeVisible();
  const row = page.getByRole("row").filter({ hasText: email });
  await expect(row).toContainText("Verified");

  // Lead detail.
  await row.getByRole("link", { name: "Kavita" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Kavita" })).toBeVisible();
  for (const title of ["Profile", "Preferences", "Attribution", "Referral history", "Activity"]) {
    await expect(page.getByRole("heading", { name: title, exact: true })).toBeVisible();
  }
  await expect(page.getByRole("link", { name: email })).toBeVisible();

  // Status change.
  await page.getByLabel("New status").selectOption("QUALIFIED");
  await page.getByRole("button", { name: "Save status" }).click();
  await expect(page.getByText("Status changed from VERIFIED to QUALIFIED by an admin")).toBeVisible();

  // Export downloads a CSV containing the lead.
  await page.goto(`${urls.admin}/leads?q=${encodeURIComponent(email)}`);
  const download = page.waitForEvent("download");
  await page.getByRole("link", { name: /Export these leads as CSV/ }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(/^mise-leads-\d{8}-\d{4}\.csv$/);
  const stream = await file.createReadStream();
  let csv = "";
  for await (const chunk of stream) csv += chunk;
  expect(csv.split("\n")[0]).toContain("id,first_name,email");
  expect(csv).toContain(email);

  // Signing out ends the session.
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
  await page.goto(`${urls.admin}/leads`);
  await expect(page).toHaveURL(/\/login/);
});
