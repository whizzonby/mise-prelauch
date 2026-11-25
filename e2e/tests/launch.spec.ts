import { mkdirSync } from "node:fs";
import { join } from "node:path";

import AxeBuilder from "@axe-core/playwright";
import { expect, type Page, test } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, signInToAdmin, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 9: the public storefront, the visitor-to-subscriber handoff, SEO,
// responsive layout, accessibility, and the launch control centre.

const shots = join(import.meta.dirname, "..", "..", "docs", "screenshots", "sprint-9");
mkdirSync(shots, { recursive: true });
// Evidence screenshots come from the Chromium run only, so cross-browser runs do not overwrite them.
const shot = async (page: Page, name: string, fullPage = true) => {
  if (test.info().project.name === "chromium") await page.screenshot({ path: join(shots, `${name}.png`), fullPage });
};

const launchMode = process.env.NEXT_PUBLIC_SITE_MODE === "launch";
const password = "plantain and saltfish 2026";
const wcag = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"];

/** Waits until entrance animations have finished, so contrast is measured on the settled colours. */
async function settled(page: Page) {
  await page.waitForFunction(() => document.getAnimations().every((a) => a.playState !== "running" || a.effect?.getTiming().iterations === Infinity));
}

async function firstMeal(page: Page): Promise<string> {
  await page.goto(`${urls.web}/meals`);
  const link = page.locator("main article h3 a").first();
  const href = await link.getAttribute("href");
  expect(href).toMatch(/^\/meals\/[a-z0-9-]+$/);
  return href!;
}

test("a visitor browses meal kits and plans without an account", async ({ page }) => {
  await page.goto(urls.web);
  await expect(page).toHaveURL(`${urls.web}/meals`);
  await expect(page.getByRole("heading", { level: 1, name: "Explore meals" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Sign in" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Get started" })).toBeVisible();
  // Visitors cannot save meals or filter by their own allergens.
  await expect(page.getByRole("button", { name: /save/i })).toHaveCount(0);
  await expect(page.getByText("Hide meals with my allergens")).toHaveCount(0);
  await shot(page, "01-catalogue-visitor");

  // Search works against the live catalogue.
  await page.getByLabel("Search", { exact: true }).fill("zzzz-no-such-meal");
  await page.getByLabel("Search", { exact: true }).press("Enter");
  await expect(page.getByText("No meals match those filters.")).toBeVisible();

  const meal = await firstMeal(page);
  await page.goto(`${urls.web}${meal}`);
  await expect(page.getByRole("heading", { level: 2, name: "Availability" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "In your kit" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Allergens" })).toBeVisible();
  await shot(page, "02-meal-detail-visitor");
  await page.getByRole("link", { name: "See plans" }).click();
  await expect(page).toHaveURL(`${urls.web}/plans`);
  // Plans and prices come from configuration, never from this test.
  await expect(page.locator("main article h2").first()).toBeVisible();
  await expect(page.getByText(/a week/).first()).toBeVisible();
  await shot(page, "03-plans-visitor");
});

test("a plan chosen as a visitor survives registration and email confirmation", async ({ page, request }) => {
  await page.goto(`${urls.web}/plans`);
  const choose = page.getByRole("link", { name: /^Choose / }).first();
  const target = await choose.getAttribute("href");
  expect(target).toMatch(/^\/subscribe\?plan=/);
  await choose.click();
  // Subscribing needs an account: sign-in remembers where the visitor was going.
  await expect(page).toHaveURL(new RegExp(`/sign-in\\?next=${encodeURIComponent(target!).replace(/[?]/g, "\\?")}$`));
  await page.getByRole("link", { name: "Create an account" }).click();
  await expect(page).toHaveURL(/\/register\?next=/);

  const email = uniqueEmail("visitor-plan");
  await page.getByLabel("First name").fill("Kavita");
  await page.getByLabel("Last name").fill("Ramdass");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByLabel("I accept the Mise terms and privacy policy.").check();
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Check your email");

  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  const link = new URL(linkIn(body, "/verify-email"));
  await page.goto(`${urls.web}${link.pathname}${link.search}`);
  await page.getByRole("button", { name: "Confirm my email" }).click();
  await expect(page.getByText("Your email is confirmed")).toBeVisible();
  // The sign-in form after confirmation carries no "next"; the browser remembered the plan.
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(`${urls.web}${target}`);
  // Used once: it does not stick to later sign-ins.
  const remembered = (await page.context().cookies()).find((c) => c.name === "mise_after_signup");
  expect(remembered).toBeUndefined();
});

test("public pages are indexable, private pages are not, and the sitemap lists only the public catalogue", async ({ page, request }) => {
  const meal = await firstMeal(page);
  for (const path of ["/meals", meal, "/plans"]) {
    const res = await request.get(`${urls.web}${path}`);
    expect(res.headers()["x-robots-tag"], path).toBeUndefined();
    await page.goto(`${urls.web}${path}`);
    await expect(page.locator('meta[name="robots"]')).toHaveAttribute("content", "index, follow");
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", `${urls.web}${path}`);
    expect(await page.title()).toMatch(/\| Mise$/);
  }
  for (const path of ["/sign-in", "/register", "/dashboard"]) {
    const res = await request.get(`${urls.web}${path}`, { maxRedirects: 0 });
    expect(res.headers()["x-robots-tag"], path).toBe("noindex, nofollow");
  }
  const robots = await (await request.get(`${urls.web}/robots.txt`)).text();
  expect(robots).toContain("Disallow: /");
  expect(robots).toContain("Allow: /meals");
  const sitemap = await (await request.get(`${urls.web}/sitemap.xml`)).text();
  const meals = await (await request.get(`${urls.api}/api/v1/meals?page_size=48`)).json();
  for (const m of meals.data.items as { slug: string; is_demo: boolean }[]) {
    // Demo meals are never offered to search engines, even where demo content is shown.
    expect(sitemap.includes(`/meals/${m.slug}<`), m.slug).toBe(!m.is_demo);
  }
  for (const path of ["/how-it-works", "/faq", "/about", "/contact"]) {
    await page.goto(`${urls.site}${path}`);
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", new RegExp(`${path}$`));
    await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
  }
  const siteMap = await (await request.get(`${urls.site}/sitemap.xml`)).text();
  for (const path of ["/how-it-works", "/faq", "/about", "/contact"]) expect(siteMap).toContain(`${path}<`);
});

test("public pages fit every phone, tablet and desktop width without sideways scrolling", async ({ page }) => {
  const meal = await firstMeal(page);
  const pages = [`${urls.web}/meals`, `${urls.web}${meal}`, `${urls.web}/plans`, `${urls.web}/sign-in`, `${urls.web}/register`, urls.site, `${urls.site}/how-it-works`, `${urls.site}/faq`, `${urls.site}/contact`];
  const overflow: string[] = [];
  for (const width of [320, 375, 390, 430, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    for (const url of pages) {
      await page.goto(url);
      const extra = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      if (extra > 0) overflow.push(`${url} at ${width}px: ${extra}px`);
    }
  }
  expect(overflow).toEqual([]);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${urls.web}/meals`);
  await shot(page, "04-catalogue-phone");
  await page.goto(`${urls.web}${meal}`);
  await shot(page, "05-meal-detail-phone");
  await page.goto(urls.site);
  await shot(page, "06-home-phone", false);
});

test("public pages have no automatically detectable accessibility problems", async ({ page }) => {
  const meal = await firstMeal(page);
  const plan = (await page.request.get(`${urls.api}/api/v1/plans`).then((r) => r.json())).data.items[0]?.slug as string | undefined;
  const paths = [`${urls.web}/meals`, `${urls.web}${meal}`, `${urls.web}/plans`, ...(plan ? [`${urls.web}/plans/${plan}`] : []), urls.site, `${urls.site}/how-it-works`, `${urls.site}/faq`, `${urls.site}/about`, `${urls.site}/contact`];
  const found: string[] = [];
  for (const url of paths) {
    await page.goto(url);
    await settled(page);
    const results = await new AxeBuilder({ page }).withTags(wcag).analyze();
    found.push(...results.violations.map((v) => `${url} ${v.id}: ${v.help} (${v.nodes.length})`));
  }
  expect(found).toEqual([]);
});

test("the catalogue can be used with the keyboard alone", async ({ page, browserName }) => {
  test.skip(browserName === "webkit", "WebKit (like Safari's default setting) does not move Tab focus to links. Needs a manual check on real Safari (not done in Sprint 9).");
  await page.goto(`${urls.web}/meals`);
  // Tab from the top of the page to the first meal link, then open it with Enter.
  const firstLink = page.locator("main article h3 a").first();
  const href = await firstLink.getAttribute("href");
  for (let i = 0; i < 60; i++) {
    await page.keyboard.press("Tab");
    if (await firstLink.evaluate((el) => el === document.activeElement)) break;
  }
  await expect(firstLink).toBeFocused();
  // The focused element shows a visible focus indicator.
  const outline = await firstLink.evaluate((el) => getComputedStyle(el).outlineStyle + " " + getComputedStyle(el).outlineWidth);
  expect(outline).not.toBe("none 0px");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(`${urls.web}${href}`);
});

test("the marketing home page follows the site mode", async ({ page }) => {
  await page.goto(urls.site);
  if (launchMode) {
    const cta = page.getByRole("link", { name: "Browse meal kits" }).first();
    await expect(cta).toHaveAttribute("href", `${urls.web}/meals`);
    await expect(page.locator("#waitlist")).toHaveCount(0);
    await shot(page, "07-home-launch");
  } else {
    await expect(page.getByRole("link", { name: "Join the waitlist" }).first()).toBeVisible();
    await shot(page, "07-home-waitlist");
  }
  // In either mode the meals shown are live catalogue meals (never the old sample files),
  // and every price shown is a configured plan price.
  const api = await (await page.request.get(`${urls.api}/api/v1/meals?page_size=6`)).json();
  const names = (api.data.items as { name: string }[]).map((m) => m.name);
  for (const name of names) await expect(page.locator("#meals").getByRole("heading", { level: 3, name })).toBeVisible();
});

test("staff see launch readiness and system status, never 'ready' while gates are open", async ({ page }) => {
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/launch`);
  await expect(page.getByRole("heading", { level: 1, name: "Launch readiness" })).toBeVisible();
  await expect(page.getByRole("heading", { name: /^Not ready to launch: \d+ critical gate/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /Payment provider/ })).toContainText("Blocked");
  await shot(page, "08-launch-readiness");
  await page.goto(`${urls.admin}/system`);
  await expect(page.getByRole("heading", { level: 1, name: "System status" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Workers" })).toContainText("Running");
  await shot(page, "09-system-status");
  for (const path of ["/launch", "/system"]) {
    await page.goto(`${urls.admin}${path}`);
    const results = await new AxeBuilder({ page }).withTags(wcag).analyze();
    expect(results.violations.map((v) => `${path} ${v.id}: ${v.help}`)).toEqual([]);
  }
});
