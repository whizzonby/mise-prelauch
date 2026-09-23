import { expect, test } from "@playwright/test";

import { urls } from "../playwright.config";
import { createLeadViaApi, fillWaitlist, submitWaitlist, uniqueEmail, verifyByEmail, verifyViaApi } from "./helpers";

test("a visitor joins the waitlist, confirms their email and reaches their Mise page", async ({ page, request }) => {
  const email = uniqueEmail("visitor");
  await page.goto("/?utm_source=e2e&utm_campaign=waitlist-test");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(/Everything\s+in its place\./);

  await page.getByRole("link", { name: "Join the waitlist" }).first().click();
  await submitWaitlist(page, { name: "Asha", email });

  // Success page, personalised, and honest that one step remains.
  await expect(page).toHaveURL(/\/welcome$/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("One step left, Asha");
  const link = page.getByLabel("Your invitation link");
  await expect(link).toHaveValue(/\/join\/[A-Z0-9]{7}$/);
  // No queue position is shown, because none is calculated.
  await expect(page.getByText(/position|#\d+ in line/i)).toHaveCount(0);

  await verifyByEmail(page, request, email);
  await page.getByRole("link", { name: "Open my Mise page" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("You're on the Mise list, Asha");

  // The optional profiling questions save, and can be left partly blank.
  await page.getByText("A family", { exact: true }).click();
  await page.getByText("Weeknight dinners", { exact: true }).click();
  await page.getByRole("button", { name: "Save my answers" }).click();
  await expect(page.getByText("Answers saved. Thank you.")).toBeVisible();
});

test("an invitation link credits the friend who shared it, once the new person confirms", async ({ page, request }) => {
  // Asha is already on the list, from a different network than the browser.
  const asha = await createLeadViaApi(request, { name: "Asha", email: uniqueEmail("referrer"), ip: "203.0.113.10" });
  const code = asha.lead.referral_code;

  // Ben follows her link.
  const benEmail = uniqueEmail("friend");
  await page.goto(`/join/${code.toLowerCase()}`);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Asha saved you a place at the table");
  await submitWaitlist(page, { name: "Ben", email: benEmail });
  await expect(page).toHaveURL(/\/welcome$/);

  // Until Ben confirms his email he shows as waiting, not as joined.
  const ashaPage = await page.context().browser()!.newPage();
  await ashaPage.goto(`${urls.site}/welcome?token=${asha.profile_token}`);
  await expect(ashaPage.getByText("Waiting to confirm their email").locator("xpath=following-sibling::dd")).toHaveText("1");
  await expect(ashaPage.getByText("Friends who have joined").locator("xpath=following-sibling::dd")).toHaveText("0");
  // The token is taken out of the address bar.
  await expect(ashaPage).toHaveURL(/\/welcome$/);

  await verifyViaApi(request, benEmail);
  await ashaPage.reload();
  await expect(ashaPage.getByText("Friends who have joined").locator("xpath=following-sibling::dd")).toHaveText("1");
  await expect(ashaPage.getByText("Waiting to confirm their email").locator("xpath=following-sibling::dd")).toHaveText("0");
  await ashaPage.close();
});

test("an unknown invitation code still lets the visitor join", async ({ page }) => {
  await page.goto("/join/ZZZZZZZ");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Save your place at the table");
  await expect(page.getByText("This invitation link is not valid")).toBeVisible();
  await expect(page.getByRole("form", { name: "Join the Mise waitlist" })).toBeVisible();
});

test("invalid details are explained in place and nothing is submitted", async ({ page }) => {
  await page.goto("/#waitlist");
  const form = page.getByRole("form", { name: "Join the Mise waitlist" });
  await form.getByRole("button", { name: "Join the waitlist" }).click();

  await expect(form.getByText("Enter your first name.")).toBeVisible();
  await expect(form.getByText("Enter your email address.")).toBeVisible();
  await expect(form.getByText("Choose where you would like delivery.")).toBeVisible();
  await expect(form.getByText("Tick the box so we can email you about the launch.")).toBeVisible();

  await form.getByLabel("Email", { exact: true }).fill("not-an-email");
  await form.getByLabel("First name").fill("Asha");
  await expect(form.getByText("Enter a valid email address, like name@example.com.")).toBeVisible();
  await expect(form.getByLabel("Email", { exact: true })).toHaveAttribute("aria-invalid", "true");
  await expect(page).toHaveURL(/#waitlist$/);
});

test("joining twice with the same address is handled without creating a second lead", async ({ page, request }) => {
  const email = uniqueEmail("twice");
  await createLeadViaApi(request, { name: "Asha", email, ip: "203.0.113.20" });

  await page.goto("/#waitlist");
  // Same inbox, written differently.
  const form = await fillWaitlist(page, { name: "Asha", email: email.replace("@", "+again@").toUpperCase() });
  await form.getByRole("button", { name: "Join the waitlist" }).click();

  await expect(page.getByText("You are already on the list")).toBeVisible();
  await expect(page).toHaveURL(/#waitlist$/);
});
