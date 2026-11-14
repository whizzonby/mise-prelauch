import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Browser, type Page } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, signInToAdmin, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 6: a farmer recorded, qualified, mapped and priced; purchasing from a
// released plan to an order approved by someone else; a partial delivery
// received at the door (phone width) with a rejected bag; stock reserved and
// issued to a batch; the lot traced to the batch and its orders; role limits
// and phone width. Needs the worker and a fresh released demo plan:
//   misectl production seed-demo
// Each run uses up one procurement run. The E2E admin must be a super admin.

const password = "plantain and saltfish 2026";
const suffix = () => Date.now().toString(36).slice(-5).toUpperCase();

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

/** A staff member with one role, invited and signed in, in their own browser. */
async function staff(browser: Browser, request: APIRequestContext, role: string, name: string, viewport = { width: 1366, height: 850 }): Promise<Page> {
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
  return page;
}

/** Creates a setup record; another test running at the same moment may have just made it (409). */
async function ensure(request: APIRequestContext, token: string, path: string, data: object) {
  const res = await request.post(`${urls.api}/api/v1/admin${path}`, { headers: { Authorization: `Bearer ${token}` }, data });
  expect([200, 201, 409], `POST ${path}: ${await res.text()}`).toContain(res.status());
}

/** The procedures the business would supply: one qualification criterion and one receiving check (created once). */
async function procedures(request: APIRequestContext) {
  const token = await adminToken(request);
  const criteria = await call(request, token, "GET", "/supplier-criteria");
  if (!criteria.items.some((c: { name: string }) => c.name === "Supply agreement signed (E2E procedure)")) {
    await call(request, token, "POST", "/supplier-criteria", { name: "Supply agreement signed (E2E procedure)", procedure_ref: "E2E-SOP" });
  }
  const checks = await call(request, token, "GET", "/receiving/checklist");
  if (!checks.items.some((c: { name: string }) => c.name === "Packaging intact (E2E procedure)")) {
    await call(request, token, "POST", "/receiving/checklist", { name: "Packaging intact (E2E procedure)", criteria: "No tears or leaks.", procedure_ref: "E2E-SOP" });
  }
  const locations = await call(request, token, "GET", "/inventory/locations");
  for (const [code, name, type] of [["E2E-CHILL", "E2E chiller", "chilled"], ["E2E-QUAR", "E2E quarantine", "quarantine"]]) {
    if (!locations.items.some((l: { code: string }) => l.code === code)) await ensure(request, token, "/inventory/locations", { code, name, type });
  }
  return token;
}

/** A supplier approved through the API (the UI path is the first test). */
async function approvedFarm(request: APIRequestContext, token: string, coordinatorToken: string) {
  const s = await call(request, coordinatorToken, "POST", "/suppliers", { name: `E2E Farm ${suffix()}`, type: "farmer", phone: "+1 868 555 0199" });
  await call(request, coordinatorToken, "POST", `/suppliers/${s.id}/submit`);
  const full = await call(request, token, "GET", `/suppliers/${s.id}`);
  await call(request, token, "POST", `/suppliers/${s.id}/review`, {
    decision: "approved",
    results: full.criteria.map((c: { id: string }) => ({ criterion_id: c.id, met: true })),
  });
  return s;
}

async function noSeriousA11yIssues(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations.filter((v) => v.impact === "serious" || v.impact === "critical")).toEqual([]);
}

/** Submits a KForm and waits for its message (or for the form to give way to the next step). */
async function submit(page: Page, form: string, button: string, message: string) {
  const f = page.getByRole("form", { name: form, exact: true });
  await f.getByRole("button", { name: button, exact: true }).click();
  await expect(async () => {
    if ((await f.count()) === 0) return;
    await expect(f.getByRole("alert")).toHaveCount(0);
    await expect(f.locator("[aria-live=polite]")).toContainText(message, { timeout: 500 });
  }).toPass({ timeout: 15_000 });
}

test("a farmer is recorded, qualified against the procedure, and their product mapped and priced", async ({ page, request, browser }) => {
  // Several staff sessions in their own browsers: give it room on a loaded machine.
  test.slow();
  await procedures(request);
  const coordinator = await staff(browser, request, "supplier_coordinator", "Shirin Coordinator");
  const name = `Ramdass Garden ${suffix()}`;

  // Only the name and type are needed to start.
  await coordinator.goto(`${urls.admin}/suppliers?tab=farmers`);
  await expect(coordinator.getByRole("heading", { level: 1, name: "Farmers" })).toBeVisible();
  const add = coordinator.getByRole("form", { name: "Add a supplier" });
  await add.getByLabel("Name").fill(name);
  await add.getByLabel("Phone", { exact: true }).fill("+1 868 555 0142");
  await add.getByLabel("Preferred contact").selectOption("whatsapp");
  await add.getByRole("button", { name: "Add supplier" }).click();
  await expect(coordinator.getByRole("heading", { level: 1, name })).toBeVisible();
  const supplierUrl = coordinator.url().replace(/\?.*$/, "");

  // The farm, recorded on the farmer's behalf.
  await coordinator.goto(`${supplierUrl}?tab=farm`);
  const farm = coordinator.getByRole("form", { name: "Farm profile" });
  await farm.getByLabel("Farm name").fill("Ramdass Garden, Penal");
  await farm.getByLabel("Crops produced").fill("Pimento, chive, bodi");
  await farm.getByLabel("Harvest windows").fill("Chive weekly; pimento every second week");
  await farm.getByLabel("Where this information came from").fill("WhatsApp call, 13 November");
  await submit(coordinator, "Farm profile", "Save farm profile", "Farm profile saved.");

  // Sent for review; the coordinator cannot approve.
  await coordinator.goto(supplierUrl);
  await submit(coordinator, "Submit for review", "Submit for review", "Sent for review.");
  await coordinator.reload();
  await expect(coordinator.getByRole("form", { name: "Qualification review" })).toHaveCount(0);

  // Someone else reviews it against every criterion.
  await signInToAdmin(page);
  await page.goto(supplierUrl);
  const review = page.getByRole("form", { name: "Qualification review" });
  for (const met of await review.getByRole("radio", { name: "Met", exact: true }).all()) await met.check();
  await submit(page, "Qualification review", "Record review", "Supplier approved.");
  await page.reload();
  await expect(page.getByText("Approved").first()).toBeVisible();
  await noSeriousA11yIssues(page);

  // A product mapped to the catalogue ingredient, confirmed by review, then priced.
  await coordinator.goto(`${supplierUrl}?tab=products`);
  const product = coordinator.getByRole("form", { name: "Add a product" });
  await product.getByLabel("Product name").fill("Chicken thighs, 2 kg bag");
  await product.getByLabel("Catalogue ingredient").selectOption({ label: "Demo chicken thigh (g)" });
  await product.getByLabel("Pack size").fill("2");
  await product.getByLabel("Unit").selectOption("kg");
  await product.getByRole("button", { name: "Add product" }).click();
  await expect(coordinator.getByRole("heading", { level: 1, name: "Chicken thighs, 2 kg bag" })).toBeVisible();
  await expect(coordinator.getByText(/One pack is 2,000 g \(2 kg\) of Demo chicken thigh/)).toBeVisible();
  await submit(coordinator, "Review mapping", "Confirm mapping", "Mapping confirmed");
  const price = coordinator.getByRole("form", { name: "Record a price" });
  await price.getByLabel("Price per pack ($)").fill("45.00");
  await submit(coordinator, "Record a price", "Record price", "Price recorded.");
  await coordinator.reload();
  await expect(coordinator.getByText("In force")).toBeVisible();
  await expect(coordinator.getByRole("cell", { name: /TTD\s*45\.00/ }).first()).toBeVisible();
});

test("from a released plan to an approved order, a partial delivery, stock issued to a batch and traced", async ({ page, request, browser }) => {
  // Several staff sessions in their own browsers: give it room on a loaded machine.
  test.slow();
  const token = await procedures(request);
  // A procurement run still open, for a plan with a batch not yet started (misectl production seed-demo).
  const runs = await call(request, token, "GET", "/procurement/runs?status=open");
  let run: { id: string; plan_id: string } | undefined;
  let batch: { id: string; code: string } | undefined;
  for (const r of runs.items as { id: string; plan_id: string }[]) {
    const batches = await call(request, token, "GET", `/production/batches?plan_id=${r.plan_id}&status=released`);
    if (batches.items.length) {
      run = r;
      batch = batches.items[0];
      break;
    }
  }
  if (!run || !batch) {
    const plans = await call(request, token, "GET", "/procurement/pending-plans");
    for (const p of plans.items as { id: string }[]) {
      const batches = await call(request, token, "GET", `/production/batches?plan_id=${p.id}&status=released`);
      if (batches.items.length) {
        run = await call(request, token, "POST", "/procurement/runs", { plan_id: p.id });
        batch = batches.items[0];
        break;
      }
    }
  }
  expect(run, "No open procurement run with a released batch: run `misectl production seed-demo` first (see the README).").toBeTruthy();

  // Supplier and product through the API (the UI path is the first test). A
  // coordinator submits, so the super admin may approve.
  const farm = await approvedFarm(request, token, await coordinatorToken(request));
  const product = await call(request, token, "POST", `/suppliers/${farm.id}/products`, {
    ingredient_id: await ingredientId(request, token, "Demo chicken thigh"), name: "Chicken thighs, 2 kg bag", pack_quantity: "2", pack_unit: "kg", lead_time_days: 1 });
  await call(request, token, "POST", `/supplier-products/${product.id}/mapping`, { decision: "confirmed" });
  await call(request, token, "POST", `/supplier-products/${product.id}/prices`, { amount_minor: 4500, currency: "TTD", valid_from: "2026-11-01" });

  // The buyer decides on the run page: 3 bags from the farm; the other ingredients later.
  const buyer = await staff(browser, request, "procurement_officer", "Marcus Buyer");
  await buyer.goto(`${urls.admin}/procurement/${run!.id}`);
  const chicken = buyer.getByRole("region", { name: "Demo chicken thigh" });
  await expect(chicken.getByText("Why this quantity")).toBeVisible();
  const decision = buyer.getByRole("form", { name: "Decision for Demo chicken thigh" });
  await decision.getByRole("radio", { name: `Buy Chicken thighs, 2 kg bag from ${farm.name}` }).check();
  await decision.getByLabel(`Packs from ${farm.name}`).fill("3");
  await submit(buyer, "Decision for Demo chicken thigh", "Record decision", "Decision recorded.");
  for (const other of ["Demo long-grain rice", "Demo onion"]) {
    const form = buyer.getByRole("form", { name: `Decision for ${other}` });
    if (await form.count()) {
      await form.getByRole("radio", { name: "Decide later" }).check();
      await form.getByLabel("Why (required not to buy or to defer)").fill("Covered by a separate order this week.");
      await submit(buyer, `Decision for ${other}`, "Record decision", "Decision recorded.");
    }
  }
  // Officers do not approve; the super admin does. Then the buyer drafts the order.
  await expect(buyer.getByRole("form", { name: "Approve decisions" })).toHaveCount(0);
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/procurement/${run!.id}`);
  await submit(page, "Approve decisions", "Approve decisions", "Purchasing decisions approved.");
  await buyer.reload();
  await submit(buyer, "Draft purchase orders", "Draft purchase orders", "Draft orders made");
  await buyer.goto(`${urls.admin}/purchase-orders?run_id=${run!.id}`);
  await buyer.getByRole("link", { name: /^PO-/ }).first().click();
  await expect(buyer.getByRole("heading", { level: 1, name: /^Order PO-/ })).toBeVisible();
  const orderUrl = buyer.url();
  const tomorrow = new Date(Date.now() + 86400000).toISOString().slice(0, 10);
  const edit = buyer.getByRole("form", { name: "Edit order" });
  await edit.getByLabel("Expected delivery").fill(tomorrow);
  await edit.getByLabel("Deliver to").selectOption({ label: "E2E chiller" });
  await submit(buyer, "Edit order", "Save order", "Order saved.");
  await buyer.reload();
  await submit(buyer, "Submit order", "Submit for approval", "Sent for approval.");
  // The buyer cannot approve their own order; the super admin approves and issues it.
  await buyer.reload();
  await expect(buyer.getByRole("form", { name: "Approve order" })).toHaveCount(0);
  await page.goto(orderUrl);
  await submit(page, "Approve order", "Approve order", "Approval recorded.");
  await page.reload();
  await page.getByRole("form", { name: "Issue order" }).getByLabel("Sent by").selectOption("whatsapp");
  await submit(page, "Issue order", "Record as sent", "Recorded as sent to the supplier.");
  await page.goto(`${orderUrl}/print`);
  await expect(page.getByRole("heading", { level: 1, name: /^Purchase order PO-/ })).toBeVisible();

  // At the door, on a phone: two of three bags; one torn.
  const receiver = await staff(browser, request, "receiving_staff", "Andre Receiver", { width: 390, height: 844 });
  await receiver.goto(`${urls.admin}/receiving`);
  await receiver.getByRole("link", { name: "Receive" }).first().click();
  const line = receiver.getByRole("region", { name: /Chicken thighs, 2 kg bag/ });
  await line.getByLabel("Packs delivered", { exact: true }).fill("2");
  await line.getByLabel("Accepted", { exact: true }).fill("1");
  await line.getByLabel("Rejected", { exact: true }).fill("1");
  await line.getByLabel("Expiry or use-by date").fill(new Date(Date.now() + 6 * 86400000).toISOString().slice(0, 10));
  await line.getByLabel("Supplier's lot reference").fill("RG-1113");
  await line.getByLabel("Quarantine or rejected stock to").selectOption({ label: "E2E quarantine" });
  await line.getByLabel("Store accepted stock in").selectOption({ label: "E2E chiller" });
  for (const pass of await line.getByRole("radio", { name: "Pass", exact: true }).all()) await pass.check();
  await line.getByLabel("Discrepancy").selectOption("damaged");
  await line.getByLabel("What was wrong").fill("One bag torn in transit.");
  const overflow = await receiver.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, "receiving form at 390 px").toBeLessThanOrEqual(1);
  await receiver.getByRole("button", { name: "Record the delivery" }).click();
  await expect(receiver.getByRole("heading", { level: 1, name: /^Receipt GR-/ })).toBeVisible();
  await expect(receiver.getByText(/Rejected/).first()).toBeVisible();
  const lotLink = receiver.getByRole("link", { name: /^LOT-/ }).first();
  const lotUrl = `${urls.admin}${await lotLink.getAttribute("href")}`;

  // The order is partly received; reserve the accepted bag for the plan from the run page.
  await page.goto(orderUrl);
  await expect(page.getByText("Partially received").first()).toBeVisible();
  await page.goto(`${urls.admin}/procurement/${run!.id}`);
  const reserveForm = page.getByRole("form", { name: "Reserve Demo chicken thigh" });
  await reserveForm.getByRole("button").click();
  // Once everything usable is reserved, the form gives way.
  await expect(async () => {
    if ((await reserveForm.count()) === 0) return;
    await expect(reserveForm.locator("[aria-live=polite]")).toContainText("Reserved", { timeout: 500 });
  }).toPass({ timeout: 15_000 });
  await page.reload();
  // The second figure on the line is what is reserved for this plan.
  await expect(page.getByRole("region", { name: "Demo chicken thigh" }).getByRole("definition").nth(1)).not.toHaveText("0 g");

  // The kitchen issues chicken to the batch: the plan's reservation first.
  const cook = await staff(browser, request, "kitchen_staff", "Nadia Cook");
  await cook.goto(`${urls.admin}/inventory/batches/${batch!.id}`);
  const issue = cook.getByRole("form", { name: "Issue stock" });
  await issue.getByLabel("Ingredient").selectOption({ label: "Demo chicken thigh (g)" });
  await issue.getByLabel("Quantity (base unit)").fill("1500");
  await submit(cook, "Issue stock", "Issue", "Issued from LOT-");
  await cook.reload();
  await expect(cook.getByRole("cell", { name: /^LOT-/ }).first()).toBeVisible();

  // The lot traced from supplier to batch and the orders behind it.
  await page.goto(lotUrl);
  await page.getByRole("link", { name: "Trace this lot" }).click();
  await expect(page.getByText("Traceability is batch-level")).toBeVisible();
  await expect(page.getByRole("link", { name: farm.name })).toBeVisible();
  await expect(page.getByRole("link", { name: batch!.code })).toBeVisible();
  await noSeriousA11yIssues(page);
});

// coordinatorToken returns a token for a fresh supplier coordinator (approval needs a different submitter).
async function coordinatorToken(request: APIRequestContext): Promise<string> {
  const token = await adminToken(request);
  const email = uniqueEmail("coordinator-api");
  await call(request, token, "POST", "/staff/invitations", { email, name: "API Coordinator", role: "supplier_coordinator" });
  const body = await waitForEmail(request, email, "You're invited to the Mise console");
  const invitation = new URL(linkIn(body, "/accept-invite")).searchParams.get("token");
  await request.post(`${urls.api}/api/v1/admin/auth/invitations/accept`, { data: { token: invitation, password } });
  const login = await request.post(`${urls.api}/api/v1/admin/auth/login`, { data: { email, password } });
  return (await login.json()).data.token as string;
}

async function ingredientId(request: APIRequestContext, token: string, name: string): Promise<string> {
  const res = await call(request, token, "GET", `/ingredients?q=${encodeURIComponent(name)}`);
  const hit = (res.items as { id: string; name: string }[]).find((i) => i.name === name);
  expect(hit, `${name} exists (misectl production seed-demo)`).toBeTruthy();
  return hit!.id;
}

test("each supply-chain role sees and does only its own part", async ({ request, browser }) => {
  await procedures(request);
  const receiver = await staff(browser, request, "receiving_staff", "Gail Receiver");
  const nav = receiver.getByRole("navigation", { name: "Admin" });
  await expect(nav.getByRole("link", { name: "Goods receiving" })).toBeVisible();
  for (const hidden of ["Supplier directory", "Procurement planning", "Quotations", "Supply settings", "Supplier performance"]) {
    await expect(nav.getByRole("link", { name: hidden, exact: true })).toHaveCount(0);
  }
  // Pages are closed, not just hidden; orders are shown without prices.
  await receiver.goto(`${urls.admin}/suppliers`);
  await expect(receiver.getByRole("heading", { level: 1, name: "Supplier directory" })).toHaveCount(0);
  await receiver.goto(`${urls.admin}/purchase-orders`);
  await expect(receiver.getByRole("columnheader", { name: "Total" })).toHaveCount(0);

  const cook = await staff(browser, request, "kitchen_staff", "Leon Cook");
  const cookNav = cook.getByRole("navigation", { name: "Admin" });
  await expect(cookNav.getByRole("link", { name: "Inventory", exact: true })).toBeVisible();
  await expect(cookNav.getByRole("link", { name: "Purchase orders" })).toHaveCount(0);
  await cook.goto(`${urls.admin}/supply/settings`);
  await expect(cook.getByRole("heading", { level: 1, name: "Supply settings" })).toHaveCount(0);

  // The API refuses too: a coordinator cannot approve a supplier.
  const coordinator = await coordinatorToken(request);
  const s = await call(request, coordinator, "POST", "/suppliers", { name: `Self Approver ${suffix()}`, type: "distributor", phone: "1" });
  const refused = await request.post(`${urls.api}/api/v1/admin/suppliers/${s.id}/review`, { headers: { Authorization: `Bearer ${coordinator}` }, data: { decision: "approved" } });
  expect(refused.status()).toBe(403);
});

test("the supply-chain screens work on a phone", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await signInToAdmin(page);
  for (const path of ["/supply", "/procurement", "/purchase-orders", "/receiving", "/inventory", "/inventory/lots", "/inventory/counts", "/suppliers", "/supplier-products",
    "/traceability", "/supply/settings", "/reports/suppliers"]) {
    await page.goto(`${urls.admin}${path}`);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, path).toBeLessThanOrEqual(1);
  }
  // Records hold the wide tables (price history, lines, movements): open the first of each.
  for (const [list, prefix] of [["/suppliers", "/suppliers/"], ["/supplier-products", "/supplier-products/"], ["/purchase-orders", "/purchase-orders/"],
    ["/procurement", "/procurement/"], ["/receiving", "/receiving/"], ["/inventory/lots", "/inventory/lots/"]]) {
    await page.goto(`${urls.admin}${list}`);
    const link = page.locator(`main a[href^="${prefix}"]:not([href^="/receiving/new"])`).first();
    if (!(await link.count())) continue; // nothing recorded yet on a fresh database
    const href = (await link.getAttribute("href"))!;
    await page.goto(`${urls.admin}${href}`);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, href).toBeLessThanOrEqual(1);
  }
});
