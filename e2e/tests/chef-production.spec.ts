import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Browser, type Page } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, signInToAdmin, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 5: the chef's recipe workflow, and the kitchen taking a released
// batch through tasks, output, a quality hold, packing and completion; role
// limits; phone width. Needs the worker (invitations) and released demo
// batches, which only a moved clock can make (see the README):
//   misectl catalog seed-demo && misectl commerce seed-demo && misectl production seed-demo
// Each run uses up one released batch. The E2E admin must be a super admin.

const password = "plantain and saltfish 2026";

async function adminToken(request: APIRequestContext): Promise<string> {
  const res = await request.post(`${urls.api}/api/v1/admin/auth/login`, {
    data: { email: process.env.E2E_ADMIN_EMAIL, password: process.env.E2E_ADMIN_PASSWORD },
  });
  expect(res.status(), await res.text()).toBe(200);
  return (await res.json()).data.token as string;
}

/** A staff member with one role, invited and signed in to the console in their own browser. */
async function staff(browser: Browser, request: APIRequestContext, role: string, name: string): Promise<Page & { email: string }> {
  const token = await adminToken(request);
  const email = uniqueEmail(role.replace(/_/g, "-"));
  const invite = await request.post(`${urls.api}/api/v1/admin/staff/invitations`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { email, name, role },
  });
  expect(invite.status(), await invite.text()).toBe(201);
  const body = await waitForEmail(request, email, "You're invited to the Mise console");
  const invitation = new URL(linkIn(body, "/accept-invite")).searchParams.get("token");
  const accept = await request.post(`${urls.api}/api/v1/admin/auth/invitations/accept`, { data: { token: invitation, password } });
  expect(accept.status(), await accept.text()).toBe(200);

  const page = await (await browser.newContext({ viewport: { width: 1366, height: 850 } })).newPage();
  await page.goto(`${urls.admin}/login`);
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("navigation", { name: "Admin" })).toBeVisible();
  return Object.assign(page, { email });
}

/** A released demo batch nobody has started, newest first. */
async function releasedBatch(request: APIRequestContext): Promise<{ id: string; code: string; meal_name: string }> {
  const token = await adminToken(request);
  const res = await request.get(`${urls.api}/api/v1/admin/production/batches`, { headers: { Authorization: `Bearer ${token}` }, params: { status: "released" } });
  expect(res.status(), await res.text()).toBe(200);
  const items = (await res.json()).data.items as { id: string; code: string; meal_name: string }[];
  expect(items.length, "No released batch is left: run `misectl production seed-demo` (see the README).").toBeGreaterThan(0);
  return items[items.length - 1]!;
}

async function noSeriousA11yIssues(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations.filter((v) => v.impact === "serious" || v.impact === "critical")).toEqual([]);
}

/**
 * Submits a KForm by its accessible name and waits for its saved message.
 * Many actions move the record on, and the page then shows the next step
 * instead of this form: the form going away also counts, as long as no
 * error was shown first.
 */
async function submit(page: Page, form: string, button: string, message: string) {
  const f = page.getByRole("form", { name: form, exact: true });
  await f.getByRole("button", { name: button, exact: true }).click();
  await expect(async () => {
    if ((await f.count()) === 0) return;
    await expect(f.getByRole("alert")).toHaveCount(0);
    await expect(f.locator("[aria-live=polite]")).toContainText(message, { timeout: 500 });
  }).toPass({ timeout: 15_000 });
}

test("a chef drafts a recipe step by step; the culinary team, a nutritionist and the director take it to publication", async ({ page, request, browser }) => {
  const chef = await staff(browser, request, "chef", "Marisa Chef");
  const name = `Coconut bake ${Date.now().toString(36)}`;

  // The chef's workspace starts on "My recipes".
  await chef.goto(`${urls.admin}/recipes`);
  await expect(chef.getByRole("heading", { level: 1, name: "My recipes" })).toBeVisible();
  const start = chef.getByRole("form", { name: "Start a recipe" });
  await start.getByLabel("Name").fill(name);
  await start.getByRole("button", { name: "Start recipe" }).click();
  await expect(chef.getByRole("heading", { level: 1, name })).toBeVisible();
  await expect(chef).toHaveURL(/step=ingredients/);

  // Ingredients: the bill of materials, converted to each ingredient's base unit.
  const bom = chef.getByRole("form", { name: "Ingredients" });
  await bom.getByLabel("Line 1: ingredient").selectOption({ label: "Demo long-grain rice (g)" });
  await bom.getByLabel("Quantity").first().fill("0.3");
  await bom.getByLabel("Unit").first().selectOption("kg");
  await bom.getByLabel("Line 2: ingredient").selectOption({ label: "Demo onion (g)" });
  await bom.getByLabel("Quantity").nth(1).fill("150");
  await bom.getByLabel("Preparation").nth(1).fill("sliced");
  await submit(chef, "Ingredients", "Save ingredients", "Saved. Earlier reviews of this version no longer count.");

  // Submitting without a method is refused, with the reason.
  const steps = chef.getByRole("navigation", { name: "Recipe steps" });
  await steps.getByRole("link", { name: "Review" }).click();
  await chef.getByRole("form", { name: "Submit for review" }).getByRole("button", { name: "Submit for review" }).click();
  await expect(chef.getByRole("form", { name: "Submit for review" }).getByRole("alert")).toBeVisible();

  await steps.getByRole("link", { name: "Method" }).click();
  await chef.getByLabel("Kitchen preparation steps").fill("Weigh the rice into its bag.\n\nSlice the onion and bag it.");
  await chef.getByLabel("Storage instructions").fill("Keep chilled.");
  await submit(chef, "Method", "Save method", "Saved. Earlier reviews of this version no longer count.");

  await steps.getByRole("link", { name: "Allergens & nutrition" }).click();
  const safety = chef.getByRole("form", { name: "Allergens and nutrition" });
  for (const [label, value] of [
    ["Energy (kcal)", "410"],
    ["Protein (g)", "9"],
    ["Carbohydrate (g)", "80"],
    ["Fat (g)", "6"],
    ["Saturated fat (g)", "4.5"],
    ["Fibre (g)", "3"],
    ["Sugars (g)", "4"],
    ["Sodium (mg)", "380"],
  ] as const) {
    await safety.getByLabel(label, { exact: true }).fill(value);
  }
  await safety.getByLabel("Source of the figures").selectOption("laboratory_analysis");
  await safety.getByLabel("Method or reference").fill("Lab report E2E-1");
  await submit(chef, "Allergens and nutrition", "Save allergens and nutrition", "Saved. Earlier reviews of this version no longer count.");
  await expect(chef.getByText("Complete, not yet verified")).toBeVisible();

  await steps.getByRole("link", { name: "Review" }).click();
  await submit(chef, "Submit for review", "Submit for review", "Submitted for review.");
  const recipeUrl = chef.url().replace(/\?.*$/, "");
  // Submitted: locked for the chef, and nothing more for them to do.
  await chef.reload();
  await expect(chef.getByText("Nothing for you to do on this version.")).toBeVisible();
  // Chefs approve nothing.
  await expect(chef.getByRole("form", { name: "Final approval" })).toHaveCount(0);

  // The culinary review, the nutrition sign-off and the final approval are
  // each someone else's: the super admin holds all three.
  await signInToAdmin(page);
  await page.goto(`${recipeUrl}?step=review`);
  await submit(page, "Culinary review", "Approve (to nutrition review)", "Culinary review recorded.");
  await page.reload();
  // Final approval waits for the nutrition sign-off.
  await page.getByRole("form", { name: "Final approval" }).getByRole("button", { name: "Approve this version" }).click();
  await expect(page.getByRole("form", { name: "Final approval" }).getByRole("alert")).toBeVisible();
  await page.getByRole("form", { name: "Nutrition sign-off" }).getByLabel("Review notes").fill("Checked against lab report E2E-1.");
  await submit(page, "Nutrition sign-off", "Sign off nutrition and allergens", "Nutrition and allergens signed off.");
  await page.reload();
  await submit(page, "Final approval", "Approve this version", "Approved. This version can no longer change.");
  await page.reload();
  await submit(page, "Publish", "Publish", "Published for production.");
  await page.reload();
  await expect(page.getByText(/production uses v1/)).toBeVisible();
  await noSeriousA11yIssues(page);

  // The review history shows each stage.
  await expect(page.getByText(/Culinary review: Approved/)).toBeVisible();
  await expect(page.getByText(/Nutrition review: Approved/)).toBeVisible();

  // Published versions never change: the chef can only start a revision.
  await chef.goto(`${recipeUrl}?step=method`);
  await expect(chef.getByText(/is published and cannot be edited/)).toBeVisible();
  await expect(chef.getByRole("form", { name: "Method" })).toHaveCount(0);
});

test("the kitchen takes a released batch through tasks, output, a quality hold, packing and completion", async ({ request, browser }) => {
  const batch = await releasedBatch(request);
  const cook = await staff(browser, request, "kitchen_staff", "Devon Cook");
  const qa = await staff(browser, request, "quality_inspector", "Anika Inspector");
  const url = `${urls.admin}/production/batches/${batch.id}`;

  await cook.goto(url);
  await expect(cook.getByRole("heading", { level: 1, name: batch.meal_name })).toBeVisible();
  await expect(cook.getByText("Start the batch to work its tasks.").first()).toBeVisible();
  await noSeriousA11yIssues(cook);
  await cook.getByRole("form", { name: "Start batch" }).getByLabel("Team (optional)").fill("Line A");
  await submit(cook, "Start batch", "Start batch", "Batch started.");

  // Tasks: take each one, then finish it. A batch with an unfinished task
  // cannot complete.
  await cook.reload();
  const startForms = cook.getByRole("form", { name: /^Start / });
  const tasks = await startForms.count();
  expect(tasks, "the batch has tasks from the configured templates").toBeGreaterThan(0);
  for (let i = 0; i < tasks; i++) {
    const taskName = ((await startForms.first().getAttribute("aria-label")) ?? "").replace(/^Start /, "");
    await submit(cook, `Start ${taskName}`, "Take and start", "Task updated.");
    await cook.reload();
    if (i === 0) {
      // Still on the list of what completion needs while it is open.
      await expect(cook.getByText(`Task not done: ${taskName}.`)).toBeVisible();
    }
    await submit(cook, `Finish ${taskName}`, "Done", "Task done.");
    await cook.reload();
  }
  await expect(cook.getByText(/^Task not done:/)).toHaveCount(0);

  // Output: everything planned was made.
  await cook.reload();
  await submit(cook, "Record output", "Record output", "Output recorded.");

  // The inspector fails the critical check: the batch is held.
  await qa.goto(url);
  const critical = qa.getByRole("listitem").filter({ hasText: "Critical" }).first();
  const checkName = ((await critical.getByRole("form").getAttribute("aria-label")) ?? "").replace(/^Check: /, "");
  await critical.getByLabel("Notes (required to fail)").fill("Label is missing from two bags.");
  await submit(qa, `Check: ${checkName}`, "Fail", "Check failed and recorded.");
  await qa.reload();
  await expect(qa.getByText("On quality hold: cannot be released for fulfilment")).toBeVisible();

  // The hold blocks the kitchen: no completion, and packing is refused.
  await cook.reload();
  await expect(cook.getByText("On quality hold: cannot be released for fulfilment")).toBeVisible();
  await expect(cook.getByRole("form", { name: "Complete batch" })).toHaveCount(0);
  await expect(cook.getByRole("form", { name: "Resolve hold" })).toHaveCount(0);
  const packing = cook.getByRole("form", { name: "Packing" });
  await packing.getByLabel("Labels checked against the recipe").check();
  await packing.getByRole("button", { name: "Finish packing" }).click();
  await expect(packing.getByRole("alert")).toBeVisible();

  // Only the inspector releases the hold, and the checks start again.
  await qa.getByRole("form", { name: "Resolve hold" }).getByLabel("Resolution").fill("Labels reprinted from the approved recipe and applied.");
  await submit(qa, "Resolve hold", "Release hold", "Hold released. The failed checks must be inspected again.");
  await qa.reload();
  const checks = qa.getByRole("form", { name: /^Check: / });
  const count = await checks.count();
  expect(count).toBeGreaterThan(0);
  for (let i = 0; i < count; i++) {
    const name = (await checks.nth(i).getAttribute("aria-label"))!;
    await submit(qa, name, "Pass", "Check passed.");
  }
  await qa.reload();
  await expect(qa.getByRole("listitem").filter({ hasText: checkName }).getByText("Passed")).toBeVisible();

  // Packed = accepted, labels verified, then complete.
  await cook.reload();
  const pack = cook.getByRole("form", { name: "Packing" });
  await pack.getByLabel("Packaging used").fill("Insulated bag");
  await pack.getByLabel("Labels checked against the recipe").check();
  await submit(cook, "Packing", "Finish packing", "Packing complete.");
  await cook.reload();
  await submit(cook, "Complete batch", "Complete batch", "Batch complete: ready for fulfilment.");
  await cook.reload();
  await expect(cook.getByText("Completed").first()).toBeVisible();
  await expect(cook.getByText(/Hold released/).first()).toBeVisible();

  // Waste is recorded as it happens.
  const waste = cook.getByRole("form", { name: "Record waste" });
  await waste.getByLabel("What").fill("Rice");
  await waste.getByLabel("Quantity").fill("120");
  await waste.getByLabel("Reason").fill("Spilled while bagging");
  await submit(cook, "Record waste", "Record waste", "Waste recorded.");
});

test("each kitchen role sees and does only its own part", async ({ request, browser }) => {
  const cook = await staff(browser, request, "kitchen_staff", "Ravi Cook");
  const nav = cook.getByRole("navigation", { name: "Admin" });
  await expect(nav.getByRole("link", { name: "Kitchen tasks" })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Batches" })).toBeVisible();
  for (const hidden of ["Recipes", "Kitchen settings", "Recipe approvals", "Customers", "All customers"]) {
    await expect(nav.getByRole("link", { name: hidden, exact: true })).toHaveCount(0);
  }
  // Pages are closed, not just hidden.
  await cook.goto(`${urls.admin}/recipes`);
  await expect(cook.getByRole("heading", { level: 1, name: "Recipes" })).toHaveCount(0);
  await cook.goto(`${urls.admin}/production/settings`);
  await expect(cook.getByRole("heading", { level: 1, name: "Kitchen settings" })).toHaveCount(0);
  // Plans are visible to the kitchen, but only managers plan.
  await cook.goto(`${urls.admin}/production/plans`);
  await expect(cook.getByRole("heading", { level: 1, name: "Production plans" })).toBeVisible();
  await expect(cook.getByRole("button", { name: "Take snapshot" })).toHaveCount(0);
  await expect(cook.getByRole("button", { name: "Create draft plan" })).toHaveCount(0);

  // A chef has no quality control and cannot approve.
  const chef = await staff(browser, request, "chef", "Kerry Chef");
  const chefNav = chef.getByRole("navigation", { name: "Admin" });
  await expect(chefNav.getByRole("link", { name: "Recipes", exact: true })).toBeVisible();
  for (const hidden of ["Quality control", "Recipe approvals", "Kitchen settings", "Packaging"]) {
    await expect(chefNav.getByRole("link", { name: hidden, exact: true })).toHaveCount(0);
  }
  await chef.goto(`${urls.admin}/recipe-approvals`);
  await expect(chef.getByRole("heading", { level: 1, name: "Recipe approvals" })).toHaveCount(0);

  // The API refuses too, whatever the screen shows.
  const login = await request.post(`${urls.api}/api/v1/admin/auth/login`, { data: { email: cook.email, password } });
  const cookToken = (await login.json()).data.token as string;
  const plan = await request.post(`${urls.api}/api/v1/admin/production/plans`, { headers: { Authorization: `Bearer ${cookToken}` }, data: { week: "2026-11-23" } });
  expect(plan.status()).toBe(403);
  const recipe = await request.post(`${urls.api}/api/v1/admin/recipes`, { headers: { Authorization: `Bearer ${cookToken}` }, data: { name: "Not allowed" } });
  expect(recipe.status()).toBe(403);
});

test("the kitchen screens work on a phone", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await signInToAdmin(page);
  for (const path of ["/production", "/production/tasks", "/production/plans", "/production/batches", "/production/quality", "/recipes", "/ingredients", "/reports/production"]) {
    await page.goto(`${urls.admin}${path}`);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, path).toBeLessThanOrEqual(1);
  }
});
