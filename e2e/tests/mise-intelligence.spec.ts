import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Browser, type Page } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, signInToAdmin, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 9.2: Mise Intelligence in the browser. No AI provider is configured
// in development, so the assistant must say "AI unavailable" while every
// deterministic view (briefings, scenarios, risks, finance) works. The
// development database holds only test-provider payments and no expenses,
// so profit stays withheld and the scenario needs entered assumptions.

test.describe.configure({ mode: "serial" });

const password = "breadfruit and buljol 2026";
const shot = async (page: Page, name: string) => {
  await page.screenshot({ path: `../docs/screenshots/sprint-9-2/e2e-${name}.png`, fullPage: true });
};
const CC = `${urls.admin}/intelligence/command-center`;

async function adminToken(request: APIRequestContext): Promise<string> {
  const res = await request.post(`${urls.api}/api/v1/admin/auth/login`, {
    data: { email: process.env.E2E_ADMIN_EMAIL, password: process.env.E2E_ADMIN_PASSWORD },
  });
  expect(res.status(), await res.text()).toBe(200);
  return (await res.json()).data.token as string;
}

/** A new staff member with one role, signed in to the console. */
async function staff(browser: Browser, request: APIRequestContext, role: string) {
  const token = await adminToken(request);
  const email = uniqueEmail(`mi-${role.replace(/_/g, "-")}`);
  const inv = await request.post(`${urls.api}/api/v1/admin/staff/invitations`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { email, name: `MI ${role}`, role },
  });
  expect(inv.status(), await inv.text()).toBeLessThan(300);
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
  return page;
}

test("the command center shows readiness, honest figures and a clear AI-unavailable state", async ({ page }) => {
  await signInToAdmin(page);
  await page.getByRole("navigation", { name: "Admin" }).getByRole("link", { name: "Command center" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Command center" })).toBeVisible();
  await expect(page.getByText(/Data readiness: Not ready/)).toBeVisible();
  await expect(page.getByText(/No real payments yet/)).toBeVisible();
  await expect(page.getByText(/of the money collected in this range went through the simulated test payment provider/)).toBeVisible();
  // Definitions and freshness are one click away.
  await page.getByText("Definition and freshness").first().click();
  await expect(page.getByText(/^As of /).first()).toBeVisible();

  await page.getByRole("link", { name: "Ask Mise AI" }).click();
  await expect(page.getByRole("heading", { name: "AI unavailable" })).toBeVisible();
  await expect(page.getByText(/No AI provider is configured/)).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Question" })).toHaveCount(0);

  const axe = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(axe.violations.filter((v) => v.impact === "serious" || v.impact === "critical").map((v) => v.id)).toEqual([]);
});

test("a weekly briefing is prepared once per unchanged period, with limitations", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${CC}?tab=briefing&cadence=weekly`);
  await page.getByRole("button", { name: "Prepare the weekly briefing" }).click();
  await expect(page.getByRole("heading", { name: "Business performance" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Data limitations" })).toBeVisible();
  await expect(page.getByText(/AI unavailable: .* The briefing is complete without it\./)).toBeVisible();
  await expect(page.getByText(/Contribution margin: withheld|Estimated operating result: withheld/).first()).toBeVisible();
  await shot(page, "briefing");
  // Again, with nothing changed: the same version.
  await page.getByRole("button", { name: "Prepare the weekly briefing" }).click();
  await expect(page.getByText(/Nothing changed since the last version|A new briefing version was stored/)).toBeVisible();
});

test("a scenario needs every input, then projects deterministically and can be saved", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${CC}?tab=outlook`);
  await expect(page.getByText("Not calculated: some inputs are missing")).toBeVisible();
  const values: Record<string, string> = {
    active_subscriptions: "100", new_subscriptions_per_week: "10", weekly_churn_bps: "5", renewal_success_bps: "100",
    weekly_value_minor: "200", ingredient_cost_per_kit_minor: "60", packaging_cost_per_kit_minor: "5", delivery_cost_per_kit_minor: "15",
    provider_fee_bps: "3", fixed_costs_monthly_minor: "5200", labour_monthly_minor: "0", marketing_monthly_minor: "0",
  };
  for (const [k, v] of Object.entries(values)) await page.locator(`input[name="v_${k}"]`).fill(v);
  await page.locator('select[name="horizon"]').selectOption("30");
  await page.getByRole("button", { name: "Run scenario" }).click();
  // 120,000 a week fixed ÷ 11,400 contribution per subscription = 10.53 → 11.
  const be = page.locator("div", { has: page.getByText("Break-even subscriptions", { exact: true }) }).last();
  await expect(be).toContainText("11");
  await expect(page.getByText("Operating result by week")).toBeVisible();
  await expect(page.getByText("Assumption").first()).toBeVisible();
  await shot(page, "outlook-run");
  await page.getByLabel("Name").fill(`E2E base plan ${Date.now()}`);
  await page.getByRole("button", { name: "Save scenario" }).click();
  await expect(page.getByRole("heading", { name: "Version 1" })).toBeVisible();
  await expect(page.getByText("Recomputed from its stored inputs: identical to the stored result.")).toBeVisible();
});

test("an expense goes from draft to approved and is audited", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/finance/expenses`);
  const description = `E2E kitchen rent ${Date.now()}`;
  await page.getByRole("combobox", { name: "Category", exact: true }).last().selectOption("kitchen_rent");
  await page.getByLabel("Description").fill(description);
  await page.getByLabel("Amount (TTD)").fill("4,500.00");
  await page.getByRole("button", { name: "Record expense" }).click();
  await expect(page.getByText(/Expense recorded as a draft/)).toBeVisible();
  await page.getByRole("link", { name: description }).click();
  await expect(page.getByText("TTD 4,500.00").first()).toBeVisible();
  await page.getByRole("combobox", { name: "Step", exact: true }).selectOption("submit");
  await page.getByRole("button", { name: "Apply" }).click();
  await expect(page.getByText("Expense updated.")).toBeVisible();
  await page.reload();
  await page.getByRole("combobox", { name: "Step", exact: true }).selectOption("approve");
  await page.getByRole("button", { name: "Apply" }).click();
  await expect(page.getByText("Expense updated.")).toBeVisible();
  await page.reload();
  await expect(page.getByText("Approved", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "History" })).toBeVisible();
  // Approved expenses cannot be edited, only voided.
  await expect(page.getByRole("combobox", { name: "Step", exact: true }).locator("option")).toHaveText(["Void"]);
});

test("a triggered risk is saved for review and dismissed with a reason", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${CC}?tab=risks`);
  await expect(page.getByText(/No probabilities are given/)).toBeVisible();
  const firstRisk = page.locator("li", { has: page.locator("summary", { hasText: "Evidence" }) }).first();
  await firstRisk.locator("summary", { hasText: "Evidence" }).click();
  await expect(firstRisk.getByText(/They never state a probability/)).toBeVisible();
  const save = page.getByRole("button", { name: "Save for review" }).first();
  await save.click();
  await expect(page.getByText("Saved for review under Recommendations.").first()).toBeVisible();
  await page.goto(`${CC}?tab=recommendations&status=open`);
  const item = page.locator("li", { has: page.getByText("Observed", { exact: true }) }).first();
  await expect(item).toBeVisible();
  const title = ((await item.locator("p").first().textContent()) ?? "").replace(/Observed.*$/, "").trim();
  await item.getByRole("combobox", { name: "Action", exact: true }).selectOption("dismiss");
  await item.getByRole("button", { name: "Apply" }).click();
  await expect(item.getByText("Some fields need attention.")).toBeVisible();
  await item.getByLabel(/Note/).fill("Reviewed in the weekly meeting: test data only.");
  await item.getByRole("button", { name: "Apply" }).click();
  // Dismissed: it leaves the open list and is kept, with its reason, under Dismissed.
  await page.goto(`${CC}?tab=recommendations&status=dismissed`);
  await expect(page.getByText(title, { exact: false }).first()).toBeVisible();
});

test("a meal proposal stays a draft and moves only through review", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${CC}?tab=meals`);
  await expect(page.getByText(/Draft concepts only/)).toBeVisible();
  await expect(page.getByText(/AI unavailable/).first()).toBeVisible();
  await page.getByLabel("Name").fill(`E2E callaloo pasta ${Date.now()}`);
  await page.getByLabel("Ingredients").fill("Callaloo | 200 g\nDragon fruit dust | 1 tsp");
  await page.getByRole("button", { name: "Add proposal" }).click();
  await expect(page.getByText("A draft, not a meal")).toBeVisible();
  await expect(page.getByText("Not in catalogue").first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Required approvals" })).toBeVisible();
  await page.getByRole("combobox", { name: "Step", exact: true }).selectOption("review");
  await page.getByRole("button", { name: "Apply" }).click();
  await expect(page.getByText("Proposal updated.")).toBeVisible();
  await page.reload();
  await page.getByRole("combobox", { name: "Step", exact: true }).selectOption("approve");
  await page.getByRole("button", { name: "Apply" }).click();
  await expect(page.getByText("Some fields need attention.")).toBeVisible();
  await shot(page, "proposal");
});

test("roles see only their intelligence: finance has finance, operations has no expenses", async ({ browser, request }) => {
  const fin = await staff(browser, request, "finance");
  await fin.goto(CC);
  const tabs = fin.getByRole("navigation", { name: "Sections" });
  await expect(tabs.getByRole("link", { name: "Finance" })).toBeVisible();
  await expect(tabs.getByRole("link", { name: "Outlook" })).toBeVisible();
  await expect(tabs.getByRole("link", { name: "Meals" })).toHaveCount(0);
  await expect(tabs.getByRole("link", { name: "Settings" })).toHaveCount(0);
  await fin.close();

  const ops = await staff(browser, request, "operations");
  await ops.goto(CC);
  await expect(ops.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: "Meals" })).toBeVisible();
  await expect(ops.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: "Finance" })).toHaveCount(0);
  const res = await ops.goto(`${urls.admin}/finance/expenses`);
  expect(res?.status()).toBe(404);
  await ops.close();

  const support = await staff(browser, request, "support");
  const r2 = await support.goto(CC);
  expect(r2?.status()).toBe(404);
  await support.close();
});

test("the command center works on a phone", async ({ browser }) => {
  const page = await (await browser.newContext({ viewport: { width: 390, height: 844 } })).newPage();
  await signInToAdmin(page);
  for (const tab of ["", "?tab=briefing", "?tab=risks", "?tab=outlook", "?tab=ask"]) {
    await page.goto(CC + tab);
    await expect(page.getByRole("heading", { level: 1, name: "Command center" })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  }
  await shot(page, "phone-ask");
});
