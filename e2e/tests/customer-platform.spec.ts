import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Page } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 2: the customer platform (apps/web) and catalogue publishing (apps/admin).
// Needs the demo catalogue: `misectl catalog seed-demo` with CATALOG_DEMO_ENABLED.

const password = "plantain and saltfish 2026";

/** A confirmed account, created through the API, then signed in through the app. */
async function newCustomer(page: Page, request: APIRequestContext, prefix: string) {
  const email = uniqueEmail(prefix);
  const res = await request.post(`${urls.api}/api/v1/auth/register`, {
    data: { email, password, first_name: "Kavita", last_name: "Ramdass", accept_terms: true },
  });
  expect(res.status(), await res.text()).toBe(202);
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  const token = new URL(linkIn(body, "/verify-email")).searchParams.get("token");
  expect((await request.post(`${urls.api}/api/v1/auth/verify-email`, { data: { token } })).status()).toBe(200);
  await page.goto(`${urls.web}/sign-in`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/onboarding$/);
  return email;
}

test("onboarding saves each answer, resumes after a reload, and refuses contradictions", async ({ page, request }) => {
  await newCustomer(page, request, "onboard");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Welcome to Mise, Kavita.");
  await page.getByRole("button", { name: "Let’s get started" }).click();

  await expect(page.getByRole("heading", { name: "Who are you cooking for?" })).toBeFocused();
  await expect(page.getByText("Question 1 of 5")).toBeVisible();
  await page.getByText("Two people", { exact: true }).click();
  await page.getByRole("button", { name: "Continue" }).click();

  await expect(page.getByRole("heading", { name: "What kind of meals interest you?" })).toBeVisible();
  await page.getByText("Caribbean classics", { exact: true }).click();
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByRole("heading", { name: "Any dietary preferences?" })).toBeVisible();

  // Resumable: a reload (or another device) picks up at the same question.
  await page.reload();
  await expect(page.getByRole("heading", { name: "Any dietary preferences?" })).toBeVisible();

  await page.getByText("Vegan", { exact: true }).click();
  await page.getByText("Pescatarian", { exact: true }).click();
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "do not go together" })).toBeVisible();
  await page.getByText("Pescatarian", { exact: true }).click();
  await page.getByRole("button", { name: "Continue" }).click();

  await expect(page.getByRole("heading", { name: "Allergies and restrictions" })).toBeVisible();
  await expect(page.getByText("does not guarantee a meal is free of an allergen")).toBeVisible();
  await page.getByText("Peanuts", { exact: true }).click();
  await page.getByRole("button", { name: "Continue" }).click();

  // Back keeps what was saved.
  await expect(page.getByRole("heading", { name: "Where would you like deliveries?" })).toBeVisible();
  await page.getByRole("button", { name: "Back" }).click();
  await expect(page.getByLabel("Peanuts")).toBeChecked();
  await page.getByRole("button", { name: "Continue" }).click();

  await page.getByLabel("Region").selectOption("chaguanas");
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByRole("heading", { name: "Your Mise kitchen is ready." })).toBeVisible();
  // Mise delivers across Trinidad and Tobago (migration 00006).
  await page.goto(`${urls.web}/profile`);
  await expect(page.getByText("Mise delivers here.")).toBeVisible();
  await page.goto(`${urls.web}/onboarding`);
  await page.getByRole("link", { name: "Explore meals" }).click();
  await expect(page).toHaveURL(/\/meals$/);

  // The answers are the customer's records now.
  await page.goto(`${urls.web}/profile/preferences`);
  await expect(page.getByRole("form", { name: "Food preferences" }).getByLabel("Vegan")).toBeChecked();
  await expect(page.getByRole("form", { name: "Allergies" }).getByLabel("Peanuts")).toBeChecked();
  await page.goto(`${urls.web}/profile/household`);
  await expect(page.getByLabel("Number of people")).toHaveValue("2");
});

test("the dashboard suggests meals, and meals can be filtered, opened and saved", async ({ page, request }) => {
  await newCustomer(page, request, "browse");
  await page.getByRole("link", { name: "Finish later" }).click();

  await expect(page.getByRole("heading", { level: 1 })).toHaveText(/^Good (morning|afternoon|evening), Kavita\.$/);
  await expect(page.getByRole("heading", { name: "Finish setting up your kitchen" })).toBeVisible();
  // Nothing invented: no orders or deliveries for someone without a
  // subscription, just a way to see the plans (Sprint 3).
  await expect(page.locator("main").getByText(/next delivery|your order|your next week/i)).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Start cooking with Mise" })).toBeVisible();

  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Explore meals" }).click();
  await expect(page).toHaveURL(/\/meals$/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Explore meals");
  await expect(page.getByText(/^\d+ meals?$/)).toBeVisible();

  await page.getByRole("search", { name: "Filter meals" }).getByText("Vegan", { exact: true }).click();
  await expect(page).toHaveURL(/diet=vegan/);
  await expect(page.getByRole("heading", { level: 3 })).toHaveText(["Fried ripe plantain", "Curried channa and rice"], { useInnerText: true });

  await page.getByRole("link", { name: "Clear all filters" }).click();
  await expect(page).toHaveURL(/\/meals$/);
  await expect(page.getByRole("search", { name: "Filter meals" }).getByLabel("Vegan")).not.toBeChecked();
  await page.getByLabel("Search", { exact: true }).fill("pumpkin");
  await page.getByLabel("Search", { exact: true }).press("Enter");
  await expect(page).toHaveURL(/q=pumpkin/);
  await expect(page.getByRole("heading", { level: 3 })).toHaveText(["Pumpkin soup"]);

  await page.getByRole("link", { name: "Pumpkin soup" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Pumpkin soup");
  await expect(page.getByText("Demo content", { exact: true })).toBeVisible();
  await expect(page.getByText("Allergen information for this meal has not been confirmed yet")).toBeVisible();
  await expect(page.getByText("Nutrition figures are published once the Mise nutritionist has checked")).toBeVisible();

  const save = page.getByRole("button", { name: "Save this meal" });
  await save.click();
  await expect(page.getByRole("button", { name: "Saved" })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("button", { name: "Saved" })).toHaveAttribute("aria-busy", "false");

  await page.goto(`${urls.web}/favorites`);
  await expect(page.getByRole("heading", { level: 3 })).toHaveText(["Pumpkin soup"]);
  await page.reload();
  await page.getByRole("button", { name: "Remove Pumpkin soup from saved meals" }).click();
  // The list refreshes without it, and stays that way after a reload.
  await expect(page.getByRole("heading", { name: "Your saved meals will appear here." })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("heading", { name: "Your saved meals will appear here." })).toBeVisible();
});

test("addresses, email preferences and household are managed from the account", async ({ page, request }) => {
  await newCustomer(page, request, "account");
  await page.goto(`${urls.web}/profile/addresses`);

  const add = page.getByRole("form", { name: "Add an address" });
  await add.getByRole("button", { name: "Add address" }).click();
  await expect(add.getByText("Some details need attention.")).toBeVisible();
  await add.getByLabel("Label").fill("Home");
  await add.getByLabel("Who receives deliveries").fill("Kavita Ramdass");
  await add.getByLabel("Street address").fill("12 Ariapita Avenue");
  await add.getByLabel("Town or city").fill("Woodbrook");
  await add.getByLabel("Region").selectOption("port-of-spain");
  await add.getByRole("button", { name: "Add address" }).click();
  await expect(page.getByText("Address added at")).toBeVisible();
  await expect(page.getByRole("listitem").filter({ hasText: "12 Ariapita Avenue" }).getByText("Default", { exact: true })).toBeVisible();
  // Delivery is confirmed per region (every region of Trinidad and Tobago, migration 00006).
  await expect(page.getByText("Mise delivers here.").first()).toBeVisible();
  await expect(add.getByLabel("Street address")).toHaveValue("");

  await add.getByLabel("Label").fill("Work");
  await add.getByLabel("Who receives deliveries").fill("Kavita Ramdass");
  await add.getByLabel("Street address").fill("1 Harris Promenade");
  await add.getByLabel("Town or city").fill("San Fernando");
  await add.getByLabel("Region").selectOption("san-fernando");
  await add.getByLabel("Make this my default address").check();
  await add.getByRole("button", { name: "Add address" }).click();
  await expect(page.getByRole("listitem").filter({ hasText: "1 Harris Promenade" }).getByText("Default", { exact: true })).toBeVisible();
  await expect(page.getByRole("listitem").filter({ hasText: "12 Ariapita Avenue" }).getByText("Default", { exact: true })).toHaveCount(0);

  await page.goto(`${urls.web}/profile/notifications`);
  await expect(page.getByText("Always on")).toBeVisible();
  await page.getByLabel("Offers and promotions").check();
  await page.getByRole("button", { name: "Save email preferences" }).click();
  await expect(page.getByText(/^Saved at /)).toBeVisible();
  await page.reload();
  await expect(page.getByLabel("Offers and promotions")).toBeChecked();

  await page.goto(`${urls.web}/profile/household`);
  await page.getByLabel("Number of people").fill("2");
  await page.getByLabel("Adults").fill("2");
  await page.getByLabel("Children").fill("1");
  await page.getByRole("button", { name: "Save household" }).click();
  await expect(page.getByText("The household is smaller than the adults and children together.")).toBeVisible();
});

test("a protected page sends a signed-out visitor to sign in and back again", async ({ page, request }) => {
  await page.goto(`${urls.web}/meals/pumpkin-soup`);
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fmeals%2Fpumpkin-soup$/);
  // A sign-in link cannot be used to bounce someone to another site.
  await page.goto(`${urls.web}/sign-in?next=//evil.example/`);
  const email = uniqueEmail("deeplink");
  await request.post(`${urls.api}/api/v1/auth/register`, {
    data: { email, password, first_name: "Kavita", last_name: "Ramdass", accept_terms: true },
  });
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  await request.post(`${urls.api}/api/v1/auth/verify-email`, { data: { token: new URL(linkIn(body, "/verify-email")).searchParams.get("token") } });
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(`${urls.web}/dashboard`);

  await page.context().clearCookies();
  await page.goto(`${urls.web}/sign-in?next=%2Fmeals%2Fpumpkin-soup`);
  await page.getByLabel("Email").fill(email);
  const pw = page.getByLabel("Password", { exact: true });
  await pw.fill(password);
  await page.getByRole("button", { name: "Show password" }).click();
  await expect(pw).toHaveAttribute("type", "text");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(`${urls.web}/meals/pumpkin-soup`);
});

test("staff publish a meal from the admin; a draft needs a photograph first", async ({ page }) => {
  await page.goto(`${urls.admin}/login`);
  await page.getByLabel("Email", { exact: true }).fill(process.env.E2E_ADMIN_EMAIL!);
  await page.getByLabel("Password").fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.getByRole("navigation", { name: "Admin" }).getByRole("link", { name: "Meals" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Meals" })).toBeVisible();

  const name = `Test callaloo ${Date.now().toString(36)}`;
  await page.getByRole("link", { name: "New meal" }).click();
  const form = page.getByRole("form", { name: "New meal" });
  await form.getByLabel("Name", { exact: true }).fill(name);
  await form.getByLabel(/^Summary/).fill("Dasheen leaves and okra cooked down with coconut milk.");
  await form.getByLabel("Category").selectOption({ label: "Caribbean classics (demo)" });
  await form.getByRole("button", { name: "Create draft" }).click();
  await expect(page.getByText("Draft created.")).toBeVisible();

  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Add at least one photograph" })).toBeVisible();

  await page.getByLabel(/^Photographs/).fill("/catalog/demo/meal-pumpkin-soup.jpg | A pot of green callaloo | test");
  await page.getByRole("button", { name: "Save meal" }).click();
  await expect(page.getByText("Saved.")).toBeVisible();
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("Published. Customers can see it now.")).toBeVisible();
  await page.getByRole("button", { name: "Unpublish" }).click();
  await expect(page.getByText("Unpublished. Customers no longer see it.")).toBeVisible();
});

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("the bottom tab bar reaches every section and nothing scrolls sideways", async ({ page, request }) => {
    await newCustomer(page, request, "phone");
    await page.getByRole("link", { name: "Finish later" }).click();
    const tabs = page.getByRole("navigation", { name: "Main" });
    for (const [label, path] of [["Meals", "/meals"], ["Saved", "/favorites"], ["Account", "/profile"], ["Home", "/dashboard"]] as const) {
      await tabs.getByRole("link", { name: label }).click();
      await expect(page).toHaveURL(new RegExp(`${path}$`));
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow, `sideways scroll on ${path}`).toBeLessThanOrEqual(0);
    }
  });
});

test("customer pages have no automatically detectable accessibility problems", async ({ page, request }) => {
  await newCustomer(page, request, "axe");
  for (const path of ["/onboarding", "/dashboard", "/meals", "/meals/curried-channa-and-rice", "/favorites", "/profile", "/profile/preferences", "/profile/addresses", "/profile/notifications", "/profile/security"]) {
    await page.goto(`${urls.web}${path}`);
    const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze();
    expect(results.violations.map((v) => `${path} ${v.id}: ${v.help} (${v.nodes.length})`)).toEqual([]);
  }
});
