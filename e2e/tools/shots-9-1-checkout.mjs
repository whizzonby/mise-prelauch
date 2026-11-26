// Sprint 9.1: checkout with a choice of payment methods, and the inbox context panel, for the evidence
// screenshots. Run while the API has placeholder Stripe *test* settings (local only; no checkout is started,
// so Stripe is never called). Enables Stripe in test mode, captures, then disables it again.
// Usage: node tools/shots-9-1-checkout.mjs <out-dir>
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "@playwright/test";

const out = process.argv[2];
const env = Object.fromEntries(
  readFileSync(join(import.meta.dirname, "..", "..", ".env"), "utf8")
    .split("\n").filter((l) => /^[A-Z0-9_]+=/.test(l)).map((l) => [l.slice(0, l.indexOf("=")), l.slice(l.indexOf("=") + 1).trim()]),
);
const API = "http://localhost:8090/api/v1", ADMIN = "http://localhost:3101", WEB = "http://localhost:3102", SITE = "http://localhost:3100";
const PW = process.env.REVIEW_PASSWORD ?? "MiseReview-2026!";
const json = async (res) => { const b = await res.json(); if (!res.ok) throw new Error(JSON.stringify(b)); return b.data; };
const post = (path, body, token, method = "POST") =>
  fetch(API + path, { method, headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) }, body: JSON.stringify(body) });

const staff = (await json(await post("/admin/auth/login", { email: env.E2E_ADMIN_EMAIL, password: env.E2E_ADMIN_PASSWORD }))).token;
const marcus = (await json(await post("/auth/login", { email: "marcus@mise.test", password: PW }))).token;
const addrs = await json(await fetch(API + "/me/addresses", { headers: { Authorization: `Bearer ${marcus}` } }));
if (!addrs.items.length) {
  await json(await post("/me/addresses", { label: "Home", recipient_name: "Marcus Ali", phone: "+18685550111", line1: "4 Mucurapo Road",
    area: "St James", city: "Port of Spain", country: "TT", region: "port-of-spain", is_default: true }, marcus));
}
const address = (await json(await fetch(API + "/me/addresses", { headers: { Authorization: `Bearer ${marcus}` } }))).items[0].id;
const plan = (await json(await fetch(API + "/plans"))).items[0].slug;
await json(await post("/admin/payment-providers/stripe", { enabled: true, note: "Screenshot only (placeholder test settings)" }, staff, "PATCH"));
const opts = await json(await fetch(API + "/payment-options?currency=TTD"));
console.log("options:", opts.items.map((o) => o.label).join(", "));

const browser = await chromium.launch();
try {
  for (const [label, ctx] of [["desktop", { viewport: { width: 1440, height: 900 } }], ["phone", { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 }]]) {
    const page = await (await browser.newContext(ctx)).newPage();
    await page.goto(`${WEB}/sign-in`);
    await page.getByLabel("Email").fill("marcus@mise.test");
    await page.getByLabel("Password", { exact: true }).fill(PW);
    await page.getByRole("button", { name: "Sign in" }).click();
    await page.waitForURL((u) => !u.pathname.startsWith("/sign-in"));
    await page.goto(`${WEB}/subscribe/review?plan=${plan}&address=${address}`, { waitUntil: "networkidle" });
    await page.getByRole("group", { name: "Pay with" }).scrollIntoViewIfNeeded();
    await page.waitForTimeout(500);
    await page.screenshot({ path: join(out, `${label}-08-checkout-review-payment.png`), fullPage: label === "desktop" });
    await page.getByRole("group", { name: "Pay with" }).screenshot({ path: join(out, `${label}-10-payment-provider-selection.png`) });
    console.log("saved", label, "checkout");
    await page.context().close();
  }
  // Payment settings with Stripe enabled in test mode.
  const admin = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage();
  await admin.goto(`${ADMIN}/login`);
  await admin.getByLabel("Email", { exact: true }).fill(env.E2E_ADMIN_EMAIL);
  await admin.getByLabel("Password").fill(env.E2E_ADMIN_PASSWORD);
  await admin.getByRole("button", { name: "Sign in" }).click();
  await admin.waitForURL((u) => !u.pathname.startsWith("/login"));
  await admin.goto(`${ADMIN}/payments/providers`, { waitUntil: "networkidle" });
  await admin.screenshot({ path: join(out, "desktop-06-payment-settings.png") });
  await admin.screenshot({ path: join(out, "desktop-06b-payment-settings-full.png"), fullPage: true });

  // The inbox context panel after a live reply (the assignment form must show the agent).
  const guest = await (await browser.newContext({ viewport: { width: 1280, height: 860 } })).newPage();
  const s = Date.now().toString(36);
  await guest.goto(SITE);
  await guest.getByRole("button", { name: /Chat with Mise/ }).click();
  const chat = guest.getByRole("dialog", { name: "Mise help" });
  await chat.getByRole("button", { name: "Send us a message" }).click();
  await chat.getByLabel("Your name").fill("Renee Joseph");
  await chat.getByLabel(/^Email/).fill(`renee.ctx.${s}@example.com`);
  await chat.getByLabel("Message").fill("Hi! Do you deliver to San Fernando on Saturdays?");
  await chat.getByRole("button", { name: "Send", exact: true }).click();
  await chat.getByText("San Fernando on Saturdays?").waitFor();
  await admin.goto(`${ADMIN}/support/chat?view=all&q=renee.ctx.${s}`, { waitUntil: "networkidle" });
  await admin.getByRole("link", { name: /Renee Joseph/ }).first().click();
  await admin.getByText("Live", { exact: true }).waitFor();
  await admin.getByLabel("Reply to the customer").fill("Hi Renee, yes: San Fernando is on our Saturday route.");
  await admin.getByRole("button", { name: "Send reply" }).click();
  await admin.getByText(/^Assigned to /).waitFor();
  await admin.waitForTimeout(1500);
  console.log("agent select value set:", (await admin.getByRole("combobox", { name: /^Agent/ }).inputValue()) !== "");
  await admin.screenshot({ path: join(out, "desktop-11-admin-support-inbox.png") });
  await admin.locator('aside[aria-label="Customer and actions"]').screenshot({ path: join(out, "desktop-12-customer-context-panel.png") });
  console.log("saved admin shots");
} finally {
  await json(await post("/admin/payment-providers/stripe", { enabled: false, note: "Screenshots taken; disabled again" }, staff, "PATCH"));
  console.log("stripe disabled again");
  await browser.close();
}
