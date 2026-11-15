import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Browser, type Page } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 8: analytics, automation and forecasting in the browser. Every
// payment in a development database is a test-provider payment, so the
// dashboards must say so and forecasting must refuse. Rules are switched
// on only to prove the approval gate and are switched off again.

test.describe.configure({ mode: "serial" });

const password = "breadfruit and buljol 2026";
const shot = async (page: Page, name: string) => {
  await page.screenshot({ path: `../docs/screenshots/sprint-8/${name}.png`, fullPage: true });
};
const phone = { width: 390, height: 844 };

async function adminToken(request: APIRequestContext): Promise<string> {
  const res = await request.post(`${urls.api}/api/v1/admin/auth/login`, {
    data: { email: process.env.E2E_ADMIN_EMAIL, password: process.env.E2E_ADMIN_PASSWORD },
  });
  expect(res.status(), await res.text()).toBe(200);
  return (await res.json()).data.token as string;
}

async function call(request: APIRequestContext, token: string, method: string, path: string, data?: object) {
  const res = await request.fetch(`${urls.api}/api/v1/admin${path}`, { method, headers: { Authorization: `Bearer ${token}` }, data });
  expect(res.status(), `${method} ${path}: ${await res.text()}`).toBeLessThan(300);
  return (await res.json()).data;
}

async function staff(browser: Browser, request: APIRequestContext, role: string, name: string, viewport = { width: 1366, height: 850 }) {
  const token = await adminToken(request);
  const email = uniqueEmail(role.replace(/_/g, "-"));
  await call(request, token, "POST", "/staff/invitations", { email, name, role });
  const body = await waitForEmail(request, email, "You're invited to the Mise console");
  const invitation = new URL(linkIn(body, "/accept-invite")).searchParams.get("token");
  const accept = await request.post(`${urls.api}/api/v1/admin/auth/invitations/accept`, { data: { token: invitation, password } });
  expect(accept.status(), await accept.text()).toBe(200);
  const page = await (await browser.newContext({ viewport })).newPage();
  await page.goto(`${urls.admin}/login`);
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("navigation", { name: "Admin" })).toBeVisible();
  return { page, email };
}

async function submit(page: Page, form: string, button: string, message: string | RegExp) {
  const f = page.getByRole("form", { name: form, exact: true });
  await f.getByRole("button", { name: button, exact: true }).click();
  await expect(f.locator("[aria-live=polite]")).toContainText(message, { timeout: 15_000 });
}

async function noSeriousA11yIssues(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations.filter((v) => v.impact === "serious" || v.impact === "critical")).toEqual([]);
}

test("an analyst reads dashboards defined in one catalogue, with test data called out", async ({ browser, request }) => {
  const { page } = await staff(browser, request, "analyst", "Nadia Analyst");
  const nav = page.getByRole("navigation", { name: "Admin" });
  await nav.getByRole("link", { name: "Insights" }).click();
  await expect(page).toHaveURL(/\/intelligence\/overview$/);
  await expect(page.getByRole("heading", { name: "Executive overview" })).toBeVisible();
  await expect(page.getByText("Read these figures with care")).toBeVisible();
  await expect(page.getByText(/simulated test payment provider/).first()).toBeVisible();
  await noSeriousA11yIssues(page);
  await shot(page, "01-executive-overview");

  await page.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: "Revenue" }).click();
  await expect(page.getByRole("heading", { name: "Revenue and subscriptions" })).toBeVisible();
  const first = page.getByText("Definition").first();
  await first.click();
  await expect(page.getByText(/Limits:/).first()).toBeVisible();
  await shot(page, "02-revenue");
  const download = page.waitForEvent("download");
  await page.getByRole("link", { name: "Download CSV" }).click();
  const csv = await (await download).path();
  expect(csv).toBeTruthy();

  for (const tab of ["Customers", "Meals", "Production", "Procurement", "Inventory", "Delivery"]) {
    await page.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: tab, exact: true }).click();
    await expect(page.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: tab, exact: true })).toHaveAttribute("aria-current", "page");
  }
  await shot(page, "03-delivery-analytics");

  await nav.getByRole("link", { name: "Metric catalogue" }).click();
  await expect(page.getByRole("heading", { name: "Metric catalogue" })).toBeVisible();
  await expect(page.getByText("revenue.collected", { exact: true })).toBeVisible();
  await shot(page, "04-metric-catalogue");

  // On a phone the same dashboard stacks into one column.
  const mobile = await (await browser.newContext({ viewport: phone, storageState: await page.context().storageState() })).newPage();
  await mobile.goto(`${urls.admin}/intelligence/overview`);
  await expect(mobile.getByRole("heading", { name: "Executive overview" })).toBeVisible();
  await shot(mobile, "05-overview-phone");
});

test("kitchen staff see no analytics", async ({ browser, request }) => {
  const { page } = await staff(browser, request, "kitchen_staff", "Kai Kitchen");
  await expect(page.getByRole("navigation", { name: "Admin" }).getByRole("link", { name: "Insights" })).toHaveCount(0);
  const res = await page.goto(`${urls.admin}/intelligence/revenue`);
  expect(res?.status()).toBe(404);
});

test("data quality, automation approvals, a scheduled report and a refused forecast", async ({ browser, request }) => {
  const { page } = await staff(browser, request, "admin", "Ola Operations");
  const nav = page.getByRole("navigation", { name: "Admin" });

  await nav.getByRole("link", { name: "Data quality" }).click();
  await submit(page, "Run data-quality checks", "Run checks now", "Checks run.");
  await expect(page.getByRole("cell", { name: /Test-provider payments/ })).toBeVisible();
  await noSeriousA11yIssues(page);
  await shot(page, "06-data-quality");

  // A rule cannot be switched on until the business decides it.
  await nav.getByRole("link", { name: "Rules" }).click();
  const form = page.getByRole("form", { name: "Configure Payment failed", exact: true });
  await expect(page.getByText("Needs a business decision before it can be switched on").first()).toBeVisible();
  await shot(page, "07-automation-rules");
  await form.getByLabel("Rule on").check();
  await form.getByRole("button", { name: "Save rule" }).click();
  await expect(form.getByRole("alert")).toContainText("cannot be enabled yet");
  await form.locator("select[name=action]").selectOption("alert");
  await form.getByLabel(/Cooldown/).fill("1440");
  await form.getByLabel(/Approval reference/).fill("E2E check, switched off after");
  await submit(page, "Configure Payment failed", "Save rule", "Saved and enabled.");
  await submit(page, "Check Payment failed now", "Check now", /execution|Nothing new/);
  await form.getByLabel("Rule on").uncheck();
  await form.getByLabel(/Approval reference/).fill("");
  await submit(page, "Configure Payment failed", "Save rule", "The rule is off.");

  await page.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: "Executions" }).click();
  await expect(page.getByRole("heading", { name: "Automation" })).toBeVisible();
  await shot(page, "08-automation-executions");
  await page.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: "My tasks" }).click();
  await expect(page.getByRole("heading", { name: "Automation" })).toBeVisible();

  // A report runs once per period; generating again is refused.
  await page.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: "Scheduled reports" }).click();
  await shot(page, "09-scheduled-reports");
  const gen = page.getByRole("form", { name: "Generate Weekly production and waste now", exact: true });
  await gen.getByRole("button", { name: "Generate now" }).click();
  await Promise.race([
    page.waitForURL(/\/automation\/reports\/[0-9a-f-]{36}$/, { timeout: 20_000 }),
    expect(gen.getByRole("alert")).toContainText("already generated", { timeout: 20_000 }),
  ]);
  if (!/\/automation\/reports\/[0-9a-f-]{36}$/.test(page.url())) {
    // Already generated for this period: open the stored run instead.
    await page.getByRole("link", { name: "Weekly production and waste" }).first().click();
  }
  await expect(page.getByRole("heading", { name: "Email delivery" })).toBeVisible();
  await shot(page, "10-generated-report");

  // Development data is all test payments: forecasting refuses and records why.
  await nav.getByRole("link", { name: "Forecasting" }).click();
  await expect(page.getByText("Not enough verified historical demand to generate a reliable forecast.").first()).toBeVisible();
  await expect(page.getByRole("cell", { name: "Not decided" }).first()).toBeVisible();
  await noSeriousA11yIssues(page);
  await shot(page, "11-forecast-readiness");
  await page.getByRole("form", { name: "Run a forecast", exact: true }).getByRole("button", { name: "Run forecast" }).click();
  await expect(page).toHaveURL(/\/forecasting\/runs\/[0-9a-f-]{36}$/);
  await expect(page.getByText("This run was refused. Nothing was forecast.")).toBeVisible();
  await shot(page, "12-forecast-refused");
  await page.goto(`${urls.admin}/forecasting/settings`);
  await expect(page.getByLabel("Minimum weeks of verified history")).toHaveValue("");
  await shot(page, "13-forecast-criteria");
});
