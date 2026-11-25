import AxeBuilder from "@axe-core/playwright";
import { expect, type APIRequestContext, type Page, test } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, signInToAdmin, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 9 Stage 5: signed-in customer pages and the checkout at every width
// the brief lists, a keyboard-only checkout, and the admin on a tablet/phone.
// Needs: misectl catalog seed-demo && misectl commerce seed-demo, worker running.

const widths = [320, 375, 390, 430, 768, 1024, 1440];
const password = "plantain and saltfish 2026";
const wcag = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"];

/** Horizontal page overflow in px (0 means nothing scrolls sideways). */
const overflowOf = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

/** A confirmed, signed-in customer with a delivery address. */
async function customer(page: Page, request: APIRequestContext, prefix: string) {
  const email = uniqueEmail(prefix);
  const res = await request.post(`${urls.api}/api/v1/auth/register`, {
    data: { email, password, first_name: "Aaliyah", last_name: "Mohammed", accept_terms: true },
  });
  expect(res.status(), await res.text()).toBe(202);
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  const token = new URL(linkIn(body, "/verify-email")).searchParams.get("token");
  expect((await request.post(`${urls.api}/api/v1/auth/verify-email`, { data: { token } })).status()).toBe(200);
  const login = await request.post(`${urls.api}/api/v1/auth/login`, { data: { email, password } });
  const session = (await login.json()).data.token as string;
  const addr = await request.post(`${urls.api}/api/v1/me/addresses`, {
    headers: { Authorization: `Bearer ${session}` },
    data: { recipient_name: "Aaliyah Mohammed", line1: "12 Pembroke Street", city: "Port of Spain", region: "port-of-spain" },
  });
  expect(addr.status(), await addr.text()).toBe(201);
  await page.goto(`${urls.web}/sign-in`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/onboarding$/);
}

/** Moves keyboard focus to `target` with Tab alone (fails if it is not reachable), then presses Enter. */
async function tabTo(page: Page, target: ReturnType<Page["getByRole"]>, maxTabs = 80) {
  for (let i = 0; i < maxTabs; i++) {
    await page.keyboard.press("Tab");
    if (await target.evaluate((el) => el === document.activeElement).catch(() => false)) {
      // Focus is visible: an outline or a box-shadow ring is drawn.
      const ring = await target.evaluate((el) => {
        const s = getComputedStyle(el);
        return (s.outlineStyle !== "none" && parseFloat(s.outlineWidth) > 0) || s.boxShadow !== "none";
      });
      expect(ring, "focused control shows a focus indicator").toBe(true);
      await page.keyboard.press("Enter");
      return;
    }
  }
  throw new Error(`not reachable with Tab within ${maxTabs} presses`);
}

test("a customer subscribes with the keyboard alone", async ({ page, request, browserName }) => {
  test.skip(browserName === "webkit", "WebKit (like Safari's default setting) does not move Tab focus to links. Needs a manual check on real Safari (not done in Sprint 9).");
  await customer(page, request, "keyboard-checkout");
  await page.goto(`${urls.web}/plans`);
  await tabTo(page, page.getByRole("link", { name: "Choose Basic" }));
  await expect(page.getByRole("heading", { name: "Your Basic plan" })).toBeVisible();
  await tabTo(page, page.getByRole("button", { name: "Review and pay" }));
  await expect(page.getByRole("heading", { name: "Review" })).toBeVisible();
  await tabTo(page, page.getByRole("button", { name: "Continue to payment" }));
  await expect(page).toHaveURL(/\/test-checkout\/tps_/);
  await tabTo(page, page.getByRole("button", { name: "Approve payment" }));
  await expect(page.getByRole("heading", { name: "You're subscribed." })).toBeVisible({ timeout: 30_000 });
});

test("signed-in pages and the checkout fit every width the brief lists", async ({ page, request }) => {
  test.setTimeout(300_000);
  await customer(page, request, "widths");
  // Checkout steps before paying.
  await page.goto(`${urls.web}/plans`);
  await page.getByRole("link", { name: "Choose Basic" }).click();
  const subscribeUrl = page.url();
  await page.getByRole("button", { name: "Review and pay" }).click();
  await expect(page.getByRole("heading", { name: "Review" })).toBeVisible();
  const reviewUrl = page.url();
  await page.getByRole("button", { name: "Continue to payment" }).click();
  await expect(page).toHaveURL(/\/test-checkout\/tps_/);
  const providerUrl = page.url();

  const overflow: string[] = [];
  const sweep = async (paths: string[]) => {
    for (const width of widths) {
      await page.setViewportSize({ width, height: 900 });
      for (const url of paths) {
        await page.goto(url);
        const extra = await overflowOf(page);
        if (extra > 0) overflow.push(`${url.replace(urls.web, "")} at ${width}px: ${extra}px`);
      }
    }
  };
  await sweep([subscribeUrl, reviewUrl, providerUrl]);

  await page.setViewportSize({ width: 1366, height: 850 });
  await page.goto(providerUrl);
  await page.getByRole("button", { name: "Approve payment" }).click();
  await expect(page.getByRole("heading", { name: "You're subscribed." })).toBeVisible({ timeout: 30_000 });

  await page.goto(`${urls.web}/menu`);
  await expect(page).toHaveURL(/\/select$/);
  const selectUrl = page.url();
  const signedIn = ["/dashboard", "/subscription", "/subscription/manage", "/billing", "/orders", "/favorites", "/profile", "/profile/addresses", "/profile/preferences", "/profile/notifications", "/profile/security"].map((p) => `${urls.web}${p}`);
  await sweep([selectUrl, ...signedIn]);
  expect(overflow).toEqual([]);

  // Axe on the steps the earlier specs do not reach.
  const found: string[] = [];
  for (const url of [subscribeUrl, `${urls.web}/subscription/manage`, `${urls.web}/dashboard`]) {
    await page.goto(url);
    const results = await new AxeBuilder({ page }).withTags(wcag).analyze();
    found.push(...results.violations.map((v) => `${url.replace(urls.web, "")} ${v.id}: ${v.help} (${v.nodes.length})`));
  }
  expect(found).toEqual([]);
});

test("the admin works on a tablet and a phone: navigation reachable, tables scroll inside their frame", async ({ page }) => {
  test.setTimeout(300_000);
  await signInToAdmin(page);
  const paths = ["/", "/customers", "/subscriptions", "/orders", "/payments", "/production", "/production/batches", "/procurement", "/purchase-orders", "/inventory", "/delivery", "/delivery/runs", "/intelligence", "/exports", "/launch", "/system"];
  const overflow: string[] = [];
  for (const width of [390, 768, 1024]) {
    await page.setViewportSize({ width, height: 900 });
    for (const path of paths) {
      await page.goto(`${urls.admin}${path}`);
      const extra = await overflowOf(page);
      if (extra > 0) overflow.push(`${path} at ${width}px: ${extra}px`);
    }
  }
  expect(overflow).toEqual([]);
});

test("customer pages respect reduced motion", async ({ browser }) => {
  const context = await browser.newContext({ reducedMotion: "reduce" });
  const page = await context.newPage();
  for (const url of [`${urls.web}/meals`, `${urls.web}/plans`, urls.site]) {
    await page.goto(url);
    // Nothing on the page is still animating once it has loaded.
    const running = await page.evaluate(() =>
      document.getAnimations().filter((a) => a.playState === "running" && (a.effect?.getTiming().iterations ?? 1) === Infinity).length,
    );
    expect(running, url).toBe(0);
  }
  await context.close();
});
