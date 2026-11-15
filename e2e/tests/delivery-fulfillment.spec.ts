import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Browser, type Page } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 7: from production readiness to proof of delivery, in the browser.
// A packer verifies kits on a phone, a manager releases, a dispatcher plans
// and records the handoff, a courier records progress and proof on a phone,
// and the customer sees real milestones. A second path fails, comes back,
// and is resolved and reconciled. Needs the worker and fresh demo data:
//   misectl production seed-demo && misectl delivery seed-demo -ready
// Each run uses two fulfillments. The E2E admin must be a super admin.

test.describe.configure({ mode: "serial" });

const password = "breadfruit and buljol 2026";
/** E2E_SHOTS=1 also saves report screenshots of screens only a test can reach. */
const shot = async (page: Page, name: string) => {
  if (process.env.E2E_SHOTS) await page.screenshot({ path: `../docs/screenshots/sprint-7/${name}.png`, fullPage: true });
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

/** A staff member with one role, invited and signed in, in their own browser. */
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

/** Submits a KForm and waits for its message. */
async function submit(page: Page, form: string, button: string, message: string) {
  const f = page.getByRole("form", { name: form, exact: true });
  await f.getByRole("button", { name: button, exact: true }).click();
  await expect(async () => {
    if ((await f.count()) === 0) return;
    await expect(f.getByRole("alert")).toHaveCount(0);
    await expect(f.locator("[aria-live=polite]")).toContainText(message, { timeout: 500 });
  }).toPass({ timeout: 15_000 });
}

async function noSeriousA11yIssues(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations.filter((v) => v.impact === "serious" || v.impact === "critical")).toEqual([]);
}

async function noSideScroll(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, "page scrolls sideways").toBeLessThanOrEqual(1);
}

type Fulfillment = { id: string; number: string; order_id: string; order_number: string; customer_id: string; status: string; version: number;
  packages: { items: { id: string; meal_name: string; quantity: number; batch_code: string }[] }[] };

/** Fulfillments made by real readiness events, waiting to be verified. */
async function readyFulfillments(request: APIRequestContext, token: string, n: number): Promise<Fulfillment[]> {
  const list = await call(request, token, "GET", "/fulfillment?status=ready_for_verification");
  expect(list.items.length, "Not enough fulfillments to verify: run `misectl production seed-demo && misectl delivery seed-demo -ready` first.").toBeGreaterThanOrEqual(n);
  return Promise.all(list.items.slice(0, n).map((f: { id: string }) => call(request, token, "GET", `/fulfillment/${f.id}`)));
}

async function demoSetup(request: APIRequestContext, token: string) {
  const partners = await call(request, token, "GET", "/delivery/partners");
  const partner = partners.items.find((p: { demo: boolean; status: string }) => p.demo && p.status === "approved");
  expect(partner, "No approved demo partner: run `misectl delivery seed-demo`.").toBeTruthy();
  const settings = await call(request, token, "GET", "/delivery/settings");
  expect(settings.proof_methods, "No proof policy: run `misectl delivery seed-demo`.").toContain("recipient_acknowledgment");
  const today = new Date().toISOString().slice(0, 10);
  const cal = await call(request, token, "GET", `/delivery/calendar?from=${today}&days=14`);
  const day = cal.items.find((d: { status: string; windows: unknown[] }) => d.status === "open" && d.windows.length);
  expect(day, "No demo service day: run `misectl delivery seed-demo`.").toBeTruthy();
  return { partner, day };
}

/** The customer of an order, signed in through the real password-reset email. */
async function customerPage(browser: Browser, request: APIRequestContext, token: string, customerId: string) {
  const c = await call(request, token, "GET", `/customers/${customerId}`);
  const page = await (await browser.newContext({ viewport: phone })).newPage();
  await page.goto(`${urls.web}/forgot-password`);
  await page.getByLabel("Email").fill(c.email);
  await page.getByRole("button", { name: "Send reset link" }).click();
  const body = await waitForEmail(request, c.email, "Reset your Mise password");
  const link = new URL(linkIn(body, "/reset-password"));
  await page.goto(`${urls.web}${link.pathname}${link.search}`);
  await page.getByLabel("New password").fill(password);
  await page.getByRole("button", { name: "Save new password" }).click();
  await expect(page.getByText("Your password is changed")).toBeVisible();
  await page.goto(`${urls.web}/sign-in`);
  await page.getByLabel("Email").fill(c.email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL((u) => !u.pathname.startsWith("/sign-in"));
  return page;
}

test("a verified order goes out, is handed over, tracked, proven delivered and shown to the customer", async ({ browser, request }) => {
  test.setTimeout(420_000);
  const token = await adminToken(request);
  const { partner, day } = await demoSetup(request, token);
  const [f] = await readyFulfillments(request, token, 1);

  // A packer on a phone checks every kit against the order.
  const packer = await staff(browser, request, "fulfillment_staff", "E2E Packer", phone);
  await packer.page.goto(`${urls.admin}/fulfillment/${f!.id}`);
  await expect(packer.page.getByRole("heading", { level: 1 })).toHaveText(`Order ${f!.order_number}`);
  await noSideScroll(packer.page);
  await shot(packer.page, "fulfillment-verify-phone");
  for (const it of f!.packages.flatMap((p) => p.items)) {
    const form = packer.page.getByRole("form", { name: `Check ${it.meal_name}`, exact: true });
    await form.getByLabel("Kits counted in the package").fill(String(it.quantity));
    await form.getByLabel("Batch code on the kit label").fill(it.batch_code);
    for (const box of ["The recipe card matches this meal", "Packaging is intact and sealed", "The delivery label is on the package"]) {
      await form.getByLabel(box).check();
    }
    await submit(packer.page, `Check ${it.meal_name}`, "Record check", "Check recorded.");
  }
  await packer.page.reload();
  await expect(packer.page.getByText("Verified", { exact: true }).first()).toBeVisible();
  await expect(packer.page.getByRole("form", { name: "Release to dispatch" })).toHaveCount(0); // packers cannot release
  await expect(packer.page.getByText("NOT CERTIFIED FOR LIVE USE").first()).toBeVisible();
  await noSeriousA11yIssues(packer.page);

  // The fulfillment manager releases it to dispatch.
  const manager = await staff(browser, request, "fulfillment_manager", "E2E Fulfillment Manager");
  await manager.page.goto(`${urls.admin}/fulfillment/${f!.id}`);
  await submit(manager.page, "Release to dispatch", "Release to dispatch", "Released to dispatch.");
  const released = await call(request, token, "GET", `/fulfillment/${f!.id}`);
  const dl = released.delivery_number as string;
  expect(dl).toBeTruthy();

  // A courier with a login, linked to the demo partner.
  const rider = `E2E Rider ${Date.now().toString(36).slice(-5)}`;
  const courier = await staff(browser, request, "courier", rider, phone);
  await call(request, token, "POST", "/delivery/couriers", { partner_id: partner.id, name: rider, user_email: courier.email });

  // The dispatcher schedules it, makes a run, assigns it, generates the manifest and releases.
  const dispatcher = await staff(browser, request, "dispatcher", "E2E Dispatcher");
  const dp = dispatcher.page;
  await dp.goto(`${urls.admin}/delivery/planning?date=${day.date}`);
  await submit(dp, `Schedule ${dl}`, "Schedule", "Scheduled.");
  const create = dp.getByRole("form", { name: "Create a run" });
  await create.getByLabel("Partner (approved only)").selectOption({ value: partner.id });
  await create.getByLabel("Courier (optional)").selectOption({ label: `${rider} · ${partner.name}` });
  await create.getByRole("button", { name: "Create run" }).click();
  await dp.waitForURL(/\/delivery\/runs\//);
  const runUrl = dp.url();
  await dp.goto(`${urls.admin}/delivery/planning?date=${day.date}`);
  const assign = dp.getByRole("form", { name: `Assign ${dl}` });
  await assign.getByLabel("Run").selectOption({ value: runUrl.split("/").pop()! });
  await submit(dp, `Assign ${dl}`, "Assign", "Added to the run.");
  await dp.goto(runUrl);
  await submit(dp, "Finalize run", "Generate manifest", "Manifest generated.");
  await expect(dp.getByRole("heading", { name: /Manifest, revision 1/ })).toBeVisible();
  await expect(dp.getByText("@example.com")).toHaveCount(0); // no customer email on the manifest
  const csv = await dp.request.get(`${runUrl}/manifest`);
  expect(csv.headers()["cache-control"]).toContain("no-store");
  expect(await csv.text()).toContain(dl);
  await submit(dp, "Release run", "Release for pickup", "Released for pickup.");
  await dp.reload();
  await submit(dp, "Record handoff", "Record handoff", "Handoff recorded.");
  await dp.reload();
  await expect(dp.getByText(/accepted/).first()).toBeVisible();

  // The courier, on a phone, sees only their run, records progress and proof, and completes.
  const cp = courier.page;
  await cp.goto(`${urls.admin}/delivery/runs`);
  await expect(cp.getByRole("heading", { level: 1 })).toHaveText("My runs");
  await cp.goto(runUrl);
  await cp.getByRole("link", { name: dl }).click();
  await expect(cp.getByRole("heading", { level: 1 })).toHaveText(`Delivery ${dl}`);
  await noSideScroll(cp);
  await submit(cp, "Record a status update", "Save update", "Update recorded.");
  await shot(cp, "courier-delivery-out-phone");
  await expect(cp.getByText("Needs an internet connection").first()).toBeVisible();
  const proofForm = cp.getByRole("form", { name: "Record proof" });
  await proofForm.locator("select[name=method]").selectOption({ label: "Recipient named who received it" });
  await proofForm.getByLabel("Received by (name)").fill("Demo Customer");
  await submit(cp, "Record proof", "Record proof", "Proof recorded.");
  await submit(cp, "Mark delivered", "Mark delivered", "Delivery completed.");
  await cp.reload();
  await expect(cp.getByText("Delivered").first()).toBeVisible();
  await noSeriousA11yIssues(cp);

  // Commerce and fulfillment agree; the customer sees real milestones in plain words.
  const after = await call(request, token, "GET", `/fulfillment/${f!.id}`);
  expect(after.status).toBe("fulfilled");
  const customer = await customerPage(browser, request, token, f!.customer_id);
  await customer.goto(`${urls.web}/orders/${f!.order_id}`);
  const section = customer.getByRole("region", { name: "Delivered" });
  await expect(section).toBeVisible();
  for (const m of ["Order confirmed", "Kitchen preparing", "Packed and checked", "Ready for delivery", "Out for delivery", "Delivered"]) {
    await expect(section.getByText(`${m} (done)`, { exact: false })).toHaveCount(1);
  }
  await expect(customer.getByText("in_transit")).toHaveCount(0);
  await expect(customer.getByText(partner.name)).toHaveCount(0);
  await noSideScroll(customer);
  await shot(customer, "customer-order-delivered-phone");
  await noSeriousA11yIssues(customer);

  // Support sees the history on the customer's record.
  const support = await staff(browser, request, "support", "E2E Support");
  await support.page.goto(`${urls.admin}/customers/${f!.customer_id}?tab=deliveries`);
  await expect(support.page.getByRole("link", { name: dl })).toBeVisible();
});

test("a failed attempt raises an exception, comes back quarantined, and the run reconciles once resolved", async ({ browser, request }) => {
  test.setTimeout(300_000);
  const token = await adminToken(request);
  const { partner, day } = await demoSetup(request, token);
  const [f] = await readyFulfillments(request, token, 1);
  // Up to the handoff through the API (the UI path is the first test).
  for (const it of f!.packages.flatMap((p) => p.items)) {
    await call(request, token, "POST", `/fulfillment/${f!.id}/verify`, { item_id: it.id, counted_quantity: it.quantity, scanned_batch_code: it.batch_code,
      recipe_confirmed: true, packaging_ok: true, label_ok: true, idempotency_key: `e2e-${it.id}` });
  }
  const rel = await call(request, token, "POST", `/fulfillment/${f!.id}/release`, {});
  const d = rel.delivery_id as string;
  const windowId = day.windows.find((w: { status: string }) => w.status === "approved").id;
  await call(request, token, "POST", `/delivery/deliveries/${d}/schedule`, { service_date: day.date, window_id: windowId });
  const run = await call(request, token, "POST", "/delivery/dispatch-runs", { service_date: day.date, partner_id: partner.id });
  await call(request, token, "POST", `/delivery/dispatch-runs/${run.id}/stops`, { delivery_id: d });
  const fin = await call(request, token, "POST", `/delivery/dispatch-runs/${run.id}/finalize`);
  await call(request, token, "POST", `/delivery/dispatch-runs/${run.id}/release`);
  const pkgs = fin.manifest.content.stops.flatMap((s: { packages: { id: string }[] }) => s.packages.map((p) => ({ package_id: p.id, outcome: "accepted" })));
  await call(request, token, "POST", `/delivery/dispatch-runs/${run.id}/handoffs`, { to_party: "Demo partner", packages: pkgs, idempotency_key: `e2e-h-${run.id}` });

  // A coordinator records the failed attempt; an exception appears.
  const coord = await staff(browser, request, "delivery_coordinator", "E2E Coordinator");
  const p = coord.page;
  await p.goto(`${urls.admin}/delivery/deliveries/${d}`);
  const fail = p.getByRole("form", { name: "Record a failed attempt" });
  await fail.getByLabel("Reason").selectOption({ label: "Recipient unavailable" });
  await fail.getByLabel("What happened").fill("No answer at the gate; phone off.");
  await submit(p, "Record a failed attempt", "Record failed attempt", "Failure recorded; an exception was raised.");
  await p.goto(`${urls.admin}/delivery/exceptions?delivery_id=${d}`);
  await expect(p.getByText(/failed: recipient unavailable/i)).toBeVisible();

  // Reconciliation refuses while the failure is unexplained.
  const recon = await request.post(`${urls.api}/api/v1/admin/delivery/dispatch-runs/${run.id}/reconcile`, { headers: { Authorization: `Bearer ${token}` }, data: {} });
  expect(recon.status()).toBe(422);

  // The kits come back: quarantined for review, then discarded per procedure.
  await p.goto(`${urls.admin}/delivery/deliveries/${d}`);
  const ret = p.getByRole("form", { name: "Record return" });
  await ret.getByLabel("Reason").fill("Brought back by the partner");
  await submit(p, "Record return", "Record return", "Return recorded; the packages are quarantined.");
  await p.goto(`${urls.admin}/delivery/returns`);
  const number = rel.delivery_number as string;
  const review = p.getByRole("form", { name: `Review return ${number}`, exact: true });
  await review.getByLabel("Note (which procedure, who)").fill("Discarded under the food-safety rule; out of the cold chain.");
  await submit(p, `Review return ${number}`, "Record review", "Review recorded.");
  await p.goto(`${urls.admin}/delivery/returns?status=all`);
  await expect(p.getByText(/Discarded under the food-safety rule/).first()).toBeVisible();

  // The exception is resolved with its next action, and the run reconciles.
  await p.goto(`${urls.admin}/delivery/exceptions?delivery_id=${d}`);
  await p.getByRole("link", { name: /^EXC-/ }).first().click();
  const work = p.getByRole("form", { name: "Update exception" });
  await work.getByLabel("Status").selectOption("resolved");
  await work.getByLabel("Next action").selectOption("refund_review");
  await work.getByLabel("Resolution (when resolving)").fill("Kits discarded; refund review passed to finance.");
  await submit(p, "Update exception", "Save", "Exception updated.");
  const manager = await staff(browser, request, "dispatch_manager", "E2E Dispatch Manager");
  await manager.page.goto(`${urls.admin}/delivery/runs/${run.id}`);
  await submit(manager.page, "Reconcile run", "Close and reconcile", "Run reconciled.");
  await manager.page.reload();
  await expect(manager.page.getByText(/^Reconciled/).first()).toBeVisible();
});

test("each delivery role sees only its own part", async ({ browser, request }) => {
  const packer = await staff(browser, request, "fulfillment_staff", "E2E Packer Two");
  await packer.page.goto(`${urls.admin}/delivery/planning`);
  await expect(packer.page.getByRole("heading", { name: /not found|Page not found/i })).toBeVisible();
  const nav = packer.page.getByRole("navigation", { name: "Admin" });
  await expect(nav.getByRole("link", { name: "Fulfillment queue" })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Dispatch planning" })).toHaveCount(0);

  const courier = await staff(browser, request, "courier", "E2E Rider Two", phone);
  const cnav = courier.page.getByRole("navigation", { name: "Admin" });
  await courier.page.goto(`${urls.admin}/delivery/runs`);
  await expect(courier.page.getByRole("heading", { level: 1 })).toHaveText("My runs");
  await expect(courier.page.getByText("not linked to a courier record yet")).toBeVisible(); // no courier record: nothing to see
  await expect(cnav.getByRole("link", { name: "Delivery partners" })).toHaveCount(0);
});

test("the delivery screens work on a phone", async ({ browser, request }) => {
  const { page } = await staff(browser, request, "dispatch_manager", "E2E Phone Dispatch", phone);
  for (const path of ["/delivery", "/fulfillment", "/delivery/planning", "/delivery/runs", "/delivery/exceptions", "/delivery/calendar", "/delivery/zones", "/delivery/partners", "/reports/delivery"]) {
    await page.goto(`${urls.admin}${path}`);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await noSideScroll(page);
  }
});
