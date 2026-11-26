// Sprint 9.1 evidence screenshots (brief §36), desktop and phone.
// Usage: node tools/shots-9-1.mjs <out-dir>
// Needs the stack running, E2E_ADMIN_EMAIL/E2E_ADMIN_PASSWORD (admin role) and the review customer
// marcus@mise.test (no subscription) with REVIEW_PASSWORD. Creates its own guest conversation and a
// clearly-labelled test help article, which it returns to draft at the end.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "@playwright/test";

const out = process.argv[2];
const env = Object.fromEntries(
  readFileSync(join(import.meta.dirname, "..", "..", ".env"), "utf8")
    .split("\n").filter((l) => /^[A-Z0-9_]+=/.test(l)).map((l) => [l.slice(0, l.indexOf("=")), l.slice(l.indexOf("=") + 1).trim()]),
);
const SITE = "http://localhost:3100", ADMIN = "http://localhost:3101", WEB = "http://localhost:3102";
const REVIEW_PW = process.env.REVIEW_PASSWORD ?? "MiseReview-2026!";
const desktop = { viewport: { width: 1440, height: 900 } };
const phone = { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 };
const browser = await chromium.launch();
const s = Date.now().toString(36);
const shot = async (page, name, fullPage = false) => {
  await page.waitForTimeout(600);
  await page.screenshot({ path: join(out, `${name}.png`), fullPage });
  console.log("saved", name);
};
const settle = (page) => page.waitForFunction(() => document.getAnimations().every((a) => a.playState !== "running" || a.effect?.getTiming().iterations === Infinity)).catch(() => undefined);

async function signInAdmin(page) {
  await page.goto(`${ADMIN}/login`);
  await page.getByLabel("Email", { exact: true }).fill(env.E2E_ADMIN_EMAIL);
  await page.getByLabel("Password").fill(env.E2E_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL((u) => !u.pathname.startsWith("/login"));
}
async function signInCustomer(page, email) {
  await page.goto(`${WEB}/sign-in`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(REVIEW_PW);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL((u) => !u.pathname.startsWith("/sign-in"));
}

for (const [label, ctxOpts] of [["desktop", desktop], ["phone", phone]]) {
  const page = await (await browser.newContext(ctxOpts)).newPage();
  // Landing hero, the whole landing page, the closed launcher.
  await page.goto(SITE, { waitUntil: "networkidle" });
  await settle(page);
  await shot(page, `${label}-01-landing-hero`);
  await page.emulateMedia({ reducedMotion: "reduce" }); // the story as a plain sequence for the full-page capture
  await page.reload({ waitUntil: "networkidle" });
  if (label === "desktop") await shot(page, `${label}-02-landing-page`, true);
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.goto(SITE, { waitUntil: "networkidle" });
  await page.evaluate(() => window.scrollTo(0, document.querySelector("[data-story]").getBoundingClientRect().top + window.scrollY + 40));
  await page.waitForTimeout(1500);
  await shot(page, `${label}-03-chat-launcher-closed`);
  // Chat open (home), then a guest conversation.
  await page.getByRole("button", { name: /Chat with Mise/ }).click();
  await page.getByRole("dialog", { name: "Mise help" }).waitFor();
  await shot(page, `${label}-04-chat-open-home`);
  await page.getByRole("button", { name: "Send us a message" }).click();
  await page.getByLabel("Your name").fill("Renee Joseph");
  await page.getByLabel(/^Email/).fill(`renee.${label}.${s}@example.com`);
  await page.getByLabel("Message").fill("Hi! Do you deliver to San Fernando on Saturdays?");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await page.getByText("San Fernando on Saturdays?").waitFor();
  await page.context().close();
}

// Agent replies, with a note, so the conversation screens have both sides.
const agent = await (await browser.newContext(desktop)).newPage();
await signInAdmin(agent);
await agent.goto(`${ADMIN}/support/chat?view=all&q=renee.desktop.${s}`, { waitUntil: "networkidle" });
await agent.getByRole("link", { name: /Renee Joseph/ }).first().click();
await agent.getByText("Live", { exact: true }).waitFor();
await agent.getByLabel("Reply to the customer").fill("Hi Renee, yes: San Fernando is on our Saturday route. Would you like to see this week's menu?");
await agent.getByRole("button", { name: "Send reply" }).click();
await agent.getByText("Saturday route").first().waitFor();
await agent.getByRole("radio", { name: "Internal note" }).click();
await agent.getByLabel(/Internal note/).fill("Asked about San Fernando. Check the zone list before promising a day.");
await agent.getByRole("button", { name: "Add note" }).click();
await agent.getByText("Check the zone list").first().waitFor();
await agent.waitForTimeout(1500);
await shot(agent, "desktop-11-admin-support-inbox");
await agent.locator('aside[aria-label="Customer and actions"]').screenshot({ path: join(out, "desktop-12-customer-context-panel.png") });
console.log("saved desktop-12-customer-context-panel");
await agent.goto(`${ADMIN}/support/chat/settings`, { waitUntil: "networkidle" });
await shot(agent, "desktop-14-support-settings");
await agent.goto(`${ADMIN}/payments/providers`, { waitUntil: "networkidle" });
await shot(agent, "desktop-06-payment-settings");
await shot(agent, "desktop-06b-payment-settings-full", true);

// A clearly marked test article so the help centre has something to show; returned to draft after.
await agent.goto(`${ADMIN}/help-centre/new`);
await agent.getByLabel("Title").fill("Example article (screenshot only)");
await agent.getByLabel(/^Address/).fill(`example-article-${s}`);
await agent.getByLabel(/^Summary/).fill("Placeholder text for a screenshot. Not Mise policy.");
await agent.getByLabel(/^Article/).fill("This placeholder shows how an article looks. It is not published content.\n\nReal articles need approved wording.");
await agent.getByRole("button", { name: "Create draft" }).click();
await agent.waitForURL(/\/help-centre\/[0-9a-f-]{36}$/);
const articleUrl = agent.url();
await agent.getByRole("button", { name: "Send for review" }).click();
await agent.getByRole("button", { name: "Publish" }).click();
await agent.getByRole("button", { name: /Archive/ }).waitFor();
await shot(agent, "desktop-15-help-article-editor");

// Customer views: the guest conversation with the reply, catalogue, checkout, help centre.
for (const [label, ctxOpts] of [["desktop", desktop], ["phone", phone]]) {
  const guest = await (await browser.newContext(ctxOpts)).newPage();
  // The guest from the desktop run (same browser state is not kept); show a fresh conversation for the phone instead.
  await guest.goto(SITE, { waitUntil: "networkidle" });
  await guest.getByRole("button", { name: /Chat with Mise/ }).click();
  await guest.getByRole("button", { name: "Send us a message" }).click();
  await guest.getByLabel("Your name").fill("Renee Joseph");
  await guest.getByLabel(/^Email/).fill(`renee.view.${label}.${s}@example.com`);
  await guest.getByLabel("Message").fill("Hi! Do you deliver to San Fernando on Saturdays?");
  await guest.getByRole("button", { name: "Send", exact: true }).click();
  await guest.getByText("San Fernando on Saturdays?").waitFor();
  await agent.goto(`${ADMIN}/support/chat?view=all&q=renee.view.${label}.${s}`, { waitUntil: "networkidle" });
  await agent.getByRole("link", { name: /Renee Joseph/ }).first().click();
  await agent.getByText("Live", { exact: true }).waitFor();
  await agent.getByLabel("Reply to the customer").fill("Hi Renee, yes: San Fernando is on our Saturday route.");
  await agent.getByRole("button", { name: "Send reply" }).click();
  await guest.getByText("Saturday route").waitFor({ timeout: 10_000 });
  await shot(guest, `${label}-09-customer-conversation${label === "phone" ? "-mobile-messaging" : ""}`);
  await guest.context().close();

  const cust = await (await browser.newContext(ctxOpts)).newPage();
  await signInCustomer(cust, "marcus@mise.test");
  await cust.goto(`${WEB}/meals`, { waitUntil: "networkidle" });
  await shot(cust, `${label}-05-meal-catalogue`);
  const plans = await (await cust.request.get("http://localhost:8090/api/v1/plans")).json();
  const plan = plans.data.items[0].slug;
  await cust.goto(`${WEB}/subscribe?plan=${plan}`, { waitUntil: "networkidle" });
  await shot(cust, `${label}-07-checkout-address`);
  await cust.getByRole("button", { name: /Continue|Review/ }).first().click().catch(() => undefined);
  await cust.waitForURL(/\/subscribe\/review/, { timeout: 10_000 }).catch(() => undefined);
  await cust.waitForTimeout(800);
  await shot(cust, `${label}-08-checkout-review-payment`, label === "desktop");
  await cust.goto(`${WEB}/help`, { waitUntil: "networkidle" });
  await shot(cust, `${label}-13-help-centre`);
  await cust.goto(`${WEB}/help/example-article-${s}`, { waitUntil: "networkidle" });
  await shot(cust, `${label}-13b-help-article`);
  await cust.context().close();
}

// Take the example article off the site again.
await agent.goto(articleUrl);
await agent.getByRole("button", { name: /Revise/ }).click();
await agent.getByRole("button", { name: "Send for review" }).waitFor();
console.log("example article returned to draft");
await browser.close();
