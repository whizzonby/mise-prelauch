// Dev helper: signs in to the admin once and screenshots the main console pages at
// desktop and phone widths. Run from e2e/: node tools/admin-shots.mjs <out-dir> [sprint-4|sprint-5|sprint-6]
import { chromium } from "@playwright/test";
import { existsSync, mkdirSync } from "node:fs";
import { join } from "node:path";

if (existsSync("../.env")) process.loadEnvFile("../.env");
const base = "http://localhost:3101";
const set = process.argv[3] ?? "sprint-4";
const outDir = process.argv[2] ?? `../docs/screenshots/${set}`;
mkdirSync(outDir, { recursive: true });

// [name, list path, optional selector for a detail link to follow]
const kitchen = [
  ["production-overview", "/production"],
  ["production-calendar", "/production/calendar"],
  ["production-plans", "/production/plans"],
  ["production-plan", "/production/plans", 'main a[href^="/production/plans/"]'],
  ["requirements", "/production/requirements"],
  ["batches", "/production/batches"],
  ["batch-completed", "/production/batches?status=completed", 'main a[href^="/production/batches/"]'],
  ["batch-released", "/production/batches?status=released", 'main a[href^="/production/batches/"]'],
  ["kitchen-tasks", "/production/tasks"],
  ["quality", "/production/quality"],
  ["packaging", "/production/packaging"],
  ["waste", "/production/waste"],
  ["kitchen-settings", "/production/settings"],
  ["recipes", "/recipes"],
  ["recipe-review", "/recipes", 'main a[href^="/recipes/"]', "?step=review"],
  ["recipe-ingredients", "/recipes", 'main a[href^="/recipes/"]', "?step=ingredients"],
  ["recipe-nutrition", "/recipes", 'main a[href^="/recipes/"]', "?step=safety"],
  ["recipe-costing", "/recipes", 'main a[href^="/recipes/"]', "?step=costing"],
  ["ingredients", "/ingredients"],
  ["ingredient", "/ingredients", 'main a[href^="/ingredients/"]'],
  ["chefs", "/chefs"],
  ["nutrition-reviews", "/nutrition"],
  ["recipe-approvals", "/recipe-approvals"],
  ["production-report", "/reports/production"],
];
const supply = [
  ["supply-overview", "/supply"],
  ["procurement-runs", "/procurement"],
  ["procurement-run", "/procurement", 'main a[href^="/procurement/"]'],
  ["purchase-orders", "/purchase-orders"],
  ["purchase-order", "/purchase-orders?status=partially_received", 'main a[href^="/purchase-orders/"]'],
  ["purchase-order-print", "/purchase-orders?status=partially_received", 'main a[href^="/purchase-orders/"]', "/print"],
  ["quotations", "/quotations"],
  ["receiving", "/receiving"],
  ["receive-form", "/receiving", 'main a[href^="/receiving/new"]'],
  ["receipt", "/receiving", 'main a[href^="/receiving/GR"], main a[href^="/receiving/"]:not([href*="new"])'],
  ["inventory", "/inventory"],
  ["lots", "/inventory/lots"],
  ["lot", "/inventory/lots?status=available", 'main a[href^="/inventory/lots/"]'],
  ["movements", "/inventory/movements"],
  ["stock-counts", "/inventory/counts"],
  ["traceability", "/traceability"],
  ["supply-settings", "/supply/settings"],
  ["suppliers", "/suppliers"],
  ["supplier", "/suppliers?status=approved", 'main a[href^="/suppliers/"]'],
  ["supplier-farm", "/suppliers?tab=farmers", 'main a[href^="/suppliers/"]', "?tab=farm"],
  ["supplier-products", "/supplier-products"],
  ["supplier-product", "/supplier-products?mapping=confirmed", 'main a[href^="/supplier-products/"]'],
  ["availability", "/supplier-availability"],
  ["report-procurement", "/reports/procurement"],
  ["report-inventory", "/reports/inventory"],
  ["report-suppliers", "/reports/suppliers"],
];
const sprint4 = [
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
const pages = set === "sprint-5" ? kitchen : set === "sprint-6" ? supply : sprint4;

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
await page.goto(`${base}/login`);
await page.getByLabel("Email", { exact: true }).fill(process.env.E2E_ADMIN_EMAIL);
await page.getByLabel("Password").fill(process.env.E2E_ADMIN_PASSWORD);
await page.getByRole("button", { name: "Sign in" }).click();
await page.waitForURL(`${base}/`);

for (const [name, path, follow, query = ""] of pages) {
  await page.goto(base + path, { waitUntil: "networkidle" });
  if (follow) {
    const link = page.locator(follow).first();
    if (!(await link.count())) {
      console.log(`skip ${name}: no row to open on ${path}`);
      continue;
    }
    await page.goto(base + (await link.getAttribute("href")) + query, { waitUntil: "networkidle" });
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
