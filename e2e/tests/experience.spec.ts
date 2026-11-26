import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { urls } from "../playwright.config";

test.describe("mobile navigation", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("opens, moves focus inside, navigates and closes", async ({ page }) => {
    await page.goto("/");
    // The desktop links are not shown on a phone.
    await expect(page.getByRole("navigation", { name: "Main" })).toBeHidden();

    await page.getByRole("button", { name: "Open menu" }).click();
    const menu = page.getByRole("dialog");
    await expect(menu).toBeVisible();
    await expect(menu.getByRole("link", { name: "Meals" })).toBeVisible();

    // Escape closes it and returns focus to the button that opened it.
    await page.keyboard.press("Escape");
    await expect(menu).toBeHidden();
    await expect(page.getByRole("button", { name: "Open menu" })).toBeFocused();

    await page.getByRole("button", { name: "Open menu" }).click();
    await menu.getByRole("link", { name: "Meals" }).click();
    await expect(menu).toBeHidden();
    await expect(page).toHaveURL(/#meals$/);
    await expect(page.getByRole("heading", { name: "From the Mise menu" })).toBeInViewport();
  });

  test("the page never scrolls sideways", async ({ page }) => {
    await page.goto("/");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });
});

test.describe("reduced motion", () => {
  test.use({ reducedMotion: "reduce" });

  test("the page is complete and still, with nothing pinned or hidden", async ({ page }) => {
    await page.goto("/");

    // The hero is in its settled state from the first frame.
    const line = page.locator("#hero-title .headline-line > span").first();
    await expect(line).toHaveCSS("animation-name", "none");

    // The story stays an ordinary list: never pinned, every stage readable.
    await page.getByRole("heading", { name: "How a Mise dinner gets made" }).scrollIntoViewIfNeeded();
    await page.waitForTimeout(1500);
    await expect(page.locator("[data-story]")).not.toHaveAttribute("data-enhanced", "true");
    for (const stage of ["Farm", "Mise kitchen", "Your meal kit", "Your kitchen", "Dinner"]) {
      await expect(page.locator("#our-story").getByRole("heading", { level: 3, name: stage, exact: true })).toBeVisible();
    }

    // No photograph is left masked.
    await page.getByRole("heading", { name: "From the Mise menu" }).scrollIntoViewIfNeeded();
    const masked = await page.evaluate(() => [...document.querySelectorAll<HTMLElement>("main div")].filter((el) => el.style.clipPath).length);
    expect(masked).toBe(0);

    await expect(page.getByRole("form", { name: "Join the Mise waitlist" })).toBeVisible();
  });
});

test("with motion allowed, the story pins on a wide screen", async ({ page }) => {
  await page.goto("/");
  await page.mouse.wheel(0, 400);
  await expect(page.locator("[data-story]")).toHaveAttribute("data-enhanced", "true");
});

// Sprint 9.1 regression: a stock-photo note under the title once pushed every stage to the bottom of the
// pinned screen (mostly off it), overlapping the progress labels. The pinned stage must fit the screen.
test("the pinned story's stage, photograph and progress all fit on the screen", async ({ page }) => {
  await page.goto("/");
  await page.mouse.wheel(0, 400);
  const story = page.locator("[data-story]");
  await expect(story).toHaveAttribute("data-enhanced", "true");
  const top = await story.evaluate((el) => el.getBoundingClientRect().top + window.scrollY);
  await page.evaluate((y) => window.scrollTo(0, y + 20), top);
  await page.waitForTimeout(800);
  const vh = page.viewportSize()!.height;
  const within = async (name: string, box: { y: number; height: number } | null) => {
    expect(box, `${name} is laid out`).not.toBeNull();
    expect(box!.y, `${name} starts on screen`).toBeGreaterThanOrEqual(0);
    expect(box!.y + box!.height, `${name} ends on screen`).toBeLessThanOrEqual(vh);
  };
  const firstText = story.locator("[data-story-text]").first();
  const firstMedia = story.locator("[data-story-media]").first();
  const progress = story.locator("[data-story-progress]");
  await within("first stage text", await firstText.boundingBox());
  await within("first photograph", await firstMedia.boundingBox());
  await within("progress", await progress.boundingBox());
  // The stage sits above the progress labels, never on top of them.
  const textBox = (await firstText.boundingBox())!;
  expect(textBox.y + textBox.height).toBeLessThanOrEqual((await progress.boundingBox())!.y);
});

test.describe("accessibility", () => {
  const pages: [string, string][] = [
    ["home page", "/"],
    ["invitation page", "/join/ZZZZZZZ"],
    ["privacy policy", "/privacy"],
    ["admin sign-in", `${urls.admin}/login`],
  ];

  for (const [name, path] of pages) {
    test(`${name} has no detectable WCAG A or AA violations`, async ({ page }) => {
      // Reduced motion keeps every section in its final state for the scan.
      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.goto(path);
      const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze();
      const summary = results.violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n    ${v.nodes.map((n) => n.target.join(" ")).slice(0, 5).join("\n    ")}`);
      expect(summary, summary.join("\n")).toEqual([]);
    });
  }

  test("the waitlist can be completed with the keyboard alone", async ({ page }) => {
    await page.goto("/");
    await page.keyboard.press("Tab");
    await expect(page.getByRole("link", { name: "Skip to content" })).toBeFocused();

    const form = page.getByRole("form", { name: "Join the Mise waitlist" });
    await form.getByLabel("First name").focus();
    await page.keyboard.type("Asha");
    await page.keyboard.press("Tab");
    // Last name follows first name (added in d70359c; this test predated it).
    await expect(form.getByLabel("Last name")).toBeFocused();
    await page.keyboard.type("Mohammed");
    await page.keyboard.press("Tab");
    await expect(form.getByLabel("Email", { exact: true })).toBeFocused();
    await page.keyboard.type("asha@example.com");
    await page.keyboard.press("Tab");
    await expect(form.getByLabel("Where would you like delivery?")).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(form.getByLabel("Household size")).toBeFocused();
    await page.keyboard.press("Tab");
    // Dietary options are real checkboxes: Space toggles.
    await expect(form.getByLabel("Vegetarian")).toBeFocused();
    await page.keyboard.press("Space");
    await expect(form.getByLabel("Vegetarian")).toBeChecked();
  });
});

test("search engines get the essentials", async ({ page, request }) => {
  await page.goto("/");
  await expect(page).toHaveTitle(/Mise/);
  await expect(page.locator('meta[name="description"]')).toHaveAttribute("content", /Caribbean meal kits/);
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", /^https?:\/\/[^/]+\/?$/);
  await expect(page.locator('meta[property="og:image"]')).toHaveAttribute("content", /opengraph-image/);
  expect(await page.locator("h1").count()).toBe(1);
  const structured = JSON.parse((await page.locator('script[type="application/ld+json"]').textContent()) ?? "{}");
  expect(structured["@graph"].map((node: { "@type": string }) => node["@type"])).toEqual(["Organization", "WebSite", "FAQPage"]);

  const robots = await (await request.get("/robots.txt")).text();
  expect(robots).toContain("Sitemap:");
  expect(robots).toContain("Disallow: /welcome");
  expect((await request.get("/sitemap.xml")).ok()).toBe(true);

  // Personal pages opt out of indexing.
  await page.goto("/welcome");
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute("content", /noindex/);
});
