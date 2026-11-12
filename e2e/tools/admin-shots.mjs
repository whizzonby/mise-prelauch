// Dev helper: signs in to the admin once and screenshots the main console pages at
// desktop and phone widths. Run from e2e/: node tools/admin-shots.mjs <out-dir>
import { chromium } from "@playwright/test";
import { existsSync, mkdirSync } from "node:fs";
import { join } from "node:path";

if (existsSync("../.env")) process.loadEnvFile("../.env");
const base = "http://localhost:3101";
const outDir = process.argv[2] ?? "../docs/screenshots/sprint-4";
mkdirSync(outDir, { recursive: true });

// [name, list path, optional selector for a detail link to follow]
const pages = [
  ["overview", "/"],
  ["customers", "/customers"],
  ["customer-360", "/customers", 'main a[href^="/customers/"]'],
  ["leads", "/leads"],
  ["lead-crm", "/leads", 'main a[href^="/leads/"]:not([href*="export"])'],
  ["segments", "/segments"],
  ["campaigns", "/campaigns"],
  ["support", "/support"],
  ["support-case", "/support", 'main a[href^="/support/"]:not([href$="/new"])'],
  ["subscription", "/subscriptions", 'main a[href^="/subscriptions/"]'],
  ["invoices", "/invoices"],
  ["reports", "/reports"],
  ["exports", "/exports"],
  ["staff", "/staff"],
  ["roles", "/roles"],
  ["audit", "/audit"],
  ["settings", "/settings"],
  ["alerts", "/alerts"],
];

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
await page.goto(`${base}/login`);
await page.getByLabel("Email", { exact: true }).fill(process.env.E2E_ADMIN_EMAIL);
await page.getByLabel("Password").fill(process.env.E2E_ADMIN_PASSWORD);
await page.getByRole("button", { name: "Sign in" }).click();
await page.waitForURL(`${base}/`);

for (const [name, path, follow] of pages) {
  await page.goto(base + path, { waitUntil: "networkidle" });
  if (follow) {
    const link = page.locator(follow).first();
    if (!(await link.count())) {
      console.log(`skip ${name}: no row to open on ${path}`);
      continue;
    }
    await page.goto(base + (await link.getAttribute("href")), { waitUntil: "networkidle" });
  }
  const url = page.url();
  for (const [suffix, width, height] of [["desktop", 1440, 900], ["phone", 390, 844]]) {
    await page.setViewportSize({ width, height });
    await page.goto(url, { waitUntil: "networkidle" });
    await page.screenshot({ path: join(outDir, `${name}-${suffix}.png`), fullPage: true });
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  console.log(`ok   ${name}  ${url.replace(base, "")}`);
}
await browser.close();
